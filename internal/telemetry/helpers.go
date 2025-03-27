package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/customeros/mailstack/internal/utils"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StartSpan starts a new span with the given operation name and returns the spans and context
// This will create spans for both Jaeger and OpenTelemetry
func StartSpan(ctx context.Context, operationName string) (opentracing.Span, trace.Span, context.Context) {
	// Start Jaeger span
	jaegerSpan, ctx := opentracing.StartSpanFromContext(ctx, operationName)
	jaegerSpan.SetTag("service.name", "mailstack")
	if tenant := utils.GetTenantFromContext(ctx); tenant != "" {
		jaegerSpan.SetTag("tenant", tenant)
	}
	if userID := utils.GetUserIdFromContext(ctx); userID != "" {
		jaegerSpan.SetTag("user_id", userID)
	}

	// Start OpenTelemetry span
	tracer := otel.Tracer("mailstack")
	otelCtx, otelSpan := tracer.Start(ctx, operationName)
	otelSpan.SetAttributes(
		attribute.String("service.name", "mailstack"),
	)
	if tenant := utils.GetTenantFromContext(ctx); tenant != "" {
		otelSpan.SetAttributes(attribute.String("tenant", tenant))
	}
	if userID := utils.GetUserIdFromContext(ctx); userID != "" {
		otelSpan.SetAttributes(attribute.String("user_id", userID))
	}

	// Store both spans in the context
	ctx = otelCtx
	ctx = context.WithValue(ctx, "otel_span", otelSpan)
	return jaegerSpan, otelSpan, ctx
}

// FinishSpans finishes both Jaeger and OpenTelemetry spans
func FinishSpans(jaegerSpan opentracing.Span, otelSpan trace.Span) {
	if jaegerSpan != nil {
		jaegerSpan.Finish()
	}
	if otelSpan != nil {
		otelSpan.End()
	}
}

// LogError logs an error with context and additional fields for both systems
func LogError(ctx context.Context, err error, fields ...log.Field) {
	// Log to Jaeger
	jaegerSpan := opentracing.SpanFromContext(ctx)
	if jaegerSpan != nil {
		// Add standard error fields
		errorFields := []log.Field{
			log.Error(err),
			log.String("event", "error"),
			log.String("time", time.Now().Format(time.RFC3339)),
		}

		// Add OpenTelemetry specific fields
		otelFields := []log.Field{
			log.String("otel.status_code", "error"),
			log.String("otel.status_message", err.Error()),
		}

		// Combine all fields
		allFields := append(errorFields, append(otelFields, fields...)...)
		jaegerSpan.LogFields(allFields...)
	}

	// Log to OpenTelemetry
	if otelSpan, ok := ctx.Value("otel_span").(trace.Span); ok {
		otelSpan.RecordError(err)
		otelSpan.SetStatus(codes.Error, err.Error())
		otelSpan.SetAttributes(
			attribute.String("event", "error"),
			attribute.String("time", time.Now().Format(time.RFC3339)),
		)
	}
}

// LogInfo logs an info message with context and additional fields for both systems
func LogInfo(ctx context.Context, msg string, fields ...log.Field) {
	// Log to Jaeger
	jaegerSpan := opentracing.SpanFromContext(ctx)
	if jaegerSpan != nil {
		// Add standard info fields
		infoFields := []log.Field{
			log.String("event", "info"),
			log.String("message", msg),
			log.String("time", time.Now().Format(time.RFC3339)),
		}

		// Add OpenTelemetry specific fields
		otelFields := []log.Field{
			log.String("otel.status_code", "ok"),
		}

		// Combine all fields
		allFields := append(infoFields, append(otelFields, fields...)...)
		jaegerSpan.LogFields(allFields...)
	}

	// Log to OpenTelemetry
	if otelSpan, ok := ctx.Value("otel_span").(trace.Span); ok {
		otelSpan.SetStatus(codes.Ok, msg)
		otelSpan.SetAttributes(
			attribute.String("event", "info"),
			attribute.String("message", msg),
			attribute.String("time", time.Now().Format(time.RFC3339)),
		)
	}
}

// LogDebug logs a debug message with context and additional fields for both systems
func LogDebug(ctx context.Context, msg string, fields ...log.Field) {
	// Log to Jaeger
	jaegerSpan := opentracing.SpanFromContext(ctx)
	if jaegerSpan != nil {
		// Add standard debug fields
		debugFields := []log.Field{
			log.String("event", "debug"),
			log.String("message", msg),
			log.String("time", time.Now().Format(time.RFC3339)),
		}

		// Add OpenTelemetry specific fields
		otelFields := []log.Field{
			log.String("otel.status_code", "ok"),
		}

		// Combine all fields
		allFields := append(debugFields, append(otelFields, fields...)...)
		jaegerSpan.LogFields(allFields...)
	}

	// Log to OpenTelemetry
	if otelSpan, ok := ctx.Value("otel_span").(trace.Span); ok {
		otelSpan.SetAttributes(
			attribute.String("event", "debug"),
			attribute.String("message", msg),
			attribute.String("time", time.Now().Format(time.RFC3339)),
		)
	}
}

