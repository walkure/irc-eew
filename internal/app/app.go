// Package app wires config, the DMDATA.JP feed client, the VXSE45
// parser/formatter, and the Slack/IRC notifiers together into the running
// daemon. (The feed was WNI FastCaster until WNI ended it.)
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/irc"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/rawarchive"
	"github.com/walkure/irc-eew/internal/slack"
)

// slackSendTimeout bounds one hook's Send call including any internal
// retries on 429/5xx (see internal/slack.Notifier.Send) — generous enough
// to cover a couple of Retry-After waits (capped at 10s each) plus request
// round-trips.
const slackSendTimeout = 30 * time.Second

// Run wires everything together and runs until ctx is canceled, or returns
// an error for conditions retrying can't fix: startup misconfiguration (no
// API key, an unwritable archive dir) or a fatal DMDATA error (bad key, no
// contract, missing permission, contract terminated...; see
// dmdata.Classify). The process is expected to exit non-zero on the
// latter, so a lapsed contract shows up as a crash loop rather than a
// daemon silently retrying forever.
//
// Every received data message is archived verbatim first (handleData),
// then VXSE45 reports are parsed, formatted in the legacy WNI-era shape and
// sent to Slack and IRC (Processor).
func Run(ctx context.Context, cfg *config.Config) error {
	dm := cfg.DMDATA.WithDefaults()
	if dm.APIKey == "" {
		return fmt.Errorf("app: dmdata.api-key is required")
	}

	var archive *rawarchive.Store
	if dm.ArchiveDir != "" {
		a, err := rawarchive.Open(dm.ArchiveDir)
		if err != nil {
			return fmt.Errorf("app: %w", err)
		}
		archive = a
		slog.Info("dmdata archive enabled", "path", dm.ArchiveDir)
	} else {
		slog.Warn("dmdata.archive-dir not set, received messages will not be saved")
	}

	if cfg.LogDir != "" {
		slog.Info("logdir is the legacy WNI archive and is no longer written; received data goes to dmdata.archive-dir", "logdir", cfg.LogDir)
	}

	var allHooks, limitedHooks []slack.Hook
	if cfg.Slack != nil {
		allHooks = toSlackHooks(cfg.Slack.All)
		limitedHooks = toSlackHooks(cfg.Slack.Limited)
	}
	slog.Info("slack hooks configured", "all", len(allHooks), "limited", len(limitedHooks))
	processor := NewProcessor(notify.NewDispatcher(allHooks, limitedHooks))
	notifier := slack.New()

	ircConns := make([]*irc.Connection, 0, len(cfg.IRC))
	for _, srv := range cfg.IRC {
		ircConns = append(ircConns, irc.NewConnection(srv))
	}
	slog.Info("irc servers configured", "count", len(ircConns))

	// IRC connections run on their own context so a fatal feed error can
	// shut them down gracefully before Run returns.
	ircCtx, stopIRC := context.WithCancel(ctx)
	defer stopIRC()
	var ircWG sync.WaitGroup
	for _, conn := range ircConns {
		ircWG.Add(1)
		go func(conn *irc.Connection) {
			defer ircWG.Done()
			conn.Run(ircCtx)
		}(conn)
	}

	client := &dmdata.Client{
		API: dmdata.NewAPI(dm.APIBase, dm.APIKey),
		Request: dmdata.SocketStartRequest{
			Classifications: dm.Classifications,
			Types:           dm.Types,
			Test:            dm.Test,
			AppName:         dm.AppName,
			FormatMode:      dm.FormatMode,
		},
	}
	handlers := dmdata.Handlers{
		OnStart: func(m dmdata.StartMessage) {
			slog.Info("dmdata websocket started",
				"socket_id", m.SocketID, "classifications", m.Classifications,
				"types", m.Types, "test", m.Test, "formats", m.Formats)
		},
		OnPing: func(pingID string) {
			slog.Debug("dmdata ping answered", "ping_id", pingID)
		},
		OnData: func(m *dmdata.DataMessage) {
			handleData(archive, m)
			result, ok := processor.Process(m)
			if !ok {
				return
			}
			slog.Info("eew notification", "event_id", result.Event.EventID, "text", result.Text)
			dispatchToSlack(notifier, result)
			dispatchToIRC(ircConns, result)
		},
	}

	// A previous run that died without Socket Close (SIGKILL, OOM, node
	// loss) may still hold a connection slot; release it up front instead
	// of waiting out the server's ping timeout. Failure (e.g. the key lacks
	// socket.list) is not fatal: the 409 path in RunSession retries it, and
	// the slot frees itself eventually anyway.
	if closed, err := client.CloseStale(ctx); err != nil {
		slog.Warn("dmdata stale socket cleanup failed", "error", err)
	} else if closed > 0 {
		slog.Info("dmdata stale sockets closed at startup", "count", closed)
	}

	feedErr := runFeedLoop(ctx, client, handlers)

	// Wait for every IRC connection to finish its own graceful shutdown
	// (QUIT + Close, triggered by the context being canceled) before
	// returning, so the process doesn't exit out from under them.
	stopIRC()
	ircWG.Wait()
	if feedErr != nil {
		return fmt.Errorf("app: %w", feedErr)
	}
	return nil
}

