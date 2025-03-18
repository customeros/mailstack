package interfaces

import (
	"golang.org/x/net/context"

	"github.com/customeros/mailstack/dto"
)

type AIService interface {
	GetStructuredEmailBody(ctx context.Context, request dto.StructuredEmailRequest) (*dto.StructuredEmailResponse, error)
}
