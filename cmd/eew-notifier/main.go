// Command eew-notifier is the production daemon: connects to WNI's
// FastCaster EEW feed and posts notifications to configured Slack
// incoming-webhooks. Slack-only port of irc-eew.pl (IRC support dropped).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/walkure/irc-eew/internal/app"
	"github.com/walkure/irc-eew/internal/config"
)

func main() {
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, or error")
	flag.Parse()

	level, err := parseLogLevel(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	configPath := "config.yaml"
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("loading config", "path", configPath, "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid -log-level %q: must be debug, info, warn, or error", s)
	}
}
