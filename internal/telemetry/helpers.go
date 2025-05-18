package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/opentracing/opentracing-go/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Types and Constants
type Spans struct {
	OTel trace.Span
}

type contextKey string

const otelSpanKey contextKey = "otel_span"

// Component tag constants
const (
	ComponentGraphQL  = "graphql"
	ComponentPostgres = "postgres"
	ComponentREST     = "rest"
	ComponentService  = "service"
	ComponentListener = "listener"
	ComponentCronJob  = "cron"
)

const (
	SpanTagTenant   = "tenant"
	SpanTagUserId   = "user.id"
	SpanTagEntityId = "entity.id"
)

const (
	componentKey = "component"
)

// SpanKind constants
const (
	SpanKindInternal = "internal"
	SpanKindServer   = "server"
	SpanKindClient   = "client"
	SpanKindConsumer = "consumer"
)

// SpanOptions defines options for span creation
type SpanOptions struct {
	NewRoot bool
}

// WithForceNewTrace returns a SpanOptions that forces creation of a new trace
func WithNewRoot() SpanOptions {
	return SpanOptions{
		NewRoot: true,
	}
}

// Core Span Operations
func StartSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	// Start OpenTelemetry span
	tracer := otel.Tracer("github.com/customeros/mailstack")
	var otelCtx context.Context
	var otelSpan trace.Span
	if len(opts) > 0 && opts[0].NewRoot {
		// Force new trace by using WithNewRoot() option while keeping the context
		otelCtx, otelSpan = tracer.Start(ctx, operationName, trace.WithNewRoot())
	} else {
		otelCtx, otelSpan = tracer.Start(ctx, operationName)
	}
	otelSpan.SetAttributes(attribute.String("operation.name", operationName))

	otelSpan.SetAttributes(
		attribute.String("service.name", "mailstack"),
	)
	if tenant := utils.GetTenantFromContext(ctx); tenant != "" {
		otelSpan.SetAttributes(attribute.String(SpanTagTenant, tenant))
	}
	if userID := utils.GetUserIdFromContext(ctx); userID != "" {
		otelSpan.SetAttributes(attribute.String(SpanTagUserId, userID))
	}

	// Store both spans in the context
	ctx = otelCtx
	ctx = context.WithValue(ctx, otelSpanKey, otelSpan)

	return &Spans{
		OTel: otelSpan,
	}, ctx
}

// Finish ends OpenTelemetry spans
func (s *Spans) Finish() {
	if s == nil {
		return
	}
	FinishSpans(s)
}

// FinishSpans ends OpenTelemetry spans
func FinishSpans(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.End()
	}
}

// Component-specific Span Starters
func StartCronSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	spans, ctx := StartSpan(ctx, operationName, opts...)
	TagComponentCronJob(spans)
	SetSpanKindInternal(spans)
	return spans, ctx
}

func StartGraphQLSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	spans, ctx := StartSpan(ctx, operationName, opts...)
	TagComponentGraphQL(spans)
	SetSpanKindServer(spans)
	return spans, ctx
}

func StartPostgresSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	spans, ctx := StartSpan(ctx, operationName, opts...)
	TagComponentPostgres(spans)
	SetSpanKindDatabase(spans)
	return spans, ctx
}

func StartServiceSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	spans, ctx := StartSpan(ctx, operationName, opts...)
	TagComponentService(spans)
	SetSpanKindInternal(spans)
	return spans, ctx
}

func StartRestSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	spans, ctx := StartSpan(ctx, operationName, opts...)
	TagComponentREST(spans)
	SetSpanKindServer(spans)
	return spans, ctx
}

func StartListenerSpan(ctx context.Context, operationName string, opts ...SpanOptions) (*Spans, context.Context) {
	spans, ctx := StartSpan(ctx, operationName, opts...)
	TagComponentListener(spans)
	SetSpanKindConsumer(spans)
	return spans, ctx
}

// Component Tagging Helpers
func TagComponentGraphQL(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentGraphQL))
	}
}

func TagComponentPostgres(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentPostgres))
	}
}

func TagComponentREST(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentREST))
	}
}

func TagComponentService(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentService))
	}
}

func TagComponentListener(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentListener))
	}
}

func TagComponentCronJob(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentCronJob))
	}
}

