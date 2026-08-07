// Package app wires config, the WNI client, the decoder/formatter, and the
// Slack notifier together into the running daemon, porting irc-eew.pl's
// main loop (minus everything IRC-related).
package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/eewlog"
	"github.com/walkure/irc-eew/internal/eewmsg"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
	"github.com/walkure/irc-eew/internal/wni"
)

// idleTimeout matches irc-eew.pl's `can_read(60 * 3.5)` (210s) select
// timeout. Unlike the Perl original (which treats this as fatal and exits,
// relying on an external restart policy), reaching it here just triggers a
// reconnect — see runConnectionLoop.
const idleTimeout = 210 * time.Second

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
		log.Printf("logdir: %s", cfg.LogDir)
	} else {
		log.Printf("logdir: not set, raw telegrams will not be saved")
	}

	var allHooks, limitedHooks []slack.Hook
	if cfg.Slack != nil {
		allHooks = toSlackHooks(cfg.Slack.All)
		limitedHooks = toSlackHooks(cfg.Slack.Limited)
	}
	log.Printf("slack: %d 'all' hook(s), %d 'limited' hook(s)", len(allHooks), len(limitedHooks))

	processor := NewProcessor(store, notify.NewDispatcher(allHooks, limitedHooks))
	notifier := slack.New()

	creds := wni.Credentials{
		User:      cfg.WNIEEW.User,
		Passwd:    cfg.WNIEEW.Passwd,
		PasswdMD5: cfg.WNIEEW.PasswdMD5,
	}

	onData := func(md5hex string, body []byte) {
		result := processor.Process(md5hex, body)
		log.Printf("received: %s", result.Text)
		dispatchToSlack(notifier, result)
	}

	return runConnectionLoop(ctx, creds, cfg.WNIEEW.ServerOverride, bool(cfg.WNIEEW.Logs.KeepAlive), onData)
}

func toSlackHooks(hooks []config.Hook) []slack.Hook {
	out := make([]slack.Hook, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, slack.Hook{Name: h.DisplayName(), URL: h.URL})
	}
	return out
}

// dispatchToSlack fires one goroutine per hook so a slow or unreachable
// webhook can never stall the WNI read loop (which calls onData
// synchronously) — unlike Perl's select()-multiplexed one-shot sockets,
// Go's http.Client.Do blocks its calling goroutine.
func dispatchToSlack(notifier *slack.Notifier, result ProcessResult) {
	for _, hook := range result.Hooks {
		hook := hook
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), slackSendTimeout)
			defer cancel()
			if err := notifier.Send(ctx, hook, result.Text, result.Title); err != nil {
				log.Printf("slack: %v", err)
				return
			}
			log.Printf("slack: notified %s", hook.Name)
		}()
	}
}

// runConnectionLoop owns the WNI connection lifecycle: fetch a server,
// connect, log in, read until disconnect/idle-timeout/error, then retry
// with capped exponential backoff (reset on every successful connect+login)
// — unlike Perl's unbounded, backoff-free retry loop.
func runConnectionLoop(ctx context.Context, creds wni.Credentials, serverOverride string, logKeepAlive bool, onData func(md5 string, body []byte)) error {
	const initialBackoff = 1 * time.Second
	const maxBackoff = 60 * time.Second
	backoff := initialBackoff

	handlers := wni.Handlers{
		OnData: onData,
		OnResponse: func(h map[string]string) {
			log.Printf("wni: login response: %s", h["X-WNI-Result"])
		},
	}
	if logKeepAlive {
		handlers.OnKeepAlive = func() {
			log.Printf("wni: keep-alive")
		}
	}

	resolver := &serverResolver{override: serverOverride}

	for {
		if ctx.Err() != nil {
			return nil
		}

		client, addr, err := connectAndLogin(ctx, creds, resolver)
		if err != nil {
			log.Printf("wni: %v; retrying in %s", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		log.Printf("wni: connected to %s", addr)
		backoff = initialBackoff

		runErr := client.Run(ctx, idleTimeout, handlers)
		client.Close()

		if ctx.Err() != nil {
			return nil
		}
		log.Printf("wni: disconnected (%v), reconnecting", runErr)
	}
}

// connectAndLogin resolves a server address, dials it, and logs in — a
// single failure-checked unit so runConnectionLoop's retry logic stays flat.
func connectAndLogin(ctx context.Context, creds wni.Credentials, resolver *serverResolver) (*wni.Client, string, error) {
	addr, err := resolver.Resolve(ctx)
	if err != nil {
		return nil, "", err
	}

	client, err := wni.Dial(ctx, addr)
	if err != nil {
		return nil, "", err
	}

	if err := client.Login(creds); err != nil {
		client.Close()
		return nil, "", fmt.Errorf("login: %w", err)
	}

	return client, addr, nil
}

// serverResolver resolves the WNI relay server address to connect to next.
//
// If override is set (see config.WNIConfig.ServerOverride, used to point at
// a test/fake WNI server), it's always returned unchanged.
//
// Otherwise it fetches a fresh server list from WNI on every call — so a
// long-running process picks up relay-set changes over time, unlike the
// Perl original which fetches the list exactly once at startup and reuses
// it for every reconnect — but falls back to the last successfully fetched
// list if the refresh fails, since a transient outage of the list endpoint
// shouldn't by itself prevent reconnecting to relay servers that may still
// be perfectly reachable.
type serverResolver struct {
	override string
	cached   []string
	// fetch is overridable for tests; nil means fetch from the real WNI
	// server-list endpoint.
	fetch func(ctx context.Context) ([]string, error)
}

func (r *serverResolver) Resolve(ctx context.Context) (string, error) {
	if r.override != "" {
		return r.override, nil
	}

	fetch := r.fetch
	if fetch == nil {
		fetch = func(ctx context.Context) ([]string, error) {
			return wni.FetchServerList(ctx, wni.ServerListURL)
		}
	}

	servers, err := fetch(ctx)
	if err != nil {
		if len(r.cached) == 0 {
			return "", err
		}
		log.Printf("wni: server list refresh failed (%v), reusing last known list of %d server(s)", err, len(r.cached))
		return wni.ChooseServer(r.cached), nil
	}

	r.cached = servers
	return wni.ChooseServer(servers), nil
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
			log.Printf("eewlog: %v", err)
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
			log.Printf("eewlog: %v", err)
		} else {
			log.Printf("eewlog: saved %s", finalPath)
		}
	}

	return ProcessResult{Telegram: tel, Text: text, Title: title, Hooks: hooks}
}
