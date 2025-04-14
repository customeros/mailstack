package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"github.com/uber/jaeger-client-go/config"
	"github.com/uber/jaeger-client-go/log/zap"

	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/utils"
)

type JaegerConfig struct {
	Endpoint     string  `env:"JAEGER_ENDPOINT"`
	ServiceName  string  `env:"JAEGER_SERVICE_NAME" validate:"required"`
	AgentHost    string  `env:"JAEGER_AGENT_HOST" envDefault:"localhost" validate:"required"`
	AgentPort    string  `env:"JAEGER_AGENT_PORT" envDefault:"6831" validate:"required"`
	Enabled      bool    `env:"JAEGER_ENABLED" envDefault:"true"`
	LogSpans     bool    `env:"JAEGER_REPORTER_LOG_SPANS" envDefault:"false" `
	SamplerType  string  `env:"JAEGER_SAMPLER_TYPE" envDefault:"const" validate:"required"`
	SamplerParam float64 `env:"JAEGER_SAMPLER_PARAM" envDefault:"1" validate:"required"`
}

func NewJaegerTracer(jaegerConfig *JaegerConfig, log logger.Logger) (opentracing.Tracer, io.Closer, error) {
	cfg := initJaeger(jaegerConfig)

	return cfg.NewTracer(config.Logger(zap.NewLogger(log.Logger())))
}

func initJaeger(jaegerConfig *JaegerConfig) *config.Configuration {
	cfg := &config.Configuration{
		ServiceName: jaegerConfig.ServiceName,
		Disabled:    !jaegerConfig.Enabled,
		Sampler: &config.SamplerConfig{
			Type:  jaegerConfig.SamplerType,
			Param: jaegerConfig.SamplerParam,
		},
		Reporter: &config.ReporterConfig{
			LogSpans: jaegerConfig.LogSpans,
		},
	}

	// Use HTTP endpoint if provided, otherwise fall back to agent
	if jaegerConfig.Endpoint != "" {
		cfg.Reporter.CollectorEndpoint = jaegerConfig.Endpoint
	} else {
		cfg.Reporter.LocalAgentHostPort = jaegerConfig.AgentHost + ":" + jaegerConfig.AgentPort
	}

	return cfg
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

func SetDefaultSpanTags(ctx context.Context, span opentracing.Span) {
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
