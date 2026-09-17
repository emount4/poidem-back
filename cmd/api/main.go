package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/emount4/poidem-back/internal/app"
	"github.com/emount4/poidem-back/internal/logger"
)

func main() {
	configPath := flag.String("config", ".env", "path to .env config")
	flag.Parse()

	log := logger.New(os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, *configPath, log); err != nil {
		log.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
