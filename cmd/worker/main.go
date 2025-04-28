package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"math/rand/v2"

	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/niquet/go-fluentd-logger-poc/internal/observability"
)

func main() {
	// Get fluent configuration from environment variables
	fluentCfg, err := loadFluentConfigFromEnv()
	if err != nil {
		panic(fmt.Errorf("failed to create wrapped logger: %w", err))
	}

	loggerCfg := &observability.SugaredLoggerConfig{
		FluentConfig: fluentCfg,
		LogLevel:     "DEBUG",
	}

	logger, err := observability.NewSugaredLogger(loggerCfg)
	if err != nil {
		panic(fmt.Errorf("failed to create logger: %w", err))
	}
	defer logger.Close()

	var wg sync.WaitGroup
	wg.Add(10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			// Recovery middleware for goroutines
			defer func() {
				if r := recover(); r != nil {
					logger.Errorw("goroutine panic",
						"goroutine", id,
						"recover", r,
						"stack", string(debug.Stack()),
					)
				}
			}()

			defer wg.Done()

			r := rand.IntN(1000)
			time.Sleep(time.Duration(r) * time.Millisecond)

			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					logger.Infow("log collected",
						"goroutine", id,
					)
				}
			}
		}(i)
	}

	wg.Wait()
}

func loadFluentConfigFromEnv() (fluent.Config, error) {
	cfg := fluent.Config{
		// Set default values from specification
		FluentNetwork: "tcp",
		FluentHost:    "127.0.0.1",
		FluentPort:    24224,
		Timeout:       10 * time.Second,
		BufferLimit:   8192,
		RetryWait:     500,
		MaxRetry:      13,
		Async:         false,
	}

	// Network configuration
	if env := os.Getenv("FLUENT_NETWORK"); env != "" {
		cfg.FluentNetwork = env
	}
	if !map[string]bool{"tcp": true, "tls": true, "unix": true}[cfg.FluentNetwork] {
		return fluent.Config{}, fmt.Errorf("invalid FluentNetwork: %s", cfg.FluentNetwork)
	}

	if cfg.FluentNetwork == "unix" {
		cfg.FluentSocketPath = os.Getenv("FLUENT_SOCKET_PATH")
		if cfg.FluentSocketPath == "" {
			return fluent.Config{}, fmt.Errorf("FluentSocketPath required for unix network")
		}
	} else {
		if host := os.Getenv("FLUENT_HOST"); host != "" {
			cfg.FluentHost = host
		}
		if portStr := os.Getenv("FLUENT_PORT"); portStr != "" {
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return fluent.Config{}, fmt.Errorf("invalid FluentPort: %w", err)
			}
			cfg.FluentPort = port
		}
	}

	// Timeouts
	if timeoutStr := os.Getenv("FLUENT_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fluent.Config{}, fmt.Errorf("invalid Timeout: %w", err)
		}
		cfg.Timeout = timeout
	}

	if writeTimeoutStr := os.Getenv("FLUENT_WRITE_TIMEOUT"); writeTimeoutStr != "" {
		writeTimeout, err := time.ParseDuration(writeTimeoutStr)
		if err != nil {
			return fluent.Config{}, fmt.Errorf("invalid WriteTimeout: %w", err)
		}
		cfg.WriteTimeout = writeTimeout
	}

	// Buffer and retry configuration
	if bufLimit := os.Getenv("FLUENT_BUFFER_LIMIT"); bufLimit != "" {
		limit, err := strconv.Atoi(bufLimit)
		if err != nil {
			return fluent.Config{}, fmt.Errorf("invalid BufferLimit: %w", err)
		}
		cfg.BufferLimit = limit
	}

	var err error
	if cfg.Async, err = parseBool("FLUENT_ASYNC"); err != nil {
		return fluent.Config{}, err
	}
	if cfg.ForceStopAsyncSend, err = parseBool("FLUENT_FORCE_STOP_ASYNC_SEND"); err != nil {
		return fluent.Config{}, err
	}
	if cfg.SubSecondPrecision, err = parseBool("FLUENT_SUB_SECOND_PRECISION"); err != nil {
		return fluent.Config{}, err
	}
	if cfg.MarshalAsJSON, err = parseBool("FLUENT_MARSHAL_AS_JSON"); err != nil {
		return fluent.Config{}, err
	}
	if cfg.RequestAck, err = parseBool("FLUENT_REQUEST_ACK"); err != nil {
		return fluent.Config{}, err
	}
	if cfg.TlsInsecureSkipVerify, err = parseBool("FLUENT_TLS_INSECURE_SKIP_VERIFY"); err != nil {
		return fluent.Config{}, err
	}

	// Optional numeric parameters
	if reconnect := os.Getenv("FLUENT_ASYNC_RECONNECT_INTERVAL"); reconnect != "" {
		interval, err := strconv.Atoi(reconnect)
		if err != nil {
			return fluent.Config{}, fmt.Errorf("invalid AsyncReconnectInterval: %w", err)
		}
		cfg.AsyncReconnectInterval = interval
	}

	// String parameters
	cfg.TagPrefix = os.Getenv("FLUENT_TAG_PREFIX")

	return cfg, nil
}

func parseBool(envVar string) (bool, error) {
	val := strings.ToLower(os.Getenv(envVar))
	switch val {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value for %s", envVar)
	}
}
