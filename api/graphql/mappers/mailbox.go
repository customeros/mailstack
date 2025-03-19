package mappers

import (
	"time"

	"github.com/customeros/mailsherpa/mailvalidate"

	"github.com/customeros/mailstack/api/graphql/graphql_model"
	"github.com/customeros/mailstack/internal/enum"
	"github.com/customeros/mailstack/internal/models"
)

func MapGormMailboxToGraph(mailbox *models.Mailbox) *graphql_model.Mailbox {
	return &graphql_model.Mailbox{
		ID:                     mailbox.ID,
		Provider:               mailbox.Provider,
		EmailAddress:           mailbox.EmailAddress,
		SenderID:               &mailbox.SenderID,
		InboundEnabled:         mailbox.InboundEnabled,
		OutboundEnabled:        mailbox.OutboundEnabled,
		ReplyToAddress:         &mailbox.ReplyToAddress,
		ConnectionStatus:       mailbox.ConnectionStatus,
		LastConnectionCheck:    *mailbox.LastConnectionCheck,
		ConnectionErrorMessage: &mailbox.ErrorMessage,
	}
}

func MapGraphMailboxInputToGorm(input *graphql_model.MailboxInput) *models.Mailbox {
	// Initialize a new GORM mailbox
	gormMailbox := &models.Mailbox{
		Provider:        input.Provider,
		EmailAddress:    input.EmailAddress,
		InboundEnabled:  *input.InboundEnabled,
		OutboundEnabled: *input.OutboundEnabled,
	}

	// Split email into user and domain parts
	validateEmail := mailvalidate.ValidateEmailSyntax(input.EmailAddress)
	gormMailbox.MailboxUser = validateEmail.User
	gormMailbox.MailboxDomain = validateEmail.Domain

	// Handle optional fields with nil checks
	if input.SenderID != nil {
		gormMailbox.SenderID = *input.SenderID
	}

	if input.ReplyToAddress != nil {
		gormMailbox.ReplyToAddress = *input.ReplyToAddress
	}

	gormMailbox.ConnectionStatus = enum.ConnectionNotActive
	now := time.Now()
	gormMailbox.LastConnectionCheck = &now

	// Map IMAP configuration if provided
	if input.ImapConfig != nil {
		if input.ImapConfig.ImapServer != nil {
			gormMailbox.ImapServer = *input.ImapConfig.ImapServer
		}
		if input.ImapConfig.ImapPort != nil {
			gormMailbox.ImapPort = *input.ImapConfig.ImapPort
		}
		if input.ImapConfig.ImapUsername != nil {
			gormMailbox.ImapUsername = *input.ImapConfig.ImapUsername
		}
		if input.ImapConfig.ImapPassword != nil {
			gormMailbox.ImapPassword = *input.ImapConfig.ImapPassword
		}
		if input.ImapConfig.ImapSecurity != nil {
			gormMailbox.ImapSecurity = *input.ImapConfig.ImapSecurity
		}
	}

	// Map SMTP configuration if provided
	if input.SMTPConfig != nil {
		if input.SMTPConfig.SMTPServer != nil {
			gormMailbox.SmtpServer = *input.SMTPConfig.SMTPServer
		}
		if input.SMTPConfig.SMTPPort != nil {
			gormMailbox.SmtpPort = *input.SMTPConfig.SMTPPort
		}
		if input.SMTPConfig.SMTPUsername != nil {
			gormMailbox.SmtpUsername = *input.SMTPConfig.SMTPUsername
		}
		if input.SMTPConfig.SMTPPassword != nil {
			gormMailbox.SmtpPassword = *input.SMTPConfig.SMTPPassword
		}
		if input.SMTPConfig.SMTPSecurity != nil {
			gormMailbox.SmtpSecurity = *input.SMTPConfig.SMTPSecurity
		}
	}

	// Map sync folders if provided
	if input.SyncFolders != nil && len(input.SyncFolders) > 0 {
		// Convert []*string to []string for pq.StringArray
		syncFolders := make([]string, 0, len(input.SyncFolders))
		for _, folder := range input.SyncFolders {
			if folder != nil {
				syncFolders = append(syncFolders, *folder)
			}
		}
		gormMailbox.SyncFolders = syncFolders
	}

	return gormMailbox
}
