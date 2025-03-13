package middleware

import (
	"github.com/customeros/mailstack/internal/utils"
	"github.com/gin-gonic/gin"
)

// CustomContextMiddleware adds custom context to all requests
func CustomContextMiddleware(appSource string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := utils.WithCustomContextFromGinRequest(c, appSource)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
