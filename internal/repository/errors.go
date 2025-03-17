package repository

import "errors"

var (
	ErrSenderNotFound      = errors.New("sender not found")
	ErrEmailNotFound       = errors.New("email not found")
	ErrSenderAlreadyExists = errors.New("sender already exists")
	ErrInvalidInput        = errors.New("invalid input parameters")
)
