package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultFluentTag       = "app.logs"
	defaultShutdownTimeout = 5 * time.Second
)

var (
	ErrLoggerClosed = errors.New("logger is closed")
)

// FluentConfig wraps fluent.Config with additional fields.
type FluentConfig struct {
	Fluent  fluent.Config
	Tag     string
	Timeout time.Duration
}

// FluentLogger implements zapcore.WriteSyncer with thread safety.
type FluentLogger struct {
	mu      sync.Mutex
	logger  *fluent.Fluent
	tag     string
	closed  bool
	timeout time.Duration
}

// WrappedLogger wraps zap.SugaredLogger with ownership of resources.
type WrappedLogger struct {
	*zap.SugaredLogger
	fluent    *FluentLogger
	closeOnce sync.Once
}

// WrappedLogger provides a logger with atomic log level handling and
// proper resource cleanup.
// It bridges Zap's formatting with Fluent's transport layer.
func NewWrappedLogger(cfg FluentConfig) (*WrappedLogger, error) {
	// Validate configuration before initialization
	if cfg.Tag == "" {
		cfg.Tag = defaultFluentTag
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultShutdownTimeout
	}

	fl, err := fluent.New(cfg.Fluent)
	if err != nil {
		return nil, fmt.Errorf("failed to create fluent logger: %w", err)
	}

	fluentLogger := &FluentLogger{
		logger:  fl,
		tag:     cfg.Tag,
		timeout: cfg.Timeout,
	}

	// Configure structured logging pipeline
	encoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "logging_timestamp",
		MessageKey:     "message",
		LevelKey:       "severity", // Standardized field name for GCP/Cloud logging
		EncodeTime:     zapcore.RFC3339NanoTimeEncoder,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,  // "info" vs "INFO" for parser compatibility
		EncodeDuration: zapcore.SecondsDurationEncoder, // Decimal seconds for metrics systems
	})

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(fluentLogger),
		zapcore.InfoLevel,
	)

	return &WrappedLogger{
		SugaredLogger: zap.New(core).Sugar(),
		fluent:        fluentLogger,
	}, nil
}

// Write implements zapcore.WriteSyncer with proper error handling and JSON parsing
func (f *FluentLogger) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, ErrLoggerClosed
	}

	// Decode Zap's formatted JSON
	var entry map[string]interface{}
	if err := json.Unmarshal(p, &entry); err != nil {
		return 0, fmt.Errorf("log decode failed: %w", err)
	}

	// Fluent-specific routing
	if err := f.logger.PostWithTime(f.tag, time.Now(), entry); err != nil {
		return 0, fmt.Errorf("failed to send log entry: %w", err)
	}

	return len(p), nil
}

// Sync implements proper resource cleanup with timeout
func (f *FluentLogger) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}

	f.closed = true
	return f.logger.Close()
}

// Close implements graceful shutdown of an instance of WrappedLogger with context.
func (l *WrappedLogger) Close() error {
	if l == nil {
		return nil
	}

	// Flush buffered logs with timeout
	var err error
	l.closeOnce.Do(func() {
		_, cancel := context.WithTimeout(context.Background(), l.fluent.timeout)
		defer cancel()

		err = l.Sync()
		l.fluent.Sync() // Explicit cleanup
	})
	return err
}
