package identity

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

const (
	HeaderActorID   = "X-Kairos-Actor-Id"
	HeaderActorKind = "X-Kairos-Actor-Kind"
	HeaderActorRole = "X-Kairos-Actor-Role"
)

// Resolver converts an inbound HTTP request into a trusted identity.
type Resolver interface {
	Resolve(*http.Request) (Identity, error)
}

// TrustedResolver accepts identity attributes directly from HTTP headers. It
// must only be used when the caller and transport are inside the trust boundary.
type TrustedResolver struct{}

// Resolve parses the trusted identity headers. Actor kind defaults to agent.
func (TrustedResolver) Resolve(request *http.Request) (Identity, error) {
	id := strings.TrimSpace(request.Header.Get(HeaderActorID))
	if id == "" {
		return Identity{}, fmt.Errorf("%w: %s header is required", ErrUnauthenticated, HeaderActorID)
	}
	kind := domain.ActorKind(strings.TrimSpace(request.Header.Get(HeaderActorKind)))
	if kind == "" {
		kind = domain.ActorAgent
	}
	identity := Identity{
		Actor: domain.ActorRef{Kind: kind, ID: domain.ActorID(id)},
		Role:  strings.TrimSpace(request.Header.Get(HeaderActorRole)),
	}
	if err := identity.Validate(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}
