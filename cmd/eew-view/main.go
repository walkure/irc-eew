// Command eew-view serves a read-only web viewer for raw EEW telegrams
// archived by eew-notifier (see internal/eewlog), replacing HTML/index.pl +
// HTML/eew-show.pl (Perl CGI::Fast behind lighttpd) with a single plain
// net/http server — nothing about this component requires FastCGI.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// The production image runs FROM scratch (no on-disk zoneinfo files),
	// so the IANA timezone database is embedded in the binary instead. See
	// Dockerfile.eew-view.golang's ENV TZ=Asia/Tokyo.
	_ "time/tzdata"

	"github.com/walkure/irc-eew/internal/eewview"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg := eewview.LoadConfigFromEnv()

	handler, err := eewview.NewServer(cfg)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "error", err)
		}
	}()

	slog.Info("eew-view listening", "addr", cfg.ListenAddr, "data_dir", cfg.DataDir, "viewer_path", cfg.ViewerPath)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