// Span Kind Helpers
func SetSpanKindInternal(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindInternal))
}

func SetSpanKindServer(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindServer))
}

func SetSpanKindConsumer(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindConsumer))
}

func SetSpanKindDatabase(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindClient))
}

// Tagging Methods
func (s *Spans) TagString(key, value string) {
	if s == nil {
		return
	}
	if key == "" {
		return
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.String(key, value))
	}
}

func (s *Spans) TagInt(key string, value int) {
	if s == nil {
		return
	}
	if key == "" {
		return
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.Int(key, value))
	}
}

func (s *Spans) TagUint32(key string, value uint32) {
	if s == nil {
		return
	}
	if key == "" {
		return
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.Int64(key, int64(value)))
	}
}

func (s *Spans) TagBool(key string, value bool) {
	if s == nil {
		return
	}
	if key == "" {
		return
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.Bool(key, value))
	}
}

func (s *Spans) TagStringSlice(key string, value []string) {
	if s == nil {
		return
	}
	if key == "" {
		return
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.StringSlice(key, value))
	}
}

func (s *Spans) TagEntity(entityId string) {
	if s == nil || entityId == "" {
		return
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.String(SpanTagEntityId, entityId))
	}
}

// Logging Methods
func (s *Spans) LogFields(fields ...log.Field) {
	if s == nil {
		return
	}
	// Log to OpenTelemetry as events
	if s.OTel != nil {
		// Group fields by event type
		eventFields := make(map[string][]attribute.KeyValue)
		eventType := "log" // default event type

		for _, field := range fields {
			key := field.Key()
			value := field.Value()

			// Check if this is an event type field
			if key == "event" {
				if str, ok := value.(string); ok {
					eventType = str
				}
				continue
			}

			// Convert field to attribute
			var attr attribute.KeyValue
			switch v := value.(type) {
			case string:
				attr = attribute.String(key, v)
			case int:
				attr = attribute.Int(key, v)
			case bool:
				attr = attribute.Bool(key, v)
			case float64:
				attr = attribute.Float64(key, v)
			case error:
				attr = attribute.String(key, v.Error())
			default:
				attr = attribute.String(key, fmt.Sprintf("%v", v))
			}
			eventFields[eventType] = append(eventFields[eventType], attr)
		}

		// Add each event type as a separate event
		for eventType, attrs := range eventFields {
			s.OTel.AddEvent(eventType, trace.WithAttributes(attrs...))
		}
	}
}

func (s *Spans) LogKV(alternatingKeyValues ...interface{}) {
	if s == nil {
		return
	}

	// Log to OpenTelemetry
	if s.OTel != nil {
		attrs := make([]attribute.KeyValue, 0, len(alternatingKeyValues)/2)
		for i := 0; i < len(alternatingKeyValues); i += 2 {
			if i+1 >= len(alternatingKeyValues) {
				break
			}
			key, ok := alternatingKeyValues[i].(string)
			if !ok {
				continue
			}
			value := alternatingKeyValues[i+1]
			switch v := value.(type) {
			case string:
				attrs = append(attrs, attribute.String(key, v))
			case int:
				attrs = append(attrs, attribute.Int(key, v))
			case bool:
				attrs = append(attrs, attribute.Bool(key, v))
			case float64:
				attrs = append(attrs, attribute.Float64(key, v))
			case error:
				attrs = append(attrs, attribute.String(key, v.Error()))
			default:
				// For unknown types, try to marshal as JSON
				if jsonBytes, err := json.Marshal(v); err == nil {
					attrs = append(attrs, attribute.String(key, string(jsonBytes)))
				} else {
					attrs = append(attrs, attribute.String(key, fmt.Sprintf("%v", v)))
				}
			}
		}
		s.OTel.AddEvent("log", trace.WithAttributes(attrs...))
	}
}

func (s *Spans) LogObjectAsJson(key string, obj interface{}) {
	if s == nil {
		return
	}

	// Log to OpenTelemetry
	if s.OTel != nil {
		jsonBytes, err := json.Marshal(obj)
		if err != nil {
			s.OTel.AddEvent("log", trace.WithAttributes(
				attribute.String("error", fmt.Sprintf("failed to marshal object to JSON: %v", err)),
			))
			return
		}
		s.OTel.AddEvent("log", trace.WithAttributes(
			attribute.String(key, string(jsonBytes)),
		))
	}
}

