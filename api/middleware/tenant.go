package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func TenantValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant := c.GetHeader("tenant")
		if tenant == "" {
			tenant = c.GetHeader("Tenant")
		}
		if tenant == "" {
			tenant = c.GetHeader("TENANT")
		}
		if tenant == "" {
			tenant = c.GetHeader("tenantname")
		}
		if tenant == "" {
			tenant = c.GetHeader("TenantName")
		}
		if tenant == "" {
			tenant = c.GetHeader("tenantName")
		}
		if tenant == "" {
			tenant = c.GetHeader("TENANTNAME")
		}

		if tenant == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tenant header is required"})
			c.Abort()
			return
		}

		// Store in gin context for later use
		c.Set("tenant", tenant)
		c.Next()
	}
}
