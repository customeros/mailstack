package cron

import (
	"context"
	"sync"
	"time"

	"github.com/caarlos0/env/v6"
	cronv3 "github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/customeros/mailstack/internal/config"
	cron_config "github.com/customeros/mailstack/internal/cron/config"
	"github.com/customeros/mailstack/internal/logger"
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
		GroupMailstack: &sync.Mutex{},
	},
}

type CronManager struct {
	cfg    *config.Config
	log    logger.Logger
	cron   *cronv3.Cron
	k8s    kubernetes.Interface
	stopCh chan struct{}
	jobIDs map[string]cronv3.EntryID
}

func NewCronManager(cfg *config.Config, log logger.Logger, k8s kubernetes.Interface) *CronManager {
	return &CronManager{
		cfg:    cfg,
		log:    log,
		k8s:    k8s,
		stopCh: make(chan struct{}),
		jobIDs: make(map[string]cronv3.EntryID),
	}
}

// Start initializes and starts the cron manager with leader election
// If k8s is nil, it will start in local mode without leader election
func (cm *CronManager) Start(podName, namespace string) error {
	// If k8s client is nil, start in local mode
	if cm.k8s == nil {
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

	// Start leader election
	leaderelection.RunOrDie(context.Background(), leaderelection.LeaderElectionConfig{
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

// StartCron initializes and starts the cron scheduler
func (cm *CronManager) StartCron() {
	cm.log.Info("Starting cron manager")
	// Create a new cron with seconds field enabled and panic recovery
	cronOptions := []cronv3.Option{
		cronv3.WithSeconds(),
		cronv3.WithChain(
			cronv3.Recover(cronv3.DefaultLogger),
		),
	}
	c := cronv3.New(cronOptions...)
	cm.registerJobs(c)
	c.Start()
	cm.cron = c
}

// registerJobs adds all cron jobs to the scheduler
func (cm *CronManager) registerJobs(c *cronv3.Cron) {
	// Add mailstack reputation monitoring job
	// Load cron config from environment variables
	var cronConfig cron_config.Config
	if err := env.Parse(&cronConfig); err != nil {
		cm.log.Fatalf("Failed to parse cron config from environment: %v", err)
	}

	cm.log.Infof("Using cron schedule for mailstack reputation: %s", cronConfig.CronScheduleMailstackReputation)

	if cronConfig.CronScheduleMailstackReputation != "" {
		id, err := c.AddFunc(cronConfig.CronScheduleMailstackReputation, func() {
			jobLocks.locks[GroupMailstack].Lock()
			defer jobLocks.locks[GroupMailstack].Unlock()
			cm.checkMailstackDomainReputation()
		})
		if err != nil {
			cm.log.Fatalf("Could not add mailstack reputation cron job: %v", err)
		}
		cm.jobIDs["mailstack_reputation"] = id
		cm.log.Infof("Registered mailstack reputation job with schedule: %s", cronConfig.CronScheduleMailstackReputation)
	} else {
		cm.log.Warn("Mailstack reputation cron schedule not configured, job not registered")
	}
}

func (cm *CronManager) checkMailstackDomainReputation() {
	// TODO: Implement mailstack domain reputation checking logic
	cm.log.Info("Running mailstack domain reputation check")
}
