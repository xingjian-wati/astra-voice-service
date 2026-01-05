package logger

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

var (
	globalSugar *zap.SugaredLogger
	globalBase  *zap.Logger
)

// Init initializes a global zap logger. The env can be "production" or "development" (default).
// It also redirects the stdlib log output to zap so existing log.Printf calls are captured.
func Init(env string) (*zap.SugaredLogger, error) {
	if globalSugar != nil && globalBase != nil {
		return globalSugar, nil
	}

	var cfg zap.Config
	if strings.EqualFold(env, "prod") || strings.EqualFold(env, "production") {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	base, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	zap.ReplaceGlobals(base)
	_ = zap.RedirectStdLog(base) // route log.Printf to zap

	globalBase = base
	globalSugar = base.Sugar()
	return globalSugar, nil
}

// L returns the global sugared logger, initializing it on first use.
func L() *zap.SugaredLogger {
	if globalSugar == nil {
		env := os.Getenv("LOG_ENV")
		if _, err := Init(env); err != nil {
			base, _ := zap.NewDevelopment()
			globalBase = base
			globalSugar = base.Sugar()
		}
	}
	return globalSugar
}

// Base returns the base *zap.Logger (non-sugared).
func Base() *zap.Logger {
	if globalBase == nil {
		env := os.Getenv("LOG_ENV")
		if _, err := Init(env); err != nil {
			base, _ := zap.NewDevelopment()
			globalBase = base
			globalSugar = base.Sugar()
		}
	}
	return globalBase
}

// Debug logs with context and fields.
func Debug(ctx context.Context, msg string, fields ...zap.Field) {
	Base().WithOptions().With(fields...).Debug(msg)
}

// Info logs with context and fields.
func Info(ctx context.Context, msg string, fields ...zap.Field) {
	Base().WithOptions().With(fields...).Info(msg)
}

// Warn logs with context and fields.
func Warn(ctx context.Context, msg string, fields ...zap.Field) {
	Base().WithOptions().With(fields...).Warn(msg)
}

// Error logs with context and fields.
func Error(ctx context.Context, msg string, fields ...zap.Field) {
	Base().WithOptions().With(fields...).Error(msg)
}

// Sync flushes any buffered log entries.
func Sync() {
	if globalSugar != nil {
		_ = globalSugar.Sync()
	}
	if globalBase != nil {
		_ = globalBase.Sync()
	}
}

// GORMWriter is a Writer adapter for GORM logger that writes to zap logger
// GORM's logger.Writer interface requires Printf method
type GORMWriter struct{}

// Printf implements gorm.io/gorm/logger.Writer interface
func (w GORMWriter) Printf(format string, v ...interface{}) {
	// GORM logger writes error messages, so we use Error level
	// Format the message with the provided arguments
	msg := fmt.Sprintf(format, v...)
	// Remove trailing newline if present
	msg = strings.TrimSuffix(msg, "\n")
	msg = strings.TrimSuffix(msg, "\r\n")
	Base().Error(msg)
}

// NewGORMWriter creates a new GORM writer adapter
func NewGORMWriter() GORMWriter {
	return GORMWriter{}
}
