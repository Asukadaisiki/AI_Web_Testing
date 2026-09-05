package authn

import (
	"context"
	"errors"
)

var ErrUnauthenticated = errors.New("authentication required")

type Identity struct {
	UserID      int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Cookie      string `json:"-"`
}

type Authenticator interface {
	Authenticate(ctx context.Context, cookieHeader string) (Identity, error)
}

type identityContextKey struct{}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok && identity.UserID > 0
}
