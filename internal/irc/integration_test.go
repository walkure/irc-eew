//go:build irctest

// Package irc_test's integration tests connect internal/irc.Connection
// (production code, goirc included) to a real IRC daemon in a Docker
// container via testcontainers-go, verifying end-to-end wiring that a unit
// test can't reach — most importantly, that a real server doesn't mangle
// non-UTF-8 (ISO-2022-JP) message bytes, since goirc's own wire-framing
// already has no protocol-conformance risk left to test (see the package's
// design notes). Excluded from the default `go test ./...` via this build
// tag; run explicitly with `-tags irctest`.
package irc_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/walkure/irc-eew/internal/config"
	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/irc"
)

// startErgo starts a real IRC daemon (ergochat/ergo — actively maintained,
// plain Docker image, no custom config needed for these tests) for
// internal/irc.Connection to connect to.
func startErgo(t *testing.T) (host, port string) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/ergochat/ergo:stable",
		ExposedPorts: []string{"6667/tcp"},
		WaitingFor:   wait.ForLog("Server running").WithStartupTimeout(30 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting ergo container: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Terminate(context.Background())
	})

	h, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	mapped, err := c.MappedPort(ctx, "6667/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	return h, mapped.Port()
}

// observer is a minimal, independent (no goirc) raw-socket IRC client used
// only to verify what internal/irc.Connection actually put on the wire.
// Deliberately not reusing goirc here, so a shared blind spot between our
// charset encoder and goirc's send path can't hide a bug — see the design
// notes on why an independent implementation matters for this check.
type observer struct {
	conn net.Conn
	r    *bufio.Reader
}

func connectObserver(t *testing.T, addr, channel string) *observer {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("observer dial: %v", err)
	}
	o := &observer{conn: conn, r: bufio.NewReader(conn)}
	fmt.Fprintf(conn, "NICK observer\r\n")
	fmt.Fprintf(conn, "USER observer 0 * :observer\r\n")
	o.readUntil(t, " 001 ")
	fmt.Fprintf(conn, "JOIN %s\r\n", channel)
	o.readUntil(t, " 366 ")
	return o
}

// readUntil reads lines until one contains substr, and returns it.
func (o *observer) readUntil(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_ = o.conn.SetReadDeadline(time.Now().Add(time.Second))
		line, err := o.r.ReadString('\n')
		if err != nil {
			continue
		}
		if strings.Contains(line, substr) {
			return line
		}
	}
	t.Fatalf("timed out waiting for a line containing %q", substr)
	return ""
}

func (o *observer) close() {
	_ = o.conn.Close()
}

// waitJoined blocks until the observer sees nick JOIN channel, confirming
// our bot has actually joined (server-side) before we ask it to notify.
func waitJoined(t *testing.T, o *observer, channel, nick string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_ = o.conn.SetReadDeadline(time.Now().Add(time.Second))
		line, err := o.r.ReadString('\n')
		if err != nil {
			continue
		}
		if strings.Contains(line, nick+"!") && strings.Contains(line, "JOIN") && strings.Contains(line, channel) {
			return
		}
	}
	t.Fatalf("timed out waiting for %s to JOIN %s", nick, channel)
}

// noticeText extracts the trailing text of a "NOTICE <channel> :<text>"
// line, as raw bytes. A Go string can hold arbitrary bytes; only rune-aware
// operations (none used here) would corrupt a non-UTF-8 payload.
func noticeText(t *testing.T, line, channel string) string {
	t.Helper()
	marker := "NOTICE " + channel + " :"
	i := strings.Index(line, marker)
	if i < 0 {
		t.Fatalf("line %q does not contain %q", line, marker)
	}
	return strings.TrimRight(line[i+len(marker):], "\r\n")
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("parsing port %q: %v", s, err)
	}
	return n
}

func runConnection(t *testing.T, srv config.IRCServerConfig) *irc.Connection {
	t.Helper()
	conn := irc.NewConnection(srv)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		conn.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return conn
}

func TestConnection_NotifiesRealServer_UTF8Default(t *testing.T) {
	host, port := startErgo(t)

	const channel = "#eew-utf8"
	obs := connectObserver(t, net.JoinHostPort(host, port), channel)
	defer obs.close()

	var srv config.IRCServerConfig
	srv.Server.Host = host
	srv.Server.Port = mustAtoi(t, port)
	srv.Nick = "eewbot-u"
	srv.AllNotice = []string{channel}
	// Charset left unset: defaults to UTF-8.

	conn := runConnection(t, srv)
	waitJoined(t, obs, channel, "eewbot-u")

	const wantText = "緊急地震速報 テスト #test-チャンネル"
	conn.Notify(&decoder.Telegram{EqID: "20260809000000", WarnNum: 1}, wantText)

	line := obs.readUntil(t, "NOTICE "+channel)
	if got := noticeText(t, line, channel); got != wantText {
		t.Errorf("got %q, want %q", got, wantText)
	}
}

func TestConnection_NotifiesRealServer_ISO2022JP(t *testing.T) {
	host, port := startErgo(t)

	const channel = "#eew-jis"
	obs := connectObserver(t, net.JoinHostPort(host, port), channel)
	defer obs.close()

	var srv config.IRCServerConfig
	srv.Server.Host = host
	srv.Server.Port = mustAtoi(t, port)
	srv.Server.Charset = "iso-2022-jp"
	srv.Nick = "eewbot-j"
	srv.AllNotice = []string{channel}

	conn := runConnection(t, srv)
	waitJoined(t, obs, channel, "eewbot-j")

	const wantText = "緊急地震速報 テスト 震度3"
	conn.Notify(&decoder.Telegram{EqID: "20260809000001", WarnNum: 1}, wantText)

	line := obs.readUntil(t, "NOTICE "+channel)
	raw := noticeText(t, line, channel)

	// ISO-2022-JP is a 7-bit encoding: a real server relaying this to a
	// plain (non-websocket) client, as here, must never set the high bit.
	for i := 0; i < len(raw); i++ {
		if raw[i] >= 0x80 {
			t.Fatalf("byte %d of the received notice is 8-bit: %#x (raw=%q)", i, raw[i], raw)
		}
	}

	// Decode independently of internal/irc's own EncodeText (which also
	// uses x/text, but this exercises the actual bytes that crossed the
	// wire through a real server, not just our own encoder's output).
	decoded, _, err := transform.String(japanese.ISO2022JP.NewDecoder(), raw)
	if err != nil {
		t.Fatalf("decoding received bytes as ISO-2022-JP: %v", err)
	}
	if decoded != wantText {
		t.Errorf("got %q (decoded from %q), want %q", decoded, raw, wantText)
	}
}
