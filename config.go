package main

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
	AuthCacheTTL     time.Duration
	ChannelsCacheTTL time.Duration
	MaxBodyBytes     int64
}

func loadConfig() (Config, error) {
	var validationErrors []error
	cfg := Config{
		HTTPAddr:         envString("HTTP_ADDR", ":8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		ReadTimeout:      envDuration("READ_TIMEOUT", 5*time.Second, &validationErrors),
		WriteTimeout:     envDuration("WRITE_TIMEOUT", 10*time.Second, &validationErrors),
		IdleTimeout:      envDuration("IDLE_TIMEOUT", 60*time.Second, &validationErrors),
		ShutdownTimeout:  envDuration("SHUTDOWN_TIMEOUT", 10*time.Second, &validationErrors),
		AuthCacheTTL:     envDuration("AUTH_CACHE_TTL", 30*time.Second, &validationErrors),
		ChannelsCacheTTL: envDuration("CHANNELS_CACHE_TTL", 5*time.Second, &validationErrors),
		MaxBodyBytes:     envInt64("MAX_BODY_BYTES", 1<<20, &validationErrors),
	}

	if cfg.DatabaseURL == "" {
		validationErrors = append(validationErrors, errors.New("DATABASE_URL is required"))
	}

	for _, setting := range []struct {
		name  string
		value time.Duration
	}{
		{"READ_TIMEOUT", cfg.ReadTimeout},
		{"WRITE_TIMEOUT", cfg.WriteTimeout},
		{"IDLE_TIMEOUT", cfg.IdleTimeout},
		{"SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout},
		{"AUTH_CACHE_TTL", cfg.AuthCacheTTL},
		{"CHANNELS_CACHE_TTL", cfg.ChannelsCacheTTL},
	} {
		if setting.value < 0 {
			validationErrors = append(validationErrors, errors.New(setting.name+" must be a non-negative duration"))
		}
	}
	if cfg.MaxBodyBytes < 1 {
		validationErrors = append(validationErrors, errors.New("MAX_BODY_BYTES must be a positive integer"))
	}
	if len(validationErrors) > 0 {
		return Config{}, errors.Join(validationErrors...)
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration, validationErrors *[]error) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		*validationErrors = append(*validationErrors, errors.New(key+" must be a valid duration"))
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64, validationErrors *[]error) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		*validationErrors = append(*validationErrors, errors.New(key+" must be a positive integer"))
		return fallback
	}
	return value
}
