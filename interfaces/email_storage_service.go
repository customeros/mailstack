package interfaces

import (
	"context"

	"github.com/customeros/mailstack/dto"
)

type EmailStorageService interface {
	Start(ctx context.Context)
	HandleRawEmail(ctx context.Context, rawEmail dto.EmailInboundNew)
	PublishStoredEmail(ctx context.Context, email *dto.EmailRecord)
	Close() error
}
