package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultFluentTag                = "app.logs"
	defaultLogLevel                 = zapcore.DebugLevel
	defaultShutdownTimeout          = 5 * time.Second
	compatibleLevelWarningUpperCase = "WARNING"
)

var (
	ErrLoggerClosed = errors.New("logger is closed")
)

// FluentLogger implements zapcore.WriteSyncer with thread safety.
type FluentLogger struct {
	logger  *fluent.Fluent
	tag     string
	closed  atomic.Bool
	timeout time.Duration
}

// SugaredLoggerConfig wraps fluent.Config with additional fields.
type SugaredLoggerConfig struct {
	FluentConfig fluent.Config
	Tag          string
	LogLevel     string
}

// SugaredLogger wraps zap.SugaredLogger with ownership of resources.
type SugaredLogger struct {
	*zap.SugaredLogger
	fluent    *FluentLogger
	closeOnce sync.Once
}

// NewSugaredLogger provides a logger with atomic log level handling and
// proper resource cleanup.
// It bridges Zap's formatting with Fluent's transport layer.
func NewSugaredLogger(cfg *SugaredLoggerConfig) (*SugaredLogger, error) {
	// Validate configuration before initialization
	if cfg.Tag == "" {
		cfg.Tag = defaultFluentTag
	}
	if cfg.FluentConfig.Timeout == 0 {
		cfg.FluentConfig.Timeout = defaultShutdownTimeout
	}

	fl, err := fluent.New(cfg.FluentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create fluent logger: %w", err)
	}

	fluentLogger := &FluentLogger{
		logger:  fl,
		tag:     cfg.Tag,
		timeout: cfg.FluentConfig.Timeout,
	}

	// Configure structured logging pipeline
	encoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "severity",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.RFC3339NanoTimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	lvl := parseLogLevel(cfg.LogLevel)
	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(fluentLogger),
		lvl,
	)

	return &SugaredLogger{
		SugaredLogger: zap.New(core).Sugar(),
		fluent:        fluentLogger,
	}, nil
}

// Write implements zapcore.WriteSyncer with proper error handling and JSON parsing
func (f *FluentLogger) Write(p []byte) (int, error) {
	if f.closed.Load() {
		return 0, ErrLoggerClosed
	}

	// Decode Zap's formatted JSON
	var entry map[string]interface{}
	if err := json.Unmarshal(p, &entry); err != nil {
		return 0, fmt.Errorf("log decode failed: %w", err)
	}

	// Async PostWithTime handles its own synchronization
	if err := f.logger.PostWithTime(f.tag, time.Now(), entry); err != nil {
		return 0, fmt.Errorf("log delivery failed: %w", err)
	}

	return len(p), nil
}

// Sync implements proper resource cleanup with timeout
func (f *FluentLogger) Sync() error {
	if f.closed.Swap(true) {
		return nil
	}

	// Flush remaining logs with timeout
	done := make(chan struct{})
	go func() {
		f.logger.Close()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(f.timeout):
		return errors.New("fluent log flush timed out")
	}
}

// Close implements graceful shutdown of an instance of WrappedLogger with context.
func (l *SugaredLogger) Close() error {
	if l == nil {
		return nil
	}

	var err error
	l.closeOnce.Do(func() {
		_, cancel := context.WithTimeout(context.Background(), l.fluent.timeout)
		defer cancel()

		// Flush Zap first to ensure all logs are sent to Fluent
		if syncErr := l.SugaredLogger.Sync(); syncErr != nil {
			err = fmt.Errorf("zap sync failed: %w", syncErr)
		}

		// Then close Fluent connection
		if fluentErr := l.fluent.Sync(); fluentErr != nil {
			err = fmt.Errorf("fluent close failed: %w", fluentErr)
		}
	})
	return err
}

func parseLogLevel(lvl string) zapcore.Level {
	level := defaultLogLevel
	err := level.UnmarshalText([]byte(lvl)) // if something unknown was provided, just stay with debug
	if err != nil && strings.ToUpper(lvl) == compatibleLevelWarningUpperCase {
		level = zapcore.WarnLevel
	}
	return level
}
