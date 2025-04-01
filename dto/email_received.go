package dto

import (
	"github.com/customeros/mailstack/internal/enum"
)

type EmailReceivedIMAP struct {
	Source      enum.EmailImportSource `json:"source"`
	InitialSync bool                   `json:"initialSync"`
	MailboxID   string                 `json:"mailboxId"`
	Folder      string                 `json:"folder"`
	ImapUID     uint32                 `json:"imapUID"`
	ImapSeqNum  uint32                 `json:"imapSeqNum"`
}
