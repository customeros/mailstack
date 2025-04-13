package interfaces

import (
	"context"

	"github.com/customeros/mailstack/internal/models"
)

// GoogleService handles Google-specific operations like OAuth2 token management
type GoogleService interface {
	// RefreshTokenIfNeeded checks if the token needs refresh and refreshes it if necessary
	RefreshTokenIfNeeded(ctx context.Context, mailbox *models.Mailbox) error

	// RefreshToken refreshes the OAuth2 token for the given mailbox
	RefreshToken(ctx context.Context, mailbox *models.Mailbox) error

	// GetDecryptedAccessToken returns the decrypted access token for the mailbox
	GetDecryptedAccessToken(ctx context.Context, mailbox *models.Mailbox) (string, error)
}
