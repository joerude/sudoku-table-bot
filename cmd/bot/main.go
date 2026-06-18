// Command bot runs the Sudoku League Telegram bot.
package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joerude/sudoku-bot-telegram/internal/bot"
	"github.com/joerude/sudoku-bot-telegram/internal/config"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create data dir: %v", err)
		}
	}

	// Apply a database uploaded via /restore (atomic swap before opening).
	if err := storage.ApplyPendingRestore(cfg.DBPath); err != nil {
		log.Printf("restore: %v", err)
	}

	st, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer st.Close()

	b, err := bot.New(cfg.Token, cfg.PollTimeout, st, cfg.DBPath)
	if err != nil {
		log.Fatalf("bot: %v", err)
	}

	go b.Start()

	// Graceful shutdown: stop polling on SIGINT/SIGTERM so the DB closes cleanly.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down…")
	b.Stop()
}
