// Package config reads service settings from environment variables.
package config

import (
	"log/slog"
	"os"
)

type Config struct {
	HTTPAddr  string
	MySQLDSN  string
	RedisAddr string
	LogLevel  slog.Level
}

func Load() Config {
	cfg := Config{
		HTTPAddr: getenv("HTTP_ADDR", ":8080"),
		// parseTime=true scans DATETIME into time.Time; loc=UTC keeps every
		// timestamp in UTC so the daily-limit window is computed correctly.
		MySQLDSN:  getenv("MYSQL_DSN", "bank:bank@tcp(localhost:3306)/bank?parseTime=true&loc=UTC"),
		RedisAddr: getenv("REDIS_ADDR", "localhost:6379"),
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "INFO"))); err != nil {
		cfg.LogLevel = slog.LevelInfo
	}
	return cfg
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
