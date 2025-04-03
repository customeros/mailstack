package event_logger

import (
	"context"
	"fmt"
	"time"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/enum"
	mailstack_errors "github.com/customeros/mailstack/internal/errors"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
	"github.com/customeros/mailstack/internal/utils"
)

type EventLoggerService struct {
	repositories  *repository.Repositories
	eventsStorage interfaces.StorageService
}

func NewEventLoggerService(
	repos *repository.Repositories,
	eventsStorage interfaces.StorageService,
) interfaces.EventLoggerService {
	return &EventLoggerService{
		repositories:  repos,
		eventsStorage: eventsStorage,
	}
}

func (s *EventLoggerService) NewEmailEventRecord(ctx context.Context) *models.EmailEvent {
	spans, ctx := telemetry.StartServiceSpan(ctx, "eventLoggerService.NewEmailEventRecord")
	defer spans.Finish()

	// validate context
	var err error
	tenant := utils.GetTenantFromContext(ctx)
	userID := utils.GetUserIdFromContext(ctx)
	if tenant == "" {
		err = mailstack_errors.ErrTenantMissing
		spans.TraceError(err)
	}
	if userID == "" {
		err = mailstack_errors.ErrUserIdMissing
		spans.TraceError(err)
	}
	return &models.EmailEvent{
		ID:           utils.GenerateNanoIDWithPrefix("event", 21),
		Timestamp:    utils.Now(),
		Service:      enum.MailstackStorageService,
		Tenant:       tenant,
		User:         userID,
		ErrorMessage: err.Error(),
	}
}

// storeEventInR2 stores the event data in R2 and returns the key
func (s *EventLoggerService) StoreEmailEventInR2(ctx context.Context, eventID string, data []byte) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "eventLoggerService.StoreEventInR2")
	defer spans.Finish()

	payloadKey := fmt.Sprintf("emails/%s/%s.json", time.Now().Format("20060102"), eventID)

	// Store in R2
	err := s.eventsStorage.Upload(ctx, payloadKey, data, "application/json")
	if err != nil {
		spans.TraceError(err)
	}

	return payloadKey, err
}

func (s *EventLoggerService) LogEmailEventToTimescale(ctx context.Context, event *models.EmailEvent) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "eventLoggerService.LogEmailEventToTimescale")
	defer spans.Finish()

	event.Timestamp = utils.Now()
	event.Tenant = utils.GetTenantFromContext(ctx)
	event.User = utils.GetUserIdFromContext(ctx)
	event.Direction = enum.EmailDirectionInbound
	event.Service = enum.MailstackStorageService

	if event.ErrorMessage == "" {
		event.Success = true
	}

	// Save to database
	err := s.repositories.EmailEventRepository.Create(ctx, event)
	if err != nil {
		spans.TraceError(err)
	}
	return
}
