// api/graphql/resolver/resolver.go
package resolver

import (
	"github.com/customeros/mailstack/api/graphql/generated"
	"github.com/customeros/mailstack/internal/repository"
	"github.com/customeros/mailstack/services"
)

// Resolver is the base resolver structure
type Resolver struct {
	repositories *repository.Repositories
	services     *services.Services
}

func NewResolver(repos *repository.Repositories, services *services.Services) *Resolver {
	return &Resolver{
		repositories: repos,
		services:     services,
	}
}

// Query returns the QueryResolver implementation
func (r *Resolver) Query() generated.QueryResolver {
	return &queryResolver{r}
}

func (r *Resolver) Mutation() generated.MutationResolver {
	return &mutationResolver{r}
}

type mutationResolver struct {
	*Resolver
}

type (
	queryResolver struct{ *Resolver }
)
