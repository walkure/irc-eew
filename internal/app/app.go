// Package app wires config, the upstream EEW feed client, the
// decoder/formatter, and the Slack/IRC notifiers together into the running
// daemon.
//
// TODO(dmdata): the upstream feed was WNI FastCaster until it was shut
// down; it is being replaced by DMDATA.JP's WebSocket v2 API.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/eewlog"
	"github.com/walkure/irc-eew/internal/eewmsg"
	"github.com/walkure/irc-eew/internal/irc"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
)

// slackSendTimeout bounds one hook's Send call including any internal
// retries on 429/5xx (see internal/slack.Notifier.Send) — generous enough
// to cover a couple of Retry-After waits (capped at 10s each) plus request
// round-trips.
const slackSendTimeout = 30 * time.Second

// Run wires everything together and runs until ctx is canceled, or returns
// an error for conditions Perl also treated as fatal at startup (an
// unwritable logdir) that can't reasonably be self-healed.
func Run(ctx context.Context, cfg *config.Config) error {
	var store *eewlog.Store
	if cfg.LogDir != "" {
		s, err := eewlog.Open(cfg.LogDir)
		if err != nil {
			return fmt.Errorf("app: %w", err)
		}
		store = s
		slog.Info("logdir enabled", "path", cfg.LogDir)
	} else {
		slog.Info("logdir not set, raw telegrams will not be saved")
	}

	var allHooks, limitedHooks []slack.Hook
	if cfg.Slack != nil {
		allHooks = toSlackHooks(cfg.Slack.All)
		limitedHooks = toSlackHooks(cfg.Slack.Limited)
	}
	slog.Info("slack hooks configured", "all", len(allHooks), "limited", len(limitedHooks))

	processor := NewProcessor(store, notify.NewDispatcher(allHooks, limitedHooks))
	notifier := slack.New()

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

	onData := func(md5hex string, body []byte) {
		result := processor.Process(md5hex, body)
		slog.Info("received telegram", "text", result.Text)
		dispatchToSlack(notifier, result)
		dispatchToIRC(ircConns, result)
	}

	// TODO(dmdata): run the DMDATA.JP WebSocket receive loop here, calling
	// onData for each EEW telegram. Until then the daemon only keeps its IRC
	// connections up.
	_ = onData
	<-ctx.Done()
	var err error
	// Wait for every IRC connection to finish its own graceful shutdown
	// (QUIT + Close, triggered by ctx being canceled) before returning, so
	// the process doesn't exit out from under them.
	ircWG.Wait()
	return err
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
