package dto

import (
	"github.com/emersion/go-imap"

	"github.com/customeros/mailstack/internal/enum"
)

type ProcessEmail struct {
	Source        enum.EmailProvider
	InitialSync   bool
	MailboxID     string
	Folder        string
	ImapMessageID uint32
	ImapMessage   *imap.Message
}
