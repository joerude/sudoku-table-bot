// Package config loads bot configuration from environment variables.
package config

import (
	"errors"
	"os"
	"time"
)

// Config holds runtime configuration.
type Config struct {
	Token       string        // Telegram bot token (BOT_TOKEN)
	DBPath      string        // SQLite file path (DB_PATH)
	PollTimeout time.Duration // long-poll timeout
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		Token:       os.Getenv("BOT_TOKEN"),
		DBPath:      getenv("DB_PATH", "./data/sudoku.db"),
		PollTimeout: 10 * time.Second,
	}
	if c.Token == "" {
		return c, errors.New("BOT_TOKEN is not set")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
