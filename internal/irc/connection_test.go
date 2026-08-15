package irc

import (
	"context"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/decoder"
)

func testServerConfig() config.IRCServerConfig {
	var srv config.IRCServerConfig
	srv.Server.Host = "irc.example.jp"
	srv.Server.Port = 6667
	srv.Nick = "EEWNotice"
	srv.Name = "EEWBot"
	srv.Desc = "Emergency Earthquake Warning"
	srv.AllNotice = []string{"#all"}
	srv.LimitedNotice = []string{"#limited"}
	return srv
}

func TestClientConfig_MapsFieldsAndDisablesFlood(t *testing.T) {
	srv := testServerConfig()
	srv.Server.Password = "secret"
	cfg := clientConfig(srv)

	if cfg.Server != "irc.example.jp:6667" {
		t.Errorf("Server: got %q", cfg.Server)
	}
	if cfg.Pass != "secret" {
		t.Errorf("Pass: got %q", cfg.Pass)
	}
	if !cfg.Flood {
		t.Error("Flood: expected true (no flood control, matching irc-eew.pl)")
	}
	if cfg.Me.Nick != "EEWNotice" {
		t.Errorf("Me.Nick: got %q", cfg.Me.Nick)
	}
	// Perl's "name" field is the IRC ident/username; "desc" is the realname.
	if cfg.Me.Ident != "EEWBot" {
		t.Errorf("Me.Ident: got %q, want the configured Name (EEWBot)", cfg.Me.Ident)
	}
	if cfg.Me.Name != "Emergency Earthquake Warning" {
		t.Errorf("Me.Name: got %q, want the configured Desc", cfg.Me.Name)
	}
}

func TestConnection_NotifyEnqueuesDispatchedNotice(t *testing.T) {
	c := NewConnection(testServerConfig())

	tel := &decoder.Telegram{EqID: "20260809000000", WarnNum: 1}
	text := "震央:<http://maps.google.com/maps?q=1,2|N1/E2>(テスト)深さ10km 最大:M3.0 震度2"
	c.Notify(tel, text)

	n, ok := c.queue.Dequeue()
	if !ok {
		t.Fatal("expected a queued notice")
	}
	if n.EqID != "20260809000000" {
		t.Errorf("EqID: got %q", n.EqID)
	}
	if !contains(n.Channels, "#all") || !contains(n.Channels, "#limited") {
		t.Errorf("Channels: got %v, want both #all and #limited (first report)", n.Channels)
	}
	if n.Text != "震央:N1/E2(テスト)深さ10km 最大:M3.0 震度2" {
		t.Errorf("Text: got %q (link markup should be stripped)", n.Text)
	}
}

func TestConnection_NotifySkipsQueueWhenNoChannelsSelected(t *testing.T) {
	var srv config.IRCServerConfig
	srv.Server.Host = "irc.example.jp"
	srv.Server.Port = 6667
	srv.Nick = "EEWNotice"
	srv.LimitedNotice = []string{"#limited"} // no all-notice configured
	c := NewConnection(srv)

	eqID := "20260809000000"
	c.Notify(&decoder.Telegram{EqID: eqID, WarnNum: 1}, "first report")
	if _, ok := c.queue.Dequeue(); !ok {
		t.Fatal("expected the first report to fire limited and enqueue")
	}

	// A routine follow-up for the same quake fires neither all (unset) nor
	// limited (already fired), so nothing should be queued.
	c.Notify(&decoder.Telegram{EqID: eqID, WarnNum: 2}, "routine report")
	if got := c.queue.Len(); got != 0 {
		t.Errorf("Len: got %d, want 0 (routine report should not enqueue)", got)
	}
}

func TestParseMessageType(t *testing.T) {
	cases := []struct {
		in     string
		want   messageType
		wantOK bool
	}{
		{"", messageTypeNotice, true},
		{"notice", messageTypeNotice, true},
		{"privmsg", messageTypePrivmsg, true},
		{"PRIVMSG", messageTypeNotice, false},
		{"bogus", messageTypeNotice, false},
	}
	for _, c := range cases {
		got, ok := parseMessageType(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("parseMessageType(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestNewConnection_MessageTypeDefaultsToNotice(t *testing.T) {
	c := NewConnection(testServerConfig())
	if c.messageType != messageTypeNotice {
		t.Errorf("messageType: got %v, want notice (default)", c.messageType)
	}
}

func TestNewConnection_MessageTypePrivmsg(t *testing.T) {
	srv := testServerConfig()
	srv.Server.MessageType = "privmsg"
	c := NewConnection(srv)
	if c.messageType != messageTypePrivmsg {
		t.Errorf("messageType: got %v, want privmsg", c.messageType)
	}
}

func TestNewConnection_UnknownMessageTypeFallsBackToNotice(t *testing.T) {
	srv := testServerConfig()
	srv.Server.MessageType = "bogus"
	c := NewConnection(srv)
	if c.messageType != messageTypeNotice {
		t.Errorf("messageType: got %v, want notice (fallback for unrecognized value)", c.messageType)
	}
}

func TestSleepCtx_ReturnsFalseOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, time.Second) {
		t.Error("expected sleepCtx to return false for an already-canceled context")
	}
}

func TestNextBackoff_CapsAtMax(t *testing.T) {
	got := nextBackoff(40*time.Second, 60*time.Second)
	if got != 60*time.Second {
		t.Errorf("got %v, want capped at 60s", got)
	}
	got = nextBackoff(1*time.Second, 60*time.Second)
	if got != 2*time.Second {
		t.Errorf("got %v, want doubled to 2s", got)
	}
}

func TestBackoffAfterDisconnect_GrowsOnShortLivedConnection(t *testing.T) {
	// A connection that dies well before stableConnectionThreshold must
	// grow the backoff, not reset it — otherwise a server that accepts
	// and immediately kills every connection defeats backoff entirely
	// (observed against a real network: a sub-second connect/disconnect
	// cycle that never backed off).
	got := backoffAfterDisconnect(4*time.Second, 500*time.Millisecond)
	if want := 8 * time.Second; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBackoffAfterDisconnect_ResetsOnStableConnection(t *testing.T) {
	got := backoffAfterDisconnect(32*time.Second, stableConnectionThreshold)
	if got != initialBackoff {
		t.Errorf("got %v, want reset to initialBackoff (%v)", got, initialBackoff)
	}

	got = backoffAfterDisconnect(32*time.Second, stableConnectionThreshold+time.Hour)
	if got != initialBackoff {
		t.Errorf("got %v, want reset to initialBackoff (%v)", got, initialBackoff)
	}
}
