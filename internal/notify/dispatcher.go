// Package notify decides which configured Slack webhooks should receive a
// given decoded telegram.
package notify

import (
	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/slack"
)

// Dispatcher selects hooks for each telegram: "all" hooks always fire,
// "limited" hooks only fire for a new earthquake, a cancellation, or a
// final/amended report — porting eew_callback's dedup logic (irc-eew.pl
// lines ~189-193) exactly, including its state (last-seen eq_id, reset only
// when the limited tier actually fires) and its in-memory-only, not
// persisted-across-restarts nature.
type Dispatcher struct {
	all, limited []slack.Hook
	lastEqID     string
}

// NewDispatcher creates a Dispatcher for the given "all" and "limited" hook
// lists (either may be empty/nil).
func NewDispatcher(all, limited []slack.Hook) *Dispatcher {
	return &Dispatcher{all: all, limited: limited}
}

// HooksFor returns the hooks that should be notified for t.
func (d *Dispatcher) HooksFor(t *decoder.Telegram) []slack.Hook {
	hooks := make([]slack.Hook, 0, len(d.all)+len(d.limited))
	hooks = append(hooks, d.all...)

	if t.EqID != d.lastEqID || t.IsCancellation() || t.IsFinal() {
		d.lastEqID = t.EqID
		hooks = append(hooks, d.limited...)
	}
	return hooks
}
