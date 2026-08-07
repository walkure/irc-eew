package notify_test

import (
	"testing"

	"github.com/walkure/irc-eew/internal/decoder"
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

	hooks := d.HooksFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
	if !containsName(hooks, "all-1") || !containsName(hooks, "limited-1") {
		t.Fatalf("expected both all and limited hooks for a new quake's first report, got %v", names(hooks))
	}
}

func TestDispatcher_RoutineReportOfSameQuakeSkipsLimited(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	eqID := "20260807000000"
	d.HooksFor(&decoder.Telegram{EqID: eqID, WarnNum: 1}) // first report establishes lastEqID

	hooks := d.HooksFor(&decoder.Telegram{EqID: eqID, WarnNum: 2}) // routine follow-up
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
	d.HooksFor(&decoder.Telegram{EqID: eqID, WarnNum: 1})

	hooks := d.HooksFor(&decoder.Telegram{EqID: eqID, WarnNum: 42, NCNType: 9})
	if !containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to fire for a final/amended report, got %v", names(hooks))
	}
}

func TestDispatcher_CancellationFiresLimitedEvenForSameQuake(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	eqID := "20260807000000"
	d.HooksFor(&decoder.Telegram{EqID: eqID, WarnNum: 1})

	hooks := d.HooksFor(&decoder.Telegram{EqID: eqID, WarnNum: 2, MsgTypeCode: 10})
	if !containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to fire for a cancellation, got %v", names(hooks))
	}
}

func TestDispatcher_NewQuakeAfterAnotherAlwaysFiresLimited(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	limited := []slack.Hook{{Name: "limited-1"}}
	d := notify.NewDispatcher(all, limited)

	d.HooksFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
	d.HooksFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 2}) // routine, lastEqID unchanged

	hooks := d.HooksFor(&decoder.Telegram{EqID: "20260807010000", WarnNum: 1}) // different quake
	if !containsName(hooks, "limited-1") {
		t.Errorf("expected limited hooks to fire for a genuinely new quake, got %v", names(hooks))
	}
}

func TestDispatcher_EmptyLimitedList(t *testing.T) {
	all := []slack.Hook{{Name: "all-1"}}
	d := notify.NewDispatcher(all, nil)

	hooks := d.HooksFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
	if len(hooks) != 1 || hooks[0].Name != "all-1" {
		t.Errorf("expected only the all hook when no limited hooks are configured, got %v", names(hooks))
	}
}
