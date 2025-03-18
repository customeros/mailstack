package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc/metadata"

	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/utils"
)

const (
	SpanTagTenant    = "tenant"
	SpanTagUserId    = "user-id"
	SpanTagUserEmail = "user-email"
	SpanTagEntityId  = "entity-id"
	SpanTagComponent = "component"
)

const (
	SpanTagComponentPostgresRepository = "postgresRepository"
	SpanTagComponentRest               = "rest"
	SpanTagComponentGraphQL            = "graphql"
	SpanTagComponentCronJob            = "cronJob"
	SpanTagComponentService            = "service"
	SpanTagComponentListener           = "listener"
)

func GraphQlTracingEnhancer(ctx context.Context) func(c *gin.Context) {
	return func(c *gin.Context) {
		ctxWithSpan, span := StartHttpServerTracerSpanWithHeader(ctx, ExtractGraphQLMethodName(c.Request), c.Request.Header)
		for k, v := range c.Request.Header {
			span.LogFields(log.String("request.header.key", k), log.Object("request.header.value", v))
		}
		defer span.Finish()
		TagComponentRest(span)
		c.Request = c.Request.WithContext(ctxWithSpan)
		c.Next()
	}
}

func TracingEnhancer(ctx context.Context, endpoint string) func(c *gin.Context) {
	return func(c *gin.Context) {
		ctxWithSpan, span := StartHttpServerTracerSpanWithHeader(ctx, endpoint, c.Request.Header)
		for k, v := range c.Request.Header {
			span.LogFields(log.String("request.header.key", k), log.Object("request.header.value", v))
		}
		defer span.Finish()
		TagComponentRest(span)
		c.Request = c.Request.WithContext(ctxWithSpan)
		c.Next()
	}
}

func StartHttpServerTracerSpanWithHeader(ctx context.Context, operationName string, headers http.Header) (context.Context, opentracing.Span) {
	spanCtx, err := opentracing.GlobalTracer().Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(headers))
	if err != nil {
		serverSpan := opentracing.GlobalTracer().StartSpan(operationName)
		opentracing.GlobalTracer().Inject(serverSpan.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(headers))
		return opentracing.ContextWithSpan(ctx, serverSpan), serverSpan
	}

	serverSpan := opentracing.GlobalTracer().StartSpan(operationName, ext.RPCServerOption(spanCtx))
	return opentracing.ContextWithSpan(ctx, serverSpan), serverSpan
}

func StartRabbitMQMessageTracerSpanWithHeader(ctx context.Context, operationName string, uberTraceId string) (context.Context, opentracing.Span) {
	textMapCarrierFromMetaData := make(opentracing.TextMapCarrier)
	textMapCarrierFromMetaData.Set("uber-trace-id", uberTraceId)

	span, err := opentracing.GlobalTracer().Extract(opentracing.TextMap, textMapCarrierFromMetaData)
	if err != nil {
		serverSpan := opentracing.GlobalTracer().StartSpan(operationName)
		ctx = opentracing.ContextWithSpan(ctx, serverSpan)
		return ctx, serverSpan
	}

	serverSpan := opentracing.GlobalTracer().StartSpan(operationName, ext.RPCServerOption(span))
	ctx = opentracing.ContextWithSpan(ctx, serverSpan)
	return ctx, serverSpan
}

func StartTracerSpan(ctx context.Context, operationName string) (opentracing.Span, context.Context) {
	serverSpan := opentracing.GlobalTracer().StartSpan(operationName)
	return serverSpan, opentracing.ContextWithSpan(ctx, serverSpan)
}

func InjectSpanContextIntoGrpcMetadata(ctx context.Context, span opentracing.Span) context.Context {
	if span != nil {
		// Inject the span context into the gRPC request metadata.
		textMapCarrier := make(opentracing.TextMapCarrier)
		err := span.Tracer().Inject(span.Context(), opentracing.TextMap, textMapCarrier)
		if err == nil {
			// Add the injected metadata to the gRPC context.
			md, ok := metadata.FromOutgoingContext(ctx)
			if !ok {
				md = metadata.New(nil)
			}
			for key, val := range textMapCarrier {
				md.Set(key, val)
			}
			ctx = metadata.NewOutgoingContext(ctx, md)
			return ctx
		}
	}
	return ctx
}

func InjectSpanContextIntoHTTPRequest(req *http.Request, span opentracing.Span) *http.Request {
	if span != nil {
		// Prepare to inject span context into HTTP headers
		tracer := span.Tracer()
		textMapCarrier := opentracing.HTTPHeadersCarrier(req.Header)

		// Inject the span context into the HTTP headers
		err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, textMapCarrier)
		if err != nil {
			// Log error or handle it as per the application's error handling strategy
			fmt.Println("Error injecting span context into headers:", err)
		}
	}
	return req
}

