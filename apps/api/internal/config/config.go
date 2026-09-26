// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"

	_ "time/tzdata"
)

// Config holds all runtime configuration for the API monitor service.
type Config struct {
	CheckInterval    time.Duration
	CheckTimeout     time.Duration
	RetryDelay       time.Duration
	FailureThreshold int
	DegradedLatency  time.Duration
	MaxConcurrency   int
	AppTimezone      *time.Location
	DatabaseURL      string
	APIToken         string
	Port             string
}

// Load reads configuration from environment variables, applying defaults
// for anything not explicitly set. DATABASE_URL is required.
func Load() (Config, error) {
	cfg := Config{}

	checkInterval, err := durationEnv("CHECK_INTERVAL", "60s")
	if err != nil {
		return Config{}, err
	}
	if checkInterval <= 0 {
		return Config{}, fmt.Errorf("CHECK_INTERVAL must be positive, got %s", checkInterval)
	}
	cfg.CheckInterval = checkInterval

	checkTimeout, err := durationEnv("CHECK_TIMEOUT", "5s")
	if err != nil {
		return Config{}, err
	}
	if checkTimeout <= 0 {
		return Config{}, fmt.Errorf("CHECK_TIMEOUT must be positive, got %s", checkTimeout)
	}
	cfg.CheckTimeout = checkTimeout

	retryDelay, err := durationEnv("RETRY_DELAY", "10s")
	if err != nil {
		return Config{}, err
	}
	if retryDelay <= 0 {
		return Config{}, fmt.Errorf("RETRY_DELAY must be positive, got %s", retryDelay)
	}
	cfg.RetryDelay = retryDelay

	degradedLatency, err := durationEnv("DEGRADED_LATENCY", "2s")
	if err != nil {
		return Config{}, err
	}
	cfg.DegradedLatency = degradedLatency

	failureThreshold, err := intEnv("FAILURE_THRESHOLD", 2)
	if err != nil {
		return Config{}, err
	}
	if failureThreshold < 1 {
		return Config{}, fmt.Errorf("FAILURE_THRESHOLD must be >= 1, got %d", failureThreshold)
	}
	cfg.FailureThreshold = failureThreshold

	maxConcurrency, err := intEnv("MAX_CONCURRENCY", 20)
	if err != nil {
		return Config{}, err
	}
	if maxConcurrency < 1 {
		return Config{}, fmt.Errorf("MAX_CONCURRENCY must be >= 1, got %d", maxConcurrency)
	}
	cfg.MaxConcurrency = maxConcurrency

	tz := stringEnv("APP_TIMEZONE", "America/Argentina/Buenos_Aires")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("invalid APP_TIMEZONE %q: %w", tz, err)
	}
	cfg.AppTimezone = loc

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	cfg.DatabaseURL = dbURL

	apiToken := os.Getenv("API_TOKEN")
	if apiToken == "" {
		return Config{}, fmt.Errorf("API_TOKEN is required")
	}
	cfg.APIToken = apiToken

	cfg.Port = stringEnv("PORT", "8080")

	return cfg, nil
}

func stringEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key, def string) (time.Duration, error) {
	v := stringEnv(key, def)
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	return d, nil
}

func intEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, v, err)
	}
	return n, nil
}
