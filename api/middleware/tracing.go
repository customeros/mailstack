package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go/log"

	"github.com/customeros/mailstack/internal/tracing"
)

// TracingMiddleware creates a new span for each request and adds common tags
func TracingMiddleware(parentCtx context.Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get existing custom context if any
		existingCtx := c.Request.Context()

		// Start span using existing utility with parent context
		ctx, span := tracing.StartHttpServerTracerSpanWithHeader(
			existingCtx, // Use existing context instead of parentCtx to preserve values
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

		// Store span in context while preserving existing context values
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Add response status
		if c.Writer.Status() >= 400 {
			tracing.TraceErr(span, nil, log.String("event", "error"))
		}
	}
}
