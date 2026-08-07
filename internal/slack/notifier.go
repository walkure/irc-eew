// Package slack sends EEW notifications to Slack incoming-webhooks.
//
// This replaces SlackWebhookSock.pm's hand-rolled raw-socket HTTP/1.0 POST
// (built to work around IO::Socket::SSL not having a real HTTP client on
// top of it) with plain net/http — there is no protocol reason to keep the
// raw-socket approach in Go.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Retry tuning for transient failures (429 rate-limit, 5xx). Slack's
// incoming-webhook rate limit is roughly 1 message/sec per webhook, and a
// single quake can produce dozens of reports within a couple of minutes
// (the real archive corpus has events with up to 81 reports in ~2 minutes)
// — every one fans out to every "all" tier hook, so hitting 429 on a busy
// event is a real, expected scenario rather than an edge case.
const (
	maxAttempts      = 3
	defaultRetryWait = 1 * time.Second // used when a 429/5xx response has no Retry-After
	maxRetryWait     = 10 * time.Second
)

// Hook is one Slack incoming-webhook endpoint. Name is used only for
// logging/error messages (as in SlackWebhookSock.pm's `name()`), defaulting
// to the URL itself when not given a label in config.
type Hook struct {
	Name string
	URL  string
}

func (h Hook) label() string {
	if h.Name != "" {
		return h.Name
	}
	return h.URL
}

// Notifier posts EEW messages to Slack webhooks.
type Notifier struct {
	client *http.Client
}

// New creates a Notifier with a short per-request timeout so a slow or dead
// webhook can never block the caller for long.
func New() *Notifier {
	return &Notifier{client: &http.Client{Timeout: 10 * time.Second}}
}

// retryableError signals a 429 or 5xx response — worth retrying — as
// opposed to a 4xx (bad URL, revoked webhook, malformed payload) which
// retrying can't fix.
type retryableError struct {
	status     string
	retryAfter time.Duration
}

func (e *retryableError) Error() string { return fmt.Sprintf("responded with status %s", e.status) }

// Send posts {"text": text, "username": username} to hook, matching the
// payload shape SlackWebhookSock::send_json builds in eew_callback.
//
// On a 429 or 5xx response it retries up to maxAttempts times, honoring
// Slack's Retry-After header when present (falling back to a small linear
// backoff when absent), capped at maxRetryWait per attempt so one stuck
// hook can't stall a burst of reports for an unbounded time. ctx's deadline
// (set by the caller, see internal/app's per-hook send timeout) bounds the
// whole operation regardless.
func (n *Notifier) Send(ctx context.Context, hook Hook, text, username string) error {
	payload, err := json.Marshal(map[string]string{
		"text":     text,
		"username": username,
	})
	if err != nil {
		return fmt.Errorf("slack: encoding payload: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := n.sendOnce(ctx, hook, payload)
		if err == nil {
			if attempt > 1 {
				log.Printf("slack: %s: succeeded on attempt %d/%d", hook.label(), attempt, maxAttempts)
			}
			return nil
		}
		lastErr = err

		var re *retryableError
		if !errors.As(err, &re) || attempt == maxAttempts {
			break
		}

		wait := re.retryAfter
		if wait <= 0 {
			wait = defaultRetryWait * time.Duration(attempt)
		}
		if wait > maxRetryWait {
			wait = maxRetryWait
		}

		log.Printf("slack: %s: attempt %d/%d failed (%s), retrying in %s", hook.label(), attempt, maxAttempts, re.status, wait)

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return fmt.Errorf("slack: %s: giving up after %d attempt(s), context done: %w", hook.label(), attempt, ctx.Err())
		}
	}
	return fmt.Errorf("slack: %s: %w", hook.label(), lastErr)
}

func (n *Notifier) sendOnce(ctx context.Context, hook Hook, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building request for %s: %w", hook.label(), err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("posting to %s: %w", hook.label(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return &retryableError{
			status:     resp.Status,
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s responded with status %s", hook.label(), resp.Status)
	}
	return nil
}

// parseRetryAfter accepts both forms the header may take: an integer
// seconds count (what Slack sends) or an HTTP date.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
