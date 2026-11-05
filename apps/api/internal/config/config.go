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
	SSEHeartbeat     time.Duration
	AppTimezone      *time.Location
	DatabaseURL      string
	APIToken         string
	Port             string
	// ListenAddr is the address the HTTP server binds to: loopback only
	// when running behind ANCM, all interfaces otherwise.
	ListenAddr string
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

	// Below a typical ALB's default 60s idle timeout, so a deployment behind
	// one doesn't need any extra configuration for /api/events to survive.
	sseHeartbeat, err := durationEnv("SSE_HEARTBEAT", "25s")
	if err != nil {
		return Config{}, err
	}
	if sseHeartbeat <= 0 {
		return Config{}, fmt.Errorf("SSE_HEARTBEAT must be positive, got %s", sseHeartbeat)
	}
	cfg.SSEHeartbeat = sseHeartbeat

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

	// Behind IIS (ASP.NET Core Module, out-of-process) the module picks the
	// port the app must listen on and hands it over in ASPNETCORE_PORT, so
	// it takes precedence over the generic PORT. ANCM reaches the app on
	// 127.0.0.1 only, so in that case bind loopback and don't expose the
	// port on other interfaces.
	if p := os.Getenv("ASPNETCORE_PORT"); p != "" {
		cfg.Port = p
		cfg.ListenAddr = "127.0.0.1:" + p
	} else {
		cfg.Port = stringEnv("PORT", "8080")
		cfg.ListenAddr = ":" + cfg.Port
	}

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
