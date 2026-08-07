// Command eew-notifier is the production daemon: connects to WNI's
// FastCaster EEW feed and posts notifications to configured Slack
// incoming-webhooks. Slack-only port of irc-eew.pl (IRC support dropped).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/walkure/irc-eew/internal/app"
	"github.com/walkure/irc-eew/internal/config"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("loading config %s: %v", configPath, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Fatalf("fatal: %v", err)
	}
	log.Println("shutdown complete")
}
