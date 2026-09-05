package httptransport

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
)

const actorContextKey = "actor_identity"

func actorMiddleware(actorUserID int64) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Set(actorContextKey, actorContext{UserID: actorUserID})
		c.Next(ctx)
	}
}

func currentActor(c *app.RequestContext) (actorContext, error) {
	value, ok := c.Get(actorContextKey)
	if !ok {
		return actorContext{}, errors.New("default actor is unavailable")
	}
	identity, ok := value.(actorContext)
	if !ok || identity.UserID < 1 {
		return actorContext{}, errors.New("default actor is invalid")
	}
	return identity, nil
}
