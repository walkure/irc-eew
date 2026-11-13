// Command eew-notifier is the production daemon: receives EEW telegrams
// from DMDATA.JP's WebSocket v2 feed and notifies configured Slack
// incoming-webhooks and IRC channels. Originally a port of irc-eew.pl,
// which read WNI's (now discontinued) FastCaster feed.
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

	// The production image runs FROM scratch (no on-disk zoneinfo files),
	// so the IANA timezone database is embedded in the binary instead. This
	// is what lets ENV TZ=Asia/Tokyo (set in Dockerfile) resolve to
	// a real location rather than falling back to UTC.
	_ "time/tzdata"

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
