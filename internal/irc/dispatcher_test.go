package irc

import (
	"testing"

	"github.com/walkure/irc-eew/internal/eew"
)

func contains(channels []string, ch string) bool {
	for _, c := range channels {
		if c == ch {
			return true
		}
	}
	return false
}

func TestDispatcher_FirstReportOfNewQuakeFiresLimited(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})

	channels, priority := d.ChannelsFor(eew.Event{EventID: "20260807000000"})
	if !contains(channels, "#all") || !contains(channels, "#limited") {
		t.Fatalf("expected both channels for a new quake's first report, got %v", channels)
	}
	if priority != PriorityNormal {
		t.Errorf("priority: got %v, want PriorityNormal", priority)
	}
}

func TestDispatcher_RoutineReportOfSameQuakeSkipsLimited(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})
	eqID := "20260807000000"
	d.ChannelsFor(eew.Event{EventID: eqID}) // marks the EventID as seen

	channels, _ := d.ChannelsFor(eew.Event{EventID: eqID})
	if !contains(channels, "#all") {
		t.Errorf("expected #all for a routine report, got %v", channels)
	}
	if contains(channels, "#limited") {
		t.Errorf("expected #limited to be skipped for a routine same-quake report, got %v", channels)
	}
}

func TestDispatcher_FinalReportFiresLimitedAndReportsPriority(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})
	eqID := "20260807000000"
	d.ChannelsFor(eew.Event{EventID: eqID})

	channels, priority := d.ChannelsFor(eew.Event{EventID: eqID, Final: true})
	if !contains(channels, "#limited") {
		t.Errorf("expected #limited to fire for a final/amended report, got %v", channels)
	}
	if priority != PriorityFinal {
		t.Errorf("priority: got %v, want PriorityFinal", priority)
	}
}

func TestDispatcher_CancellationFiresLimitedAndReportsPriority(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})
	eqID := "20260807000000"
	d.ChannelsFor(eew.Event{EventID: eqID})

	channels, priority := d.ChannelsFor(eew.Event{EventID: eqID, Cancelled: true})
	if !contains(channels, "#limited") {
		t.Errorf("expected #limited to fire for a cancellation, got %v", channels)
	}
	if priority != PriorityCancellation {
		t.Errorf("priority: got %v, want PriorityCancellation", priority)
	}
}

func TestDispatcher_NewQuakeAfterAnotherAlwaysFiresLimited(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})
	d.ChannelsFor(eew.Event{EventID: "20260807000000"})
	d.ChannelsFor(eew.Event{EventID: "20260807000000"}) // routine

	channels, _ := d.ChannelsFor(eew.Event{EventID: "20260807010000"}) // different quake
	if !contains(channels, "#limited") {
		t.Errorf("expected #limited to fire for a genuinely new quake, got %v", channels)
	}
}

func TestDispatcher_EmptyLimitedList(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, nil)

	channels, _ := d.ChannelsFor(eew.Event{EventID: "20260807000000"})
	if len(channels) != 1 || channels[0] != "#all" {
		t.Errorf("expected only #all when no limited channels are configured, got %v", channels)
	}
}

func TestDispatcher_ChannelInBothListsIsNotDuplicated(t *testing.T) {
	d := NewDispatcher([]string{"#eew", "#all"}, []string{"#eew", "#limited"})

	channels, _ := d.ChannelsFor(eew.Event{EventID: "20260807000000"})
	count := 0
	for _, ch := range channels {
		if ch == "#eew" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("#eew (listed in both all-notice and limited-notice) appeared %d times, want 1", count)
	}
}

func TestDispatcher_JoinChannelsIsDeduplicatedUnion(t *testing.T) {
	d := NewDispatcher([]string{"#eew", "#all"}, []string{"#eew", "#limited"})
	got := d.JoinChannels()
	want := map[string]bool{"#eew": true, "#all": true, "#limited": true}
	if len(got) != len(want) {
		t.Fatalf("JoinChannels: got %v, want 3 unique channels", got)
	}
	for _, ch := range got {
		if !want[ch] {
			t.Errorf("unexpected channel %q in JoinChannels", ch)
		}
	}
}

func TestDispatcher_InterleavedQuakesFireLimitedOncePerQuake(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})
	steps := []struct {
		ev          eew.Event
		wantLimited bool
	}{
		{eew.Event{EventID: "A"}, true},
		{eew.Event{EventID: "B"}, true},
		{eew.Event{EventID: "A"}, false},
		{eew.Event{EventID: "B"}, false},
		{eew.Event{EventID: "A", Final: true}, true},
		{eew.Event{EventID: "B", Cancelled: true}, true},
	}
	for i, s := range steps {
		channels, _ := d.ChannelsFor(s.ev)
		if got := contains(channels, "#limited"); got != s.wantLimited {
			t.Errorf("step %d (%+v): #limited = %v, want %v", i, s.ev, got, s.wantLimited)
		}
	}
}
