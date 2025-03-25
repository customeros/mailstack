package mailboxold

import (
	"github.com/customeros/mailstack/interfaces"
	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/repository"
)

type mailboxServiceOld struct {
	log            logger.Logger
	postgres       *repository.Repositories
	openSrsService interfaces.OpenSrsService
}

func NewMailboxServiceOld(log logger.Logger, postgres *repository.Repositories, openSrsService interfaces.OpenSrsService) interfaces.MailboxServiceOld {
	return &mailboxServiceOld{
		log:            log,
		postgres:       postgres,
		openSrsService: openSrsService,
	}
}
