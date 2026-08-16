package mcpapi

import (
	"context"
	"net/http"

	"github.com/ScienJus/kairos/internal/identity"
)

type identityContextKey struct{}

func withIdentity(ctx context.Context, actor identity.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, actor)
}

func identityFromContext(request *http.Request) (identity.Identity, bool) {
	actor, ok := request.Context().Value(identityContextKey{}).(identity.Identity)
	return actor, ok
}
