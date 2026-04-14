package middleware

import (
	"crypto/subtle"

	"github.com/TicketsBot-cloud/dashboard/config"
	"github.com/TicketsBot-cloud/dashboard/utils"
	"github.com/gin-gonic/gin"
)

// ApiKeyAuth authenticates external API requests using a shared API key
// provided via the X-Api-Key header. This is separate from the JWT-based
// dashboard authentication and is intended for trusted external callers.
func ApiKeyAuth(ctx *gin.Context) {
	expected := config.Conf.Server.ExternalApiKey
	if expected == "" {
		ctx.AbortWithStatusJSON(503, utils.ErrorStr("External API is not configured"))
		return
	}

	provided := ctx.GetHeader("X-Api-Key")
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		ctx.AbortWithStatusJSON(401, utils.ErrorStr("Invalid or missing API key"))
		return
	}
}
