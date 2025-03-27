package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/customeros/mailstack/internal/tracing"
	"github.com/customeros/mailstack/internal/utils"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Spans wraps both Jaeger and OpenTelemetry spans
type Spans struct {
	Jaeger opentracing.Span
	OTel   trace.Span
}

// StartSpan starts a new span with the given operation name and returns the spans and context
// This will create spans for both Jaeger and OpenTelemetry
func StartSpan(ctx context.Context, operationName string) (*Spans, context.Context) {
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

	return &Spans{
		Jaeger: jaegerSpan,
		OTel:   otelSpan,
	}, ctx
}

// FinishSpans finishes both Jaeger and OpenTelemetry spans
func FinishSpans(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.Jaeger != nil {
		spans.Jaeger.Finish()
	}
	if spans.OTel != nil {
		spans.OTel.End()
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

// TagString adds a string tag/attribute to both spans
func (s *Spans) TagString(key, value string) {
	if s == nil {
		return
	}
	if s.Jaeger != nil {
		s.Jaeger.SetTag(key, value)
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.String(key, value))
	}
}

// TagInt adds an integer tag/attribute to both spans
func (s *Spans) TagInt(key string, value int) {
	if s == nil {
		return
	}
	if s.Jaeger != nil {
		s.Jaeger.SetTag(key, value)
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.Int(key, value))
	}
}

// TagBool adds a boolean tag/attribute to both spans
func (s *Spans) TagBool(key string, value bool) {
	if s == nil {
		return
	}
	if s.Jaeger != nil {
		s.Jaeger.SetTag(key, value)
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.Bool(key, value))
	}
}

// TagStringSlice adds a string slice tag/attribute to both spans
func (s *Spans) TagStringSlice(key string, value []string) {
	if s == nil {
		return
	}
	if s.Jaeger != nil {
		s.Jaeger.SetTag(key, fmt.Sprintf("%v", value))
	}
	if s.OTel != nil {
		s.OTel.SetAttributes(attribute.StringSlice(key, value))
	}
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
func TagComponentGraphQL(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.Jaeger != nil {
		spans.Jaeger.SetTag(componentKey, ComponentGraphQL)
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentGraphQL))
	}
}

func TagComponentPostgres(spans *Spans) {
	if spans == nil {
		return
	}
	SetSpanKindDatabase(spans)
	if spans.Jaeger != nil {
		spans.Jaeger.SetTag(componentKey, ComponentPostgres)
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentPostgres))
	}
}

func TagComponentREST(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.Jaeger != nil {
		spans.Jaeger.SetTag(componentKey, ComponentREST)
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentREST))
	}
}

func TagComponentService(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.Jaeger != nil {
		spans.Jaeger.SetTag(componentKey, ComponentService)
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentService))
	}
}

func TagComponentListener(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.Jaeger != nil {
		spans.Jaeger.SetTag(componentKey, ComponentListener)
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentListener))
	}
}

func TagComponentCronJob(spans *Spans) {
	if spans == nil {
		return
	}
	if spans.Jaeger != nil {
		spans.Jaeger.SetTag(componentKey, ComponentCronJob)
	}
	if spans.OTel != nil {
		spans.OTel.SetAttributes(attribute.String(componentKey, ComponentCronJob))
	}
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
func SetSpanKindInternal(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindInternal))
}

// SetSpanKindServer sets the span kind to server
func SetSpanKindServer(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindServer))
}

// SetSpanKindClient sets the span kind to client
func SetSpanKindClient(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindClient))
}

// SetSpanKindProducer sets the span kind to producer
func SetSpanKindProducer(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindProducer))
}

// SetSpanKindConsumer sets the span kind to consumer
func SetSpanKindConsumer(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindConsumer))
}

// SetSpanKindDatabase sets the span kind to client for database operations
func SetSpanKindDatabase(spans *Spans) {
	if spans == nil || spans.OTel == nil {
		return
	}
	spans.OTel.SetAttributes(attribute.String("span.kind", SpanKindClient))
}

// TraceError adds error information to both Jaeger and OpenTelemetry spans
func (s *Spans) TraceError(err error) {
	if s == nil || err == nil {
		return
	}

	// Trace error in Jaeger
	if s.Jaeger != nil {
		tracing.TraceErr(s.Jaeger, err)
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

// LogFields logs fields to both Jaeger and OpenTelemetry spans
func (s *Spans) LogFields(fields ...log.Field) {
	if s == nil {
		return
	}

	// Log to Jaeger
	if s.Jaeger != nil {
		s.Jaeger.LogFields(fields...)
	}

	// Log to OpenTelemetry
	if s.OTel != nil {
		attrs := make([]attribute.KeyValue, 0, len(fields))
		for _, field := range fields {
			key := field.Key()
			value := field.Value()
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
				attrs = append(attrs, attribute.String(key, fmt.Sprintf("%v", v)))
			}
		}
		s.OTel.AddEvent("log", trace.WithAttributes(attrs...))
	}
}

// LogKV logs key-value pairs to both Jaeger and OpenTelemetry spans
func (s *Spans) LogKV(alternatingKeyValues ...interface{}) {
	if s == nil {
		return
	}

	// Log to Jaeger
	if s.Jaeger != nil {
		s.Jaeger.LogKV(alternatingKeyValues...)
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

// LogObjectAsJson logs an object as JSON to both Jaeger and OpenTelemetry spans
func (s *Spans) LogObjectAsJson(key string, obj interface{}) {
	if s == nil {
		return
	}

	// Log to Jaeger
	if s.Jaeger != nil {
		tracing.LogObjectAsJson(s.Jaeger, key, obj)
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
