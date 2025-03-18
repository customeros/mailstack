// api/graphql/resolver/resolver.go
package resolver

import (
	"github.com/customeros/mailstack/api/graphql/generated"
	"github.com/customeros/mailstack/internal/repository"
)

// Resolver is the base resolver structure
type Resolver struct {
	repositories *repository.Repositories
}

func NewResolver(repos *repository.Repositories) *Resolver {
	return &Resolver{
		repositories: repos,
	}
}

// Query returns the QueryResolver implementation
func (r *Resolver) Query() generated.QueryResolver {
	return &queryResolver{r}
}

type (
	queryResolver struct{ *Resolver }
)