// TagError adds error information to a span for both systems
func TagError(span opentracing.Span, err error) {
	if err != nil {
		// Add standard error tags
		span.SetTag("error", true)

		// Add OpenTelemetry specific tags
		span.SetTag("otel.status_code", "error")
		span.SetTag("otel.status_message", err.Error())

		span.LogFields(
			log.Error(err),
			log.String("event", "error"),
			log.String("time", time.Now().Format(time.RFC3339)),
		)
	}
}

// TagString adds a string tag to a span for both systems
func TagString(span opentracing.Span, key, value string) {
	span.SetTag(key, value)
	// Add OpenTelemetry specific prefix for certain keys
	if key == "component" || key == "service.name" || key == "tenant" {
		span.SetTag("otel."+key, value)
	}
}

// TagInt adds an integer tag to a span for both systems
func TagInt(span opentracing.Span, key string, value int) {
	span.SetTag(key, value)
}

// TagBool adds a boolean tag to a span for both systems
func TagBool(span opentracing.Span, key string, value bool) {
	span.SetTag(key, value)
}

// TagStringSlice adds a string slice tag to a span for both systems
func TagStringSlice(span opentracing.Span, key string, value []string) {
	span.SetTag(key, fmt.Sprintf("%v", value))
}

// Component tag constants
const (
	ComponentGraphQL  = "graphql"
	ComponentPostgres = "postgres"
	ComponentREST     = "rest"
	ComponentService  = "service"
	ComponentListener = "listener"
	ComponentCronJob  = "cronJob"
)

const (
	componentKey = "component"
)

// Component tagging helpers
func TagComponentGraphQL(span opentracing.Span) {
	span.SetTag(componentKey, ComponentGraphQL)
}

func TagComponentPostgres(span opentracing.Span) {
	span.SetTag(componentKey, ComponentPostgres)
}

func TagComponentREST(span opentracing.Span) {
	span.SetTag(componentKey, ComponentREST)
}

func TagComponentService(span opentracing.Span) {
	span.SetTag(componentKey, ComponentService)
}

func TagComponentListener(span opentracing.Span) {
	span.SetTag(componentKey, ComponentListener)
}

func TagComponentCronJob(span opentracing.Span) {
	span.SetTag(componentKey, ComponentCronJob)
}

// SpanKind constants
const (
	SpanKindInternal = "internal"
	SpanKindServer   = "server"
	SpanKindClient   = "client"
	SpanKindProducer = "producer"
	SpanKindConsumer = "consumer"
)

// SetSpanKindInternal sets the span kind to internal
func SetSpanKindInternal(span interface{}) {
	switch s := span.(type) {
	case trace.Span:
		s.SetAttributes(attribute.String("span.kind", SpanKindInternal))
	case opentracing.Span:
		s.SetTag("span.kind", SpanKindInternal)
	}
}

// SetSpanKindServer sets the span kind to server
func SetSpanKindServer(span interface{}) {
	switch s := span.(type) {
	case trace.Span:
		s.SetAttributes(attribute.String("span.kind", SpanKindServer))
	case opentracing.Span:
		s.SetTag("span.kind", SpanKindServer)
	}
}

// SetSpanKindClient sets the span kind to client
func SetSpanKindClient(span interface{}) {
	switch s := span.(type) {
	case trace.Span:
		s.SetAttributes(attribute.String("span.kind", SpanKindClient))
	case opentracing.Span:
		s.SetTag("span.kind", SpanKindClient)
	}
}

// SetSpanKindProducer sets the span kind to producer
func SetSpanKindProducer(span interface{}) {
	switch s := span.(type) {
	case trace.Span:
		s.SetAttributes(attribute.String("span.kind", SpanKindProducer))
	case opentracing.Span:
		s.SetTag("span.kind", SpanKindProducer)
	}
}

// SetSpanKindConsumer sets the span kind to consumer
func SetSpanKindConsumer(span interface{}) {
	switch s := span.(type) {
	case trace.Span:
		s.SetAttributes(attribute.String("span.kind", SpanKindConsumer))
	case opentracing.Span:
		s.SetTag("span.kind", SpanKindConsumer)
	}
}
