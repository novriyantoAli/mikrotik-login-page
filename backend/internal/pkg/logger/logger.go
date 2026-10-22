// Package logger builds the single *zap.Logger injected through Fx. Domain code
// receives it; it never constructs its own logger.
package logger

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// New builds a zap logger from configuration and flushes it on shutdown.
func New(lc fx.Lifecycle, cfg *config.Config) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Logger.Level)
	if err != nil {
		return nil, fmt.Errorf("parse logger.level %q: %w", cfg.Logger.Level, err)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeDuration = zapcore.MillisDurationEncoder

	var encoder zapcore.Encoder
	if cfg.Logger.Format == config.LogFormatConsole {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	sink, closer, err := openSink(cfg.Logger.OutputPath)
	if err != nil {
		return nil, err
	}

	core := zapcore.NewCore(encoder, sink, level)
	log := zap.New(core, zap.AddStacktrace(zapcore.ErrorLevel))

	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			_ = log.Sync()
			return closer.Close()
		},
	})

	return log, nil
}

func openSink(path string) (zapcore.WriteSyncer, closer, error) {
	switch path {
	case "", "stdout":
		return zapcore.AddSync(os.Stdout), nopCloser{}, nil
	case "stderr":
		return zapcore.AddSync(os.Stderr), nopCloser{}, nil
	default:
		//nolint:gosec // the path is operator configuration, never user input.
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open logger.output_path %q: %w", path, err)
		}
		return zapcore.AddSync(file), file, nil
	}
}

type closer interface{ Close() error }

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
