package tracing

import (
	"io"

	"github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go/config"
	"github.com/uber/jaeger-client-go/log/zap"

	"github.com/customeros/mailstack/internal/logger"
)

type JaegerConfig struct {
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

		// "const" sampler is a binary sampling strategy: 0=never sample, 1=always sample.
		Sampler: &config.SamplerConfig{
			Type:  jaegerConfig.SamplerType,
			Param: jaegerConfig.SamplerParam,
		},

		// Log the emitted spans to stdout.
		Reporter: &config.ReporterConfig{
			LogSpans:           jaegerConfig.LogSpans,
			LocalAgentHostPort: jaegerConfig.AgentHost + ":" + jaegerConfig.AgentPort,
		},
	}
	return cfg
}
