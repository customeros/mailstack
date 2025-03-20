package mailstack_errors

import "errors"

var (
	ErrTenantNotSet = errors.New("tenant not set on context")
	ErrUserIDNotSet = errors.New("userId not set on context")
)