// bodyPreviewLen is how much of a decoded body handleData logs, enough to
// tell XML, DMDATA JSON and a legacy code telegram apart at a glance.
const bodyPreviewLen = 80

// handleData archives a data message verbatim first (so it's kept even if
// anything after fails), then logs what it turned out to contain.
func handleData(archive *rawarchive.Store, m *dmdata.DataMessage) {
	path := ""
	if archive != nil {
		p, existed, err := archive.Save(m.ID, m.Raw)
		switch {
		case err != nil:
			slog.Error("dmdata archive failed", "id", m.ID, "error", err)
		case existed:
			slog.Info("dmdata duplicate message, already archived", "id", m.ID, "path", p)
		default:
			path = p
		}
	}

	attrs := []any{
		"id", m.ID,
		"classification", m.Classification,
		"type", m.Head.Type,
		"author", m.Head.Author,
		"head_time", m.Head.Time,
		"designation", dmdata.StrOrEmpty(m.Head.Designation),
		"test", m.Head.Test,
		"format", dmdata.StrOrEmpty(m.Format),
		"compression", dmdata.StrOrEmpty(m.Compression),
		"encoding", dmdata.StrOrEmpty(m.Encoding),
		"path", path,
	}
	body, err := m.DecodeBody()
	if err != nil {
		slog.Warn("dmdata data received, body undecodable", append(attrs, "error", err)...)
		return
	}
	preview := body
	if len(preview) > bodyPreviewLen {
		preview = preview[:bodyPreviewLen]
	}
	slog.Info("dmdata data received", append(attrs, "body_len", len(body), "body_head", string(preview))...)
}

// stableConnection is how long a session must have stayed up before its
// end resets the reconnect backoff — the same rule as the IRC connections,
// so a feed that accepts and then immediately drops us can't make the
// loop spin with no delay.
const stableConnection = 30 * time.Second

// runFeedLoop runs DMDATA sessions back to back until ctx is canceled
// (returning nil) or a session fails with a dmdata.Fatal error (returning
// it), waiting with capped exponential backoff (1s..60s) between them.
// Each session gets a fresh single-use ticket (see dmdata.Client.RunSession).
//
// A server restart (4503, incl. the monthly maintenance) gets no special
// backoff treatment: after a normal, long-lived session the stable-
// connection rule already resets the wait to 1s, while a server answering
// every new connection with 4503 still gets backed off instead of hammered.
func runFeedLoop(ctx context.Context, client *dmdata.Client, h dmdata.Handlers) error {
	const initialBackoff = 1 * time.Second
	const maxBackoff = 60 * time.Second
	backoff := initialBackoff

	for ctx.Err() == nil {
		established, err := client.RunSession(ctx, h)
		if ctx.Err() != nil {
			return nil
		}
		severity := dmdata.Classify(err)
		if severity == dmdata.Fatal {
			return err // logged once by the caller (cmd/eew-notifier) on exit
		}
		if !established.IsZero() && time.Since(established) >= stableConnection {
			backoff = initialBackoff
		}
		level := slog.LevelWarn
		if severity == dmdata.ServerRestart {
			level = slog.LevelInfo
		}
		slog.Log(ctx, level, "dmdata session ended, reconnecting", "error", err, "severity", severity, "backoff", backoff)
		if !sleepCtx(ctx, backoff) {
			return nil
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
	return nil
}

func toSlackHooks(hooks []config.Hook) []slack.Hook {
	out := make([]slack.Hook, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, slack.Hook{Name: h.DisplayName(), URL: h.URL})
	}
	return out
}

// dispatchToSlack fires one goroutine per hook so a slow or unreachable
// webhook can never stall the feed read loop (which calls onData
// synchronously) — unlike Perl's select()-multiplexed one-shot sockets,
// Go's http.Client.Do blocks its calling goroutine.
func dispatchToSlack(notifier *slack.Notifier, result ProcessResult) {
	for _, hook := range result.Hooks {
		hook := hook
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), slackSendTimeout)
			defer cancel()
			if err := notifier.Send(ctx, hook, result.Text, result.Title); err != nil {
				slog.Error("slack notify failed", "hook", hook.Name, "error", err)
				return
			}
			slog.Info("slack notified", "hook", hook.Name)
		}()
	}
}

// dispatchToIRC hands result to every configured IRC connection's Notify,
// which does its own channel dispatch and enqueues without blocking (see
// internal/irc.Connection.Notify/Queue) — unlike dispatchToSlack, no
// per-call goroutine is needed here since Notify never does I/O itself.
func dispatchToIRC(conns []*irc.Connection, result ProcessResult) {
	for _, conn := range conns {
		conn.Notify(result.Event, result.Text)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}
