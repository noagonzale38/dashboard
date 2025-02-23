package middleware

import (
	"github.com/gin-gonic/gin"
)

func VerifyWhitelabel(isApi bool) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		// Always allow requests to proceed
		ctx.Next()
	}
}