func LogInfo(ctx context.Context, msg string, fields ...log.Field) {
	// Log to OpenTelemetry
	if otelSpan, ok := ctx.Value(otelSpanKey).(trace.Span); ok {
		otelSpan.SetStatus(codes.Ok, msg)
		// Add event with message and time
		otelSpan.AddEvent("info", trace.WithAttributes(
			attribute.String("message", msg),
			attribute.String("time", time.Now().Format(time.RFC3339)),
		))
	}
}

func (s *Spans) TraceError(err error) {
	if s == nil || err == nil {
		return
	}

	// Trace error in OpenTelemetry
	if s.OTel != nil {
		s.OTel.RecordError(err)
		s.OTel.SetStatus(codes.Error, err.Error())
		s.OTel.SetAttributes(
			attribute.String("event", "error"),
			attribute.String("time", time.Now().Format(time.RFC3339)),
		)
	}
}

func Recover(spans *Spans, logger logger.Logger) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())

		// Log to OpenTelemetry
		if spans != nil && spans.OTel != nil {
			spans.OTel.RecordError(fmt.Errorf("panic: %v", r))
			spans.OTel.SetStatus(codes.Error, fmt.Sprintf("panic: %v", r))
			spans.OTel.SetAttributes(
				attribute.String("event", "panic"),
				attribute.String("error", fmt.Sprintf("%v", r)),
				attribute.String("time", time.Now().Format(time.RFC3339)),
			)
			// Log stack trace as an event
			spans.OTel.AddEvent("panic.stack", trace.WithAttributes(
				attribute.String("stack", stack),
			))
		}

		// Log to logger
		if logger != nil {
			logger.Errorf("Recovered from panic: %v\nStack trace:\n%s", r, stack)
		}

		// Do not re-panic - allow the application to continue running
	}
}

// GetDefaultServiceSpanAttributes returns default attributes for service spans
func GetDefaultServiceSpanAttributes(ctx context.Context) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("service.name", "mailstack"),
	}

	if tenant := utils.GetTenantFromContext(ctx); tenant != "" {
		attrs = append(attrs, attribute.String(SpanTagTenant, tenant))
	}
	if userID := utils.GetUserIdFromContext(ctx); userID != "" {
		attrs = append(attrs, attribute.String("user_id", userID))
	}
	if userEmail := utils.GetUserEmailFromContext(ctx); userEmail != "" {
		attrs = append(attrs, attribute.String("user_email", userEmail))
	}

	return attrs
}

// SetDefaultServiceSpanAttributes sets default attributes on a span
func SetDefaultServiceSpanAttributes(ctx context.Context, span trace.Span) {
	if span == nil {
		return
	}
	span.SetAttributes(GetDefaultServiceSpanAttributes(ctx)...)
}

// RecoveryWithTelemetry creates a gin middleware that recovers from panics and logs them to both Jaeger and OpenTelemetry
func RecoveryWithTelemetry(log logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				// Get the current span from context or create new one
				tracer := otel.Tracer("github.com/customeros/mailstack")
				ctx := c.Request.Context()

				// Start a new span as a child of the current trace if it exists
				_, span := tracer.Start(ctx, "panic-recovery")
				defer span.End()

				// Get detailed stack trace
				stack := string(debug.Stack())

				// Log to OpenTelemetry
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, fmt.Sprintf("panic: %v", r))
				span.SetAttributes(
					attribute.String("event", "panic"),
					attribute.String("error", fmt.Sprintf("%v", r)),
					attribute.String("time", time.Now().Format(time.RFC3339)),
					attribute.String("http.url", c.Request.URL.String()),
					attribute.String("http.method", c.Request.Method),
					attribute.Int("http.status_code", 500),
				)
				// Log stack trace as an event
				span.AddEvent("panic.stack", trace.WithAttributes(
					attribute.String("stack", stack),
				))

				// Log to application logger
				log.Errorf("[Panic Recovery] Error: %v\nStack trace:\n%s\nPath: %s\nMethod: %s",
					r,
					stack,
					c.Request.URL.Path,
					c.Request.Method,
				)

				// Let the chain continue to allow other recovery handlers to process the panic
				panic(r)
			}
		}()
		c.Next()
	}
}
