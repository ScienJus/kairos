package identity

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Authenticator resolves bearer credentials into identities.
type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
}

// AuthenticatedResolver resolves identities exclusively from Bearer tokens.
// Trusted identity headers are intentionally ignored.
type AuthenticatedResolver struct {
	Authenticator Authenticator
}

// Resolve authenticates the request Authorization header.
func (r AuthenticatedResolver) Resolve(request *http.Request) (Identity, error) {
	if r.Authenticator == nil {
		return Identity{}, fmt.Errorf("%w: authenticator is not configured", ErrUnauthenticated)
	}
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Identity{}, fmt.Errorf("%w: Authorization Bearer token is required", ErrUnauthenticated)
	}
	return r.Authenticator.Authenticate(request.Context(), parts[1])
}