func ExtractGraphQLMethodName(req *http.Request) string {
	// Read the request body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		// Handle error
		return ""
	}

	// Restore the request body
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	// Parse the request body as JSON
	var requestBody map[string]interface{}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		// Handle error
		return ""
	}

	// Extract the method name from the GraphQL request
	if operationName, ok := requestBody["operationName"].(string); ok {
		return operationName
	}

	// If the method name is not found, you can add additional logic here to extract it from the request body or headers if applicable
	// ...
	return ""
}

func setDefaultSpanTags(ctx context.Context, span opentracing.Span) {
	tenant := utils.GetTenantFromContext(ctx)
	loggedInUserId := utils.GetUserIdFromContext(ctx)
	loggedInUserEmail := utils.GetUserEmailFromContext(ctx)
	if tenant != "" {
		span.SetTag(SpanTagTenant, tenant)
	}
	if loggedInUserId != "" {
		span.SetTag(SpanTagUserId, loggedInUserId)
	}
	if loggedInUserEmail != "" {
		span.SetTag(SpanTagUserEmail, loggedInUserEmail)
	}
}

func SetDefaultRestSpanTags(ctx context.Context, span opentracing.Span) {
	setDefaultSpanTags(ctx, span)
	TagComponentRest(span)
}

func SetDefaultGraphqlSpanTags(ctx context.Context, span opentracing.Span) {
	setDefaultSpanTags(ctx, span)
	TagComponentGraphql(span)
}

func SetDefaultServiceSpanTags(ctx context.Context, span opentracing.Span) {
	setDefaultSpanTags(ctx, span)
	TagComponentService(span)
}

func SetDefaultPostgresRepositorySpanTags(ctx context.Context, span opentracing.Span) {
	setDefaultSpanTags(ctx, span)
	TagComponentPostgresRepository(span)
}

func TraceErr(span opentracing.Span, err error, fields ...log.Field) {
	if span == nil || err == nil {
		return
	}
	// Log the error with the fields
	ext.LogError(span, err, fields...)
}

func LogObjectAsJson(span opentracing.Span, name string, object any) {
	if object == nil {
		span.LogFields(log.String(name, "nil"))
	}
	jsonObject, err := json.Marshal(object)
	if err == nil {
		span.LogFields(log.String(name, string(jsonObject)))
	} else {
		span.LogFields(log.Object(name, object))
	}
}

func InjectTextMapCarrier(spanCtx opentracing.SpanContext) (opentracing.TextMapCarrier, error) {
	m := make(opentracing.TextMapCarrier)
	if err := opentracing.GlobalTracer().Inject(spanCtx, opentracing.TextMap, m); err != nil {
		return nil, err
	}
	return m, nil
}

func ExtractTextMapCarrier(spanCtx opentracing.SpanContext) opentracing.TextMapCarrier {
	textMapCarrier, err := InjectTextMapCarrier(spanCtx)
	if err != nil {
		return make(opentracing.TextMapCarrier)
	}
	return textMapCarrier
}

func GetTraceId(span opentracing.Span) string {
	tracingData := ExtractTextMapCarrier((span).Context())
	return strings.Split(tracingData["uber-trace-id"], ":")[0]
}

func TagComponentPostgresRepository(span opentracing.Span) {
	span.SetTag(SpanTagComponent, SpanTagComponentPostgresRepository)
}

func TagTenant(span opentracing.Span, tenant string) {
	if tenant != "" {
		span.SetTag(SpanTagTenant, tenant)
	}
}

func TagEntity(span opentracing.Span, entityId string) {
	if entityId != "" {
		span.SetTag(SpanTagEntityId, entityId)
	}
}

func TagComponentCronJob(span opentracing.Span) {
	span.SetTag(SpanTagComponent, SpanTagComponentCronJob)
}

func TagComponentRest(span opentracing.Span) {
	span.SetTag(SpanTagComponent, SpanTagComponentRest)
}

func TagComponentGraphql(span opentracing.Span) {
	span.SetTag(SpanTagComponent, SpanTagComponentGraphQL)
}

func TagComponentService(span opentracing.Span) {
	span.SetTag(SpanTagComponent, SpanTagComponentService)
}

func TagComponentListener(span opentracing.Span) {
	span.SetTag(SpanTagComponent, SpanTagComponentListener)
}

func RecoveryWithJaeger(tracer opentracing.Tracer) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				// Log the panic to Jaeger
				span := tracer.StartSpan("panic-recovery")
				defer span.Finish()

				buf := make([]byte, 4096)
				stackSize := runtime.Stack(buf, false)
				span.LogKV(
					"event", "error",
					"error.object", r,
					"stack", string(buf[:stackSize]),
				)
				span.SetTag("error", true)
			}
		}()
		c.Next()
	}
}

func RecoverAndLogToJaeger(appLogger logger.Logger) {
	if r := recover(); r != nil {
		tracer := opentracing.GlobalTracer()
		span := tracer.StartSpan("panic-recovery")
		defer span.Finish()

		stackTrace := string(debug.Stack())
		span.LogKV(
			"event", "error",
			"error.object", r,
			"stack", stackTrace,
		)
		span.SetTag("error", true)

		appLogger.Errorf("Recovered from panic: %v\nStack trace:\n%s", r, stackTrace)
	}
}
