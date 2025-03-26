package telemetry

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/customeros/mailstack/internal/logger"
	"github.com/customeros/mailstack/internal/utils"
)

// TelemetryLogger wraps a Zap logger and adds telemetry capabilities
type TelemetryLogger struct {
	*logger.AppLogger
}

// NewTelemetryLogger creates a new TelemetryLogger that wraps a Zap logger
func NewTelemetryLogger(cfg *OpenTelemetryConfig, zapLogger *logger.AppLogger) (*TelemetryLogger, error) {
	return &TelemetryLogger{AppLogger: zapLogger}, nil
}

// log sends a log entry with telemetry information
func (l *TelemetryLogger) log(ctx context.Context, level zapcore.Level, msg string, fields ...zap.Field) {
	// Add context attributes
	contextFields := make([]zap.Field, 0, 3)
	if tenant := utils.GetTenantFromContext(ctx); tenant != "" {
		contextFields = append(contextFields, zap.String("tenant", tenant))
	}
	if userID := utils.GetUserIdFromContext(ctx); userID != "" {
		contextFields = append(contextFields, zap.String("user_id", userID))
	}
	if userEmail := utils.GetUserEmailFromContext(ctx); userEmail != "" {
		contextFields = append(contextFields, zap.String("user_email", userEmail))
	}

	// Combine context fields with provided fields
	allFields := append(contextFields, fields...)

	// Log with Zap
	if ce := l.Logger().Check(level, msg); ce != nil {
		ce.Write(allFields...)
	}
}

// Debug logs a debug message
func (l *TelemetryLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	l.log(ctx, zapcore.DebugLevel, msg, fields...)
}

// Info logs an info message
func (l *TelemetryLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	l.log(ctx, zapcore.InfoLevel, msg, fields...)
}

// Warn logs a warning message
func (l *TelemetryLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	l.log(ctx, zapcore.WarnLevel, msg, fields...)
}

// Error logs an error message
func (l *TelemetryLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	l.log(ctx, zapcore.ErrorLevel, msg, fields...)
}

// Fatal logs a fatal message and exits
func (l *TelemetryLogger) Fatal(ctx context.Context, msg string, fields ...zap.Field) {
	l.log(ctx, zapcore.FatalLevel, msg, fields...)
}

// Sync flushes any buffered log entries
func (l *TelemetryLogger) Sync() error {
	return l.AppLogger.Sync()
}
