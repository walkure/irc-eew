// Package app wires config, the DMDATA.JP feed client, the
// decoder/formatter, and the Slack/IRC notifiers together into the running
// daemon.
//
// TODO(dmdata): the upstream feed was WNI FastCaster until WNI ended it.
// Its DMDATA.JP replacement currently only receives and archives (see Run);
// decoding and notification (Processor, dispatchTo*) are still wired for
// the legacy code-telegram format and get reattached in phase 2.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/dmdata"
	"github.com/walkure/irc-eew/internal/eewlog"
	"github.com/walkure/irc-eew/internal/eewmsg"
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
// an error for startup misconfiguration that can't self-heal (no API key,
// an unwritable archive dir).
//
// Phase 1 of the DMDATA.JP migration: telegrams are received and archived
// verbatim, and their metadata logged, but nothing is decoded or notified
// yet — the payload format has to be confirmed against real deliveries
// first. IRC connections are still brought up so the bot stays present.
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

	ircConns := make([]*irc.Connection, 0, len(cfg.IRC))
	for _, srv := range cfg.IRC {
		ircConns = append(ircConns, irc.NewConnection(srv))
	}
	slog.Info("irc servers configured", "count", len(ircConns))

	var ircWG sync.WaitGroup
	for _, conn := range ircConns {
		ircWG.Add(1)
		go func(conn *irc.Connection) {
			defer ircWG.Done()
			conn.Run(ctx)
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

	runFeedLoop(ctx, client, handlers)

	// Wait for every IRC connection to finish its own graceful shutdown
	// (QUIT + Close, triggered by ctx being canceled) before returning, so
	// the process doesn't exit out from under them.
	ircWG.Wait()
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

// runFeedLoop runs DMDATA sessions back to back until ctx is canceled,
// waiting with capped exponential backoff (1s..60s) between them. Each
// session gets a fresh single-use ticket (see dmdata.Client.RunSession).
func runFeedLoop(ctx context.Context, client *dmdata.Client, h dmdata.Handlers) {
	const initialBackoff = 1 * time.Second
	const maxBackoff = 60 * time.Second
	backoff := initialBackoff

	for ctx.Err() == nil {
		established, err := client.RunSession(ctx, h)
		if ctx.Err() != nil {
			return
		}
		if !established.IsZero() && time.Since(established) >= stableConnection {
			backoff = initialBackoff
		}
		slog.Warn("dmdata session ended, reconnecting", "error", err, "backoff", backoff)
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
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
		conn.Notify(result.Telegram, result.Text)
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

// Processor turns a raw received telegram into a decoded Telegram, its
// Slack message text/title, and the list of hooks it should go to — kept
// separate from the actual (network I/O) sending so it's cheaply testable.
type Processor struct {
	store      *eewlog.Store
	dispatcher *notify.Dispatcher
}

// NewProcessor creates a Processor. store may be nil (raw archival
// disabled, matching an unset `logdir:` in config).
func NewProcessor(store *eewlog.Store, dispatcher *notify.Dispatcher) *Processor {
	return &Processor{store: store, dispatcher: dispatcher}
}

// ProcessResult is everything the caller needs to notify Slack and log.
type ProcessResult struct {
	Telegram *decoder.Telegram
	Text     string
	Title    string
	Hooks    []slack.Hook
}

// Process decodes body, formats the message, determines which hooks should
// be notified, and (if archival is enabled) saves the raw bytes under their
// final eq_id/warn_num-based path — porting eew_callback's non-notification
// logic (irc-eew.pl lines ~148-229, minus the IRC branch).
func (p *Processor) Process(md5hex string, body []byte) ProcessResult {
	var tempPath string
	if p.store != nil {
		tp, err := p.store.WriteTemp(md5hex, body)
		if err != nil {
			slog.Error("eewlog write temp failed", "error", err)
		} else {
			tempPath = tp
		}
	}

	tel := decoder.Decode(body)
	text, title := eewmsg.Format(tel)
	hooks := p.dispatcher.HooksFor(tel)

	if p.store != nil && tempPath != "" {
		finalPath, err := p.store.Finalize(tempPath, tel.EqID, tel.WarnNum)
		if err != nil {
			slog.Error("eewlog finalize failed", "error", err)
		} else {
			slog.Info("eewlog saved", "path", finalPath)
		}
	}

	return ProcessResult{Telegram: tel, Text: text, Title: title, Hooks: hooks}
}
