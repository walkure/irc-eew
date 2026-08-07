package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/slack"
)

func TestNotifier_Send(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding posted body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := slack.New()
	err := n.Send(context.Background(), slack.Hook{Name: "test", URL: srv.URL}, "地震です", "緊急地震速報")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", gotMethod)
	}
	if gotContentType != "application/json; charset=utf-8" {
		t.Errorf("content-type: got %q", gotContentType)
	}
	if gotBody["text"] != "地震です" {
		t.Errorf("text: got %q", gotBody["text"])
	}
	if gotBody["username"] != "緊急地震速報" {
		t.Errorf("username: got %q", gotBody["username"])
	}
}

func TestNotifier_Send_NonRetryableStatusFailsImmediately(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusBadRequest) // 4xx other than 429: not retryable
	}))
	defer srv.Close()

	n := slack.New()
	err := n.Send(context.Background(), slack.Hook{URL: srv.URL}, "x", "y")
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("expected exactly 1 request (no retry) for a non-retryable status, got %d", got)
	}
}

func TestNotifier_Send_RetriesOn429WithRetryAfterThenSucceeds(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&requests, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := slack.New()
	start := time.Now()
	err := n.Send(context.Background(), slack.Hook{URL: srv.URL}, "x", "y")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Send: %v (expected it to succeed after retrying)", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("expected exactly 2 requests (1 failure + 1 retry), got %d", got)
	}
	if elapsed < 800*time.Millisecond {
		t.Errorf("expected the retry to honor the ~1s Retry-After header, only waited %v", elapsed)
	}
}

func TestNotifier_Send_GivesUpAfterMaxAttemptsOn500(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Retry-After", "1") // keep the test's wall-clock time small and predictable
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := slack.New()
	err := n.Send(context.Background(), slack.Hook{URL: srv.URL}, "x", "y")
	if err == nil {
		t.Fatal("expected an error after exhausting retries against a persistently-failing endpoint")
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Errorf("expected exactly 3 attempts (maxAttempts), got %d", got)
	}
}

func TestNotifier_Send_UnreachableHostIsAnError(t *testing.T) {
	n := slack.New()
	start := time.Now()
	err := n.Send(context.Background(), slack.Hook{URL: "https://127.0.0.1:1/does-not-exist"}, "x", "y")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
	// A connection-refused/dial error is not retried (it isn't a
	// retryableError), so this should fail fast, not after 429/5xx backoff.
	if elapsed > 5*time.Second {
		t.Errorf("expected a fast failure for an unreachable host (not retried), took %v", elapsed)
	}
}
