package cron

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/config"
	cron_config "github.com/customeros/mailstack/internal/cron/config"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/tracing"
	cronv3 "github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// CONSTANTS
const (
	// GroupMailstack is the group for mailstack related jobs
	GroupMailstack = "mailstack"

	// LeaseDuration is how long a lease lasts before needing renewal
	LeaseDuration = 15 * time.Second
	// RenewDeadline is how long a leader has to renew its lease
	RenewDeadline = 10 * time.Second
	// RetryPeriod is how long to wait between leadership attempts
	RetryPeriod = 2 * time.Second
)

// LOCK MANAGEMENT
var jobLocks = struct {
	sync.Mutex
	locks map[string]*sync.Mutex
}{
	locks: map[string]*sync.Mutex{
		GroupMailstack: new(sync.Mutex),
	},
}

type CronManager struct {
	cfg    *config.Config
	log    logger.Logger
	cron   *cronv3.Cron
	k8s    kubernetes.Interface
	stopCh chan struct{}
	jobIDs map[string]cronv3.EntryID
	domain interfaces.DomainService
}

func NewCronManager(cfg *config.Config, log logger.Logger, k8s kubernetes.Interface, domain interfaces.DomainService) *CronManager {
	return &CronManager{
		cfg:    cfg,
		log:    log,
		k8s:    k8s,
		stopCh: make(chan struct{}),
		jobIDs: make(map[string]cronv3.EntryID),
		domain: domain,
	}
}

// Start initializes and starts the cron manager with leader election
// If k8s is nil, it will start in local mode without leader election
func (cm *CronManager) Start(podName, namespace string) error {
	// If k8s client is nil or we're in local development, start in local mode
	if cm.k8s == nil || os.Getenv("LOCAL_DEV") == "true" {
		cm.log.Info("Starting cron manager in local mode")
		cm.StartCron()
		return nil
	}

	// Create the leader election lock
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      "mailstack-cron-leader",
			Namespace: namespace,
		},
		Client: cm.k8s.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: podName,
		},
	}

	// Channel to track leader election errors
	errCh := make(chan error, 1)

	// Start leader election
	go func() {
		// Try leader election
		le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
			Lock:            lock,
			ReleaseOnCancel: true,
			LeaseDuration:   LeaseDuration,
			RenewDeadline:   RenewDeadline,
			RetryPeriod:     RetryPeriod,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					cm.StartCron()
				},
				OnStoppedLeading: func() {
					cm.log.Info("Leader lost - stopping crons")
					cm.Stop()
				},
				OnNewLeader: func(identity string) {
					cm.log.Infof("New leader elected: %s", identity)
				},
			},
		})
		if err != nil {
			errCh <- err
			return
		}

		// Start leader election
		ctx := context.Background()
		le.Run(ctx)
	}()

	// Wait briefly to see if leader election fails immediately
	select {
	case err := <-errCh:
		cm.log.Warnf("Leader election failed, falling back to local mode: %v", err)
		cm.StartCron()
	case <-time.After(5 * time.Second):
		// Leader election seems to be working, continue normally
	}

	return nil
}

// Stop gracefully stops the cron manager
func (cm *CronManager) Stop() {
	if cm.cron != nil {
		cm.log.Info("Stopping cron manager")
		ctx := cm.cron.Stop()
		// Wait for jobs to finish
		<-ctx.Done()
	}
	close(cm.stopCh)
}

// registerJobs adds all cron jobs to the scheduler
func (cm *CronManager) registerJobs(c *cronv3.Cron) {
	// Load cron config from environment variables
	var cronConfig cron_config.Config
	if err := env.Parse(&cronConfig); err != nil {
		cm.log.Fatalf("Failed to parse cron config from environment: %v", err)
	}

	// Register heartbeat job
	if cronConfig.CronScheduleHeartbeat != "" {
		podName := os.Getenv("POD_NAME")
		if podName == "" {
			podName = "local"
		}
		id, err := c.AddFunc(cronConfig.CronScheduleHeartbeat, func() {
			defer tracing.RecoverAndLogToJaeger(cm.log)
			cm.log.Infof("Cron heartbeat from pod: %s", podName)
		})
		if err != nil {
			cm.log.Fatalf("Could not add heartbeat cron job: %v", err)
		}
		cm.jobIDs["heartbeat"] = id
		cm.log.Infof("Registered heartbeat job with schedule: %s", cronConfig.CronScheduleHeartbeat)
	}

	// Add mailstack reputation monitoring job
	if cronConfig.CronScheduleMailstackReputation != "" {
		id, err := c.AddFunc(cronConfig.CronScheduleMailstackReputation, func() {
			defer tracing.RecoverAndLogToJaeger(cm.log)
			jobLocks.locks[GroupMailstack].Lock()
			defer jobLocks.locks[GroupMailstack].Unlock()
			cm.checkMailstackDomainReputation()
		})
		if err != nil {
			cm.log.Fatalf("Could not add mailstack reputation cron job: %v", err)
		}
		cm.jobIDs["mailstack_reputation"] = id
		cm.log.Infof("Registered mailstack reputation job with schedule: %s", cronConfig.CronScheduleMailstackReputation)
	}
}

// StartCron initializes and starts the cron scheduler
func (cm *CronManager) StartCron() {
	cm.log.Info("Starting cron manager")
	// Create a new cron with seconds field enabled and panic recovery
	cronOptions := []cronv3.Option{
		cronv3.WithSeconds(),
		cronv3.WithChain(
			cronv3.SkipIfStillRunning(cronv3.DefaultLogger), // Skip if still running
			cronv3.Recover(cronv3.DefaultLogger),            // Default recovery as backup
		),
	}
	c := cronv3.New(cronOptions...)
	cm.registerJobs(c)
	c.Start()
	cm.cron = c
}

func (cm *CronManager) checkMailstackDomainReputation() {
	cm.log.Info("Running mailstack domain reputation check")

	// Create a background context for the operation
	ctx := context.Background()

	span, ctx := tracing.StartTracerSpan(ctx, "CronManager.checkMailstackDomainReputation")
	defer span.Finish()
	tracing.TagComponentCronJob(span)

	// Call the domain service to check reputation
	if err := cm.domain.CheckMailstackDomainReputations(ctx); err != nil {
		tracing.TraceErr(span, err)
		cm.log.Errorf("Failed to check domain reputations: %v", err)
		return
	}

	cm.log.Info("Successfully completed domain reputation check")
}
