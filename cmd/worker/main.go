package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/niquet/go-fluentd-logger-poc/internal/observability"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

func main() {
	// Read environment variables
	fluentHost := getEnv("FLUENT_HOST", "fluentbit")
	fluentPort, _ := strconv.Atoi(getEnv("FLUENT_PORT", "24224"))
	async, _ := strconv.ParseBool(getEnv("FLUENT_ASYNC", "true"))
	bufferLimit, _ := strconv.Atoi(getEnv("FLUENT_BUFFER_LIMIT", "8192"))
	maxRetry, _ := strconv.Atoi(getEnv("FLUENT_MAX_RETRY", "13"))
	retryWait, _ := strconv.Atoi(getEnv("FLUENT_RETRY_WAIT", "500"))

	logger, err := observability.NewWrappedLogger(observability.FluentConfig{
		Tag:     "service.metrics",
		Timeout: 10 * time.Second,
		Fluent: fluent.Config{
			FluentHost:  fluentHost,  // Set host from env
			FluentPort:  fluentPort,  // Set port from env
			Async:       async,       // Set async from env
			BufferLimit: bufferLimit, // Set buffer limit from env
			MaxRetry:    maxRetry,    // Set max retries from env
			RetryWait:   retryWait,   // Set retry wait from env
		},
	})
	if err != nil {
		panic(fmt.Errorf("failed to create logger: %w", err))
	}
	defer logger.Close()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logSystemMetrics(logger)
		}
	}
}

// Helper function to read env vars with defaults.
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func logSystemMetrics(logger *observability.WrappedLogger) {
	cpuPercent, err := cpu.Percent(1*time.Second, false)
	if err != nil {
		logger.Errorf("Failed to get CPU metrics: %v", err)
		return
	}

	memStat, err := mem.VirtualMemory()
	if err != nil {
		logger.Errorf("Failed to get memory metrics: %v", err)
		return
	}

	logger.Infow("System metrics collected",
		"component", "resource_monitor",
		"cpu_usage_percent", cpuPercent[0],
		"memory_usage_percent", memStat.UsedPercent,
		"memory_used_mb", memStat.Used/1024/1024,
		"memory_total_mb", memStat.Total/1024/1024,
		"memory_available_mb", memStat.Available/1024/1024,
	)
}
