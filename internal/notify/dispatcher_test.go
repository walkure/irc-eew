package notify_test

import (
	"testing"

	"github.com/walkure/irc-eew/internal/eew"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
)

func names(hooks []slack.Hook) []string {
	var out []string
	for _, h := range hooks {
		out = append(out, h.Name)
	}
	return out
}

func containsName(hooks []slack.Hook, name string) bool {
	for _, h := range hooks {
		if h.Name == name {
			return true
		}
	}
	return false
}

func TestDispatcher_FirstReportOfNewQuakeFiresLimited(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	hooks := d.HooksFor(eew.Event{EventID: "20260807000000"})
	if !containsName(hooks, "all-1") || !containsName(hooks, "limited-1") {
		t.Fatalf("expected both all and limited hooks for a new quake's first report, got %v", names(hooks))
	}
}

func TestDispatcher_RoutineReportOfSameQuakeSkipsLimited(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	eqID := "20260807000000"
	d.HooksFor(eew.Event{EventID: eqID}) // first report marks the EventID as seen

	hooks := d.HooksFor(eew.Event{EventID: eqID}) // routine follow-up
	if !containsName(hooks, "all-1") {
		t.Errorf("expected all hooks for a routine report, got %v", names(hooks))
	}
	if containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to be skipped for a routine same-quake report, got %v", names(hooks))
	}
}

func TestDispatcher_FinalReportOfSameQuakeFiresLimited(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	eqID := "20260807000000"
	d.HooksFor(eew.Event{EventID: eqID})

	hooks := d.HooksFor(eew.Event{EventID: eqID, Final: true})
	if !containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to fire for a final/amended report, got %v", names(hooks))
	}
}

func TestDispatcher_CancellationFiresLimitedEvenForSameQuake(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	eqID := "20260807000000"
	d.HooksFor(eew.Event{EventID: eqID})

	hooks := d.HooksFor(eew.Event{EventID: eqID, Cancelled: true})
	if !containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to fire for a cancellation, got %v", names(hooks))
	}
}

func TestDispatcher_NewQuakeAfterAnotherAlwaysFiresLimited(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	d.HooksFor(eew.Event{EventID: "20260807000000"})
	d.HooksFor(eew.Event{EventID: "20260807000000"}) // routine

	hooks := d.HooksFor(eew.Event{EventID: "20260807010000"}) // different quake
	if !containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to fire for a genuinely new quake, got %v", names(hooks))
	}
}

func TestDispatcher_EmptyLimitedList(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	d := notify.NewDispatcher(all, nil)

	hooks := d.HooksFor(eew.Event{EventID: "20260807000000"})
	if len(hooks) != 1 || hooks[0].Name != "all-1" {
		t.Errorf("expected only the all hook when no limited hooks are configured, got %v", names(hooks))
	}
}

// Reports of two earthquakes interleaving must fire the limited tier once
// per earthquake (plus final/cancellation), not on every switch between
// them as irc-eew.pl's single last-eq_id memory did.
func TestDispatcher_InterleavedQuakesFireLimitedOncePerQuake(t *testing.T) {
	d := notify.NewDispatcher([]slack.Hook{{Name: "all-1"}}, []slack.Hook{{Name: "limited-1"}})
	steps := []struct {
		ev          eew.Event
		wantLimited bool
	}{
		{eew.Event{EventID: "A"}, true},
		{eew.Event{EventID: "B"}, true},
		{eew.Event{EventID: "A"}, false},
		{eew.Event{EventID: "B"}, false},
		{eew.Event{EventID: "A", Final: true}, true},
		{eew.Event{EventID: "B"}, false},
		{eew.Event{EventID: "B", Cancelled: true}, true},
	}
	for i, s := range steps {
		if got := containsName(d.HooksFor(s.ev), "limited-1"); got != s.wantLimited {
			t.Errorf("step %d (%+v): limited fired = %v, want %v", i, s.ev, got, s.wantLimited)
		}
	}
}
