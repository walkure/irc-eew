package irc

import "github.com/walkure/irc-eew/internal/decoder"

// Dispatcher selects the channels that should receive a given telegram, for
// one IRC connection. It mirrors internal/notify.Dispatcher's "all always
// fires, limited fires only for a new earthquake, a cancellation, or a
// final/amended report" logic (irc-eew.pl's eew_callback dedup), but each
// Dispatcher instance keeps its own lastEqID — deliberately independent
// from internal/notify.Dispatcher (Slack) and from every other IRC
// connection's Dispatcher, unlike irc-eew.pl's single shared $last_eq_id.
type Dispatcher struct {
	all, limited []string
	lastEqID     string
}

// NewDispatcher creates a Dispatcher for the given "all-notice" and
// "limited-notice" channel lists (either may be empty/nil).
func NewDispatcher(all, limited []string) *Dispatcher {
	return &Dispatcher{all: all, limited: limited}
}

// JoinChannels returns the deduplicated union of the all-notice and
// limited-notice channel lists, for JOINing when the connection is
// established — mirroring irc-eew.pl's @join_ch construction.
func (d *Dispatcher) JoinChannels() []string {
	return dedup(d.all, d.limited)
}

// ChannelsFor returns the deduplicated channels that should be notified for
// t, and the Queue Priority that notice should carry. A channel listed in
// both all-notice and limited-notice is only notified once per report
// (irc-eew.pl would notify it twice in that configuration).
func (d *Dispatcher) ChannelsFor(t *decoder.Telegram) ([]string, Priority) {
	limitedFires := t.EqID != d.lastEqID || t.IsCancellation() || t.IsFinal()
	if limitedFires {
		d.lastEqID = t.EqID
	}

	if limitedFires {
		return dedup(d.all, d.limited), priorityFor(t)
	}
	return dedup(d.all), priorityFor(t)
}

func priorityFor(t *decoder.Telegram) Priority {
	switch {
	case t.IsCancellation():
		return PriorityCancellation
	case t.IsFinal():
		return PriorityFinal
	default:
		return PriorityNormal
	}
}

func dedup(lists ...[]string) []string {
	var total int
	for _, l := range lists {
		total += len(l)
	}
	seen := make(map[string]bool, total)
	out := make([]string, 0, total)
	for _, l := range lists {
		for _, ch := range l {
			if !seen[ch] {
				seen[ch] = true
				out = append(out, ch)
			}
		}
	}
	return out
}
