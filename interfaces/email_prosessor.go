package interfaces

import (
	"context"
)

type EmailProcessor interface {
	Start(ctx context.Context) error
	Close() error
}
