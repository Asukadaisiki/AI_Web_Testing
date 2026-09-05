package httptransport

import (
	"context"
	"errors"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/authn"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const identityContextKey = "authenticated_identity"

func authenticationMiddleware(authenticator authn.Authenticator) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		identity, err := authenticator.Authenticate(ctx, string(c.GetHeader("Cookie")))
		if err != nil {
			if errors.Is(err, authn.ErrUnauthenticated) {
				writeError(c, consts.StatusUnauthorized, authn.ErrUnauthenticated)
			} else {
				writeError(c, consts.StatusBadGateway, err)
			}
			c.Abort()
			return
		}
		c.Set(identityContextKey, identity)
		c.Next(authn.WithIdentity(ctx, identity))
	}
}

func currentIdentity(c *app.RequestContext) (authn.Identity, error) {
	value, ok := c.Get(identityContextKey)
	if !ok {
		return authn.Identity{}, authn.ErrUnauthenticated
	}
	identity, ok := value.(authn.Identity)
	if !ok || identity.UserID < 1 {
		return authn.Identity{}, authn.ErrUnauthenticated
	}
	return identity, nil
}
