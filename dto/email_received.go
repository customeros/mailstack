package dto

import (
	"github.com/customeros/mailstack/internal/enum"
)

type EmailReceived struct {
	Source      enum.EmailImportSource
	InitialSync bool
	MailboxID   string
	Folder      string
	ImapUID     uint32
	ImapSeqNum  uint32
}
