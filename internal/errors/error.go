package errors

import "github.com/pkg/errors"

var (
	// common errors
	ErrTenantMissing     = errors.New("tenant is missing")
	ErrConnectionTimeout = errors.New("connection timeout")

	// domain errors
	ErrDomainNotFound            = errors.New("domain not found")
	ErrDomainConfigurationFailed = errors.New("domain configuration failed")

	// mailbox errors
	ErrMailboxExists           = errors.New("mailbox already exists")
	ErrMailboxNotFound         = errors.New("mailbox not found")
	ErrMailboxNotOwnedByTenant = errors.New("mailbox does not belong to tenant")
)
