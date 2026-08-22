// Command wnisim runs a fake WNI FastCaster server for manually verifying
// protocol compatibility. It is a development/verification tool, not part
// of the production notifier.
//
// Typical use: point a patched copy of the (10-years-in-production) Perl
// irc-eew.pl at this server's address instead of the real WNI server list,
// feed it a directory of real captured telegrams (e.g. from eewlog/), and
// confirm the unmodified Perl decode/notify pipeline behaves as expected —
// this validates that wnisim's protocol implementation is faithful to the
// real WNI server before the Go client (internal/wni) is tested against it.
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/walkure/irc-eew/internal/wnisim"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	listen := flag.String("listen", ":19000", "address to listen on")
	dir := flag.String("telegrams-dir", "", "directory of raw telegram files to replay after login (sorted by filename)")
	interval := flag.Duration("interval", 3*time.Second, "delay between telegrams")
	keepAlive := flag.Duration("keepalive-interval", 30*time.Second, "interval between Keep-Alive blocks (0 disables)")
	getPingEvery := flag.Int("get-ping-every", 1, "send the GET / HTTP/1.1 ack quirk before every Nth telegram (0 disables)")
	getPingMode := flag.String("get-ping-mode", "before",
		`when to send the GET / HTTP/1.1 ack quirk relative to a Data block: `+
			`"before" sends it as an isolated write and awaits the ack before sending Data `+
			`(default, matches production); "coalesced" sends the Data block and the ping `+
			`together in a single write (reproduces the historical bug-trigger shape, see `+
			`wnisim.Session.SendDataThenGETPing)`)
	once := flag.Bool("once", false, "exit after handling a single connection")
	flag.Parse()

	if *dir == "" {
		slog.Error("-telegrams-dir is required")
		os.Exit(1)
	}
	if *getPingMode != "before" && *getPingMode != "coalesced" {
		slog.Error("invalid -get-ping-mode", "value", *getPingMode)
		os.Exit(1)
	}

	files, err := telegramFiles(*dir)
	if err != nil {
		slog.Error("listing telegrams", "dir", *dir, "error", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		slog.Error("no files found", "dir", *dir)
		os.Exit(1)
	}
	slog.Info("loaded telegram files", "count", len(files), "dir", *dir)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		slog.Error("listen", "addr", *listen, "error", err)
		os.Exit(1)
	}
	slog.Info("wnisim listening", "addr", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Error("accept", "error", err)
			os.Exit(1)
		}
		slog.Info("connection accepted", "remote_addr", conn.RemoteAddr())
		handleConn(conn, files, *interval, *keepAlive, *getPingEvery, *getPingMode)
		if *once {
			return
		}
	}
}

func telegramFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func handleConn(conn net.Conn, files []string, interval, keepAlive time.Duration, getPingEvery int, getPingMode string) {
	defer conn.Close()
	sess := wnisim.Accept(conn)

	headers, err := sess.AwaitLogin(10 * time.Second)
	if err != nil {
		slog.Error("login failed", "error", err)
		return
	}
	slog.Info("login received",
		"account", headers["X-WNI-Account"], "id", headers["X-WNI-ID"], "protocol", headers["X-WNI-Protocol-Version"])

	if err := sess.SendResponseOK(); err != nil {
		slog.Error("send response", "error", err)
		return
	}
	slog.Info("sent login Response OK")

	stopKeepAlive := make(chan struct{})
	if keepAlive > 0 {
		go func() {
			t := time.NewTicker(keepAlive)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if err := sess.SendKeepAlive(); err != nil {
						return
					}
					slog.Info("sent Keep-Alive")
				case <-stopKeepAlive:
					return
				}
			}
		}()
		defer close(stopKeepAlive)
	}

	for i, path := range files {
		sendPing := getPingEvery > 0 && i%getPingEvery == 0

		body, err := os.ReadFile(path)
		if err != nil {
			slog.Error("read telegram file", "path", path, "error", err)
			continue
		}
		if len(body) == 0 {
			slog.Warn("skipping empty file", "path", path)
			continue
		}

		if sendPing && getPingMode == "coalesced" {
			if err := sess.SendDataThenGETPing(body); err != nil {
				slog.Error("send coalesced data+GET ping", "error", err)
				return
			}
			slog.Info("sent Data block coalesced with a GET / HTTP/1.1 ping in one write",
				"index", i+1, "total", len(files), "file", filepath.Base(path), "bytes", len(body))
			if sess.AwaitAck(5 * time.Second) {
				slog.Info("client acked the coalesced GET ping")
			} else {
				slog.Warn("no ack received for coalesced GET ping within timeout")
			}
			time.Sleep(interval)
			continue
		}

		if sendPing {
			if err := sess.SendGETPing(); err != nil {
				slog.Error("send GET ping", "error", err)
				return
			}
			slog.Info("sent GET / HTTP/1.1 ping, waiting for ack")
			if sess.AwaitAck(5 * time.Second) {
				slog.Info("client acked the GET ping")
			} else {
				slog.Warn("no ack received for GET ping within timeout")
			}
			time.Sleep(200 * time.Millisecond)
		}

		if err := sess.SendData(body); err != nil {
			slog.Error("send data", "error", err)
			return
		}
		slog.Info("sent Data block", "index", i+1, "total", len(files), "file", filepath.Base(path), "bytes", len(body))

		time.Sleep(interval)
	}

	slog.Info("all telegrams sent; idling (Ctrl+C to stop, or disconnect to test client reconnect)")
	select {}
}
