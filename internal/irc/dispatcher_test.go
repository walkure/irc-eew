package irc

import (
	"testing"

	"github.com/walkure/irc-eew/internal/decoder"
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

	channels, priority := d.ChannelsFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
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
	d.ChannelsFor(&decoder.Telegram{EqID: eqID, WarnNum: 1}) // establishes lastEqID

	channels, _ := d.ChannelsFor(&decoder.Telegram{EqID: eqID, WarnNum: 2})
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
	d.ChannelsFor(&decoder.Telegram{EqID: eqID, WarnNum: 1})

	channels, priority := d.ChannelsFor(&decoder.Telegram{EqID: eqID, WarnNum: 42, NCNType: 9})
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
	d.ChannelsFor(&decoder.Telegram{EqID: eqID, WarnNum: 1})

	channels, priority := d.ChannelsFor(&decoder.Telegram{EqID: eqID, WarnNum: 2, MsgTypeCode: 10})
	if !contains(channels, "#limited") {
		t.Errorf("expected #limited to fire for a cancellation, got %v", channels)
	}
	if priority != PriorityCancellation {
		t.Errorf("priority: got %v, want PriorityCancellation", priority)
	}
}

func TestDispatcher_NewQuakeAfterAnotherAlwaysFiresLimited(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, []string{"#limited"})
	d.ChannelsFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
	d.ChannelsFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 2}) // routine

	channels, _ := d.ChannelsFor(&decoder.Telegram{EqID: "20260807010000", WarnNum: 1}) // different quake
	if !contains(channels, "#limited") {
		t.Errorf("expected #limited to fire for a genuinely new quake, got %v", channels)
	}
}

func TestDispatcher_EmptyLimitedList(t *testing.T) {
	d := NewDispatcher([]string{"#all"}, nil)

	channels, _ := d.ChannelsFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
	if len(channels) != 1 || channels[0] != "#all" {
		t.Errorf("expected only #all when no limited channels are configured, got %v", channels)
	}
}

func TestDispatcher_ChannelInBothListsIsNotDuplicated(t *testing.T) {
	d := NewDispatcher([]string{"#eew", "#all"}, []string{"#eew", "#limited"})

	channels, _ := d.ChannelsFor(&decoder.Telegram{EqID: "20260807000000", WarnNum: 1})
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
