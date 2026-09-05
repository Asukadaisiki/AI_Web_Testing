package httptransport

import (
	"context"
	"strconv"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/corrections"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handler) createCorrection(ctx context.Context, c *app.RequestContext) {
	identity, err := currentIdentity(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var request corrections.CreateRequest
	if err := c.BindAndValidate(&request); err != nil {
		writeError(c, consts.StatusBadRequest, err)
		return
	}
	result, err := h.corrections.Create(ctx, identity.UserID, request)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Location", "/api/v2/corrections/"+strconv.FormatInt(result["id"].(int64), 10))
	c.JSON(consts.StatusCreated, result)
}
