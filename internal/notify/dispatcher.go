// Package notify decides which configured Slack webhooks should receive a
// given EEW report.
package notify

import (
	"github.com/walkure/irc-eew/internal/eew"
	"github.com/walkure/irc-eew/internal/slack"
)

// Dispatcher selects hooks for each report: "all" hooks always fire,
// "limited" hooks only fire for the first report of an earthquake, a
// final report, or a cancellation.
//
// "First report of an earthquake" means an EventID not among the most
// recently seen ones (eew.RecentSet, bounded). This deliberately departs
// from irc-eew.pl, which only remembered the single last eq_id: with
// several earthquakes' reports interleaving (common), that refired the
// limited tier on every switch between them. State is in-memory only and
// not persisted across restarts, as before.
type Dispatcher struct {
	all, limited []slack.Hook
	seen         *eew.RecentSet
}

// NewDispatcher creates a Dispatcher for the given "all" and "limited" hook
// lists (either may be empty/nil).
func NewDispatcher(all, limited []slack.Hook) *Dispatcher {
	return &Dispatcher{all: all, limited: limited, seen: eew.NewRecentSet(0)}
}

// HooksFor returns the hooks that should be notified for ev.
func (d *Dispatcher) HooksFor(ev eew.Event) []slack.Hook {
	hooks := make([]slack.Hook, 0, len(d.all)+len(d.limited))
	hooks = append(hooks, d.all...)

	isNew := d.seen.Add(ev.EventID)
	if isNew || ev.Cancelled || ev.Final {
		hooks = append(hooks, d.limited...)
	}
	return hooks
}
