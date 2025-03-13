package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go/log"

	"github.com/customeros/mailstack/internal/tracing"
)

// TracingMiddleware creates a new span for each request and adds common tags
func TracingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start span using existing utility
		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(
			c.Request.Context(),
			c.Request.Method+" "+c.FullPath(),
			c.Request.Header,
		)
		defer span.Finish()

		// Tag as REST component
		tracing.TagComponentRest(span)

		// Set default span tags (tenant, user-id, user-email)
		tracing.SetDefaultServiceSpanTags(ctx, span)

		// Add entity ID if present in URL params
		if id := c.Param("id"); id != "" {
			tracing.TagEntity(span, id)
		}

		// Store span in context
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Add response status
		if c.Writer.Status() >= 400 {
			tracing.TraceErr(span, nil, log.String("event", "error"))
		}
	}
}
