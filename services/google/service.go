package google

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/models"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/internal/telemetry"
)

// Service handles Google-specific operations like OAuth2 token management
type googleService struct {
	repositories *repository.Repositories
	config       *oauth2.Config
	encryptKey   string
}

// NewService creates a new Google service
func NewGoogleService(repos *repository.Repositories, clientID, clientSecret, encryptKey string) interfaces.GoogleService {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes: []string{
			"https://mail.google.com/", // Full access to Gmail account via IMAP
		},
	}

	return &googleService{
		repositories: repos,
		config:       config,
		encryptKey:   encryptKey,
	}
}

// RefreshTokenIfNeeded checks if the token needs refresh and refreshes it if necessary
func (s *googleService) RefreshTokenIfNeeded(ctx context.Context, mailbox *models.Mailbox) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "GoogleService.RefreshTokenIfNeeded")
	defer spans.Finish()
	spans.TagEntity(mailbox.ID)

	// Check if token needs refresh (expires in less than 5 minutes)
	if mailbox.OAuthTokenExpiry != nil && mailbox.OAuthTokenExpiry.After(time.Now().Add(5*time.Minute)) {
		return nil // Token is still valid
	}

	return s.RefreshToken(ctx, mailbox)
}

// RefreshToken refreshes the OAuth2 token for the given mailbox
func (s *googleService) RefreshToken(ctx context.Context, mailbox *models.Mailbox) error {
	spans, ctx := telemetry.StartServiceSpan(ctx, "GoogleService.RefreshToken")
	defer spans.Finish()
	spans.TagEntity(mailbox.ID)

	// Decrypt refresh token
	refreshToken, err := models.DecryptToken(s.encryptKey, mailbox.OAuthRefreshToken)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to decrypt refresh token: %w", err)
	}

	// Create token from refresh token
	token := &oauth2.Token{
		RefreshToken: refreshToken,
	}

	// Create OAuth2 token source
	tokenSource := s.config.TokenSource(ctx, token)

	// Get new token
	newToken, err := tokenSource.Token()
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	// Encrypt new access token
	encryptedAccessToken, err := models.EncryptToken(s.encryptKey, newToken.AccessToken)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to encrypt access token: %w", err)
	}

	encryptedRefreshToken, err := models.EncryptToken(s.encryptKey, newToken.RefreshToken)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to encrypt refresh token: %w", err)
	}

	// update mailbox with new token
	mailbox.OAuthAccessToken = encryptedAccessToken
	mailbox.OAuthRefreshToken = encryptedRefreshToken
	mailbox.OAuthTokenExpiry = &newToken.Expiry

	// Save to database
	err = s.repositories.MailboxRepository.UpdateOauthToken(ctx, mailbox.ID, encryptedAccessToken, encryptedRefreshToken, &newToken.Expiry)
	if err != nil {
		spans.TraceError(err)
		return fmt.Errorf("failed to update mailbox with new token: %w", err)
	}

	return nil
}

// GetDecryptedAccessToken returns the decrypted access token for the mailbox
func (s *googleService) GetDecryptedAccessToken(ctx context.Context, mailbox *models.Mailbox) (string, error) {
	spans, ctx := telemetry.StartServiceSpan(ctx, "GoogleService.GetDecryptedAccessToken")
	defer spans.Finish()
	spans.TagEntity(mailbox.ID)

	// First check if we need to refresh
	err := s.RefreshTokenIfNeeded(ctx, mailbox)
	if err != nil {
		spans.TraceError(err)
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Decrypt access token
	accessToken, err := models.DecryptToken(s.encryptKey, mailbox.OAuthAccessToken)
	if err != nil {
		spans.TraceError(err)
		return "", fmt.Errorf("failed to decrypt access token: %w", err)
	}

	return accessToken, nil
}
