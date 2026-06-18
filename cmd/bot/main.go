// Command bot runs the Sudoku League Telegram bot.
package main

import (
	"log"
	"os"
	"path/filepath"

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

	st, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer st.Close()

	b, err := bot.New(cfg.Token, cfg.PollTimeout, st)
	if err != nil {
		log.Fatalf("bot: %v", err)
	}

	b.Start()
}
