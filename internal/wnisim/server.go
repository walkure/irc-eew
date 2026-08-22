// Package wnisim implements a fake WNI "FastCaster" server for testing
// clients of the proprietary EEW push protocol against, without needing
// access to the real (paid, no-sandbox) WNI production servers.
//
// The protocol was reverse-engineered from EEWSock.pm in this repository:
// the client sends a pseudo-HTTP "GET /login" request with X-WNI-* headers,
// the server replies with header blocks terminated by a blank line (each
// tagged with an X-WNI-ID of Response/Keep-Alive/Data), and Data blocks are
// followed by exactly Content-Length raw bytes (not further line-delimited).
// Oddly, the server sometimes sends a bare "GET / HTTP/1.1" line back to the
// client mid-session, which the client must acknowledge with a synthesized
// "HTTP/1.0 200 OK" response or the server stops pushing data.
package wnisim

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Session represents one accepted client connection and lets a test (or the
// manual verification CLI) drive the server side of the handshake.
type Session struct {
	conn  net.Conn
	lines chan string
	errs  chan error
}

// Accept wraps an already-accepted net.Conn and starts a background reader
// that splits incoming bytes into lines for AwaitLogin/AwaitAck to consume.
func Accept(conn net.Conn) *Session {
	s := &Session{
		conn:  conn,
		lines: make(chan string, 64),
		errs:  make(chan error, 1),
	}
	go s.readLoop()
	return s
}

func (s *Session) readLoop() {
	r := bufio.NewReader(s.conn)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			s.lines <- strings.TrimRight(line, "\r\n")
		}
		if err != nil {
			s.errs <- err
			close(s.lines)
			return
		}
	}
}

// AwaitLogin reads header lines until a blank line (end of the client's
// "GET /login" request) and returns the parsed X-WNI-* headers.
func (s *Session) AwaitLogin(timeout time.Duration) (map[string]string, error) {
	headers := map[string]string{}
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return headers, io.EOF
			}
			if line == "" {
				return headers, nil
			}
			if idx := strings.Index(line, ":"); idx >= 0 {
				name := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				headers[name] = val
			}
		case <-deadline:
			return headers, fmt.Errorf("timeout waiting for login request")
		case err := <-s.errs:
			return headers, err
		}
	}
}

// AwaitAck waits up to timeout for the client to send an acknowledgement
// (its response to a GET / HTTP/1.1 ping) — recognized by a "200 OK" line.
// Returns false (not an error) on plain timeout, since not every caller
// necessarily sent a ping that requires one.
func (s *Session) AwaitAck(timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return false
			}
			if strings.Contains(line, "200 OK") {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func (s *Session) writeRaw(b []byte) error {
	_, err := s.conn.Write(b)
	return err
}

// SendResponseOK sends the login-acknowledgement block.
func (s *Session) SendResponseOK() error {
	msg := "HTTP/1.0 200 OK\nX-WNI-ID: Response\nX-WNI-Result: OK\nX-WNI-Protocol-Version: 2.1\n\n"
	return s.writeRaw([]byte(msg))
}

// SendKeepAlive sends a Keep-Alive block (no body).
func (s *Session) SendKeepAlive() error {
	return s.writeRaw([]byte("X-WNI-ID: Keep-Alive\n\n"))
}

// SendGETPing sends the server-initiated "GET / HTTP/1.1" quirk line that a
// correct client must acknowledge (see AwaitAck). Sent as its own write so
// it can't get coalesced with a subsequent Data block in the same read on
// the client side (EEWSock.pm wipes its whole receive buffer on this line).
func (s *Session) SendGETPing() error {
	return s.writeRaw([]byte("GET / HTTP/1.1\n"))
}

// dataBlockHeader builds a Data block's headers (with Content-Length and an
// X-WNI-Data-MD5 computed the same way the real server does, over the raw
// body bytes), terminated by the blank line that precedes the body.
func dataBlockHeader(body []byte) []byte {
	sum := md5.Sum(body)
	return []byte(fmt.Sprintf(
		"X-WNI-ID: Data\nContent-Length: %d\nX-WNI-Data-MD5: %s\n\n",
		len(body), hex.EncodeToString(sum[:]),
	))
}

// SendData sends a Data block: headers followed immediately by the raw body,
// in a single write.
func (s *Session) SendData(body []byte) error {
	return s.writeRaw(append(dataBlockHeader(body), body...))
}

// SendDataThenGETPing sends a Data block's header+body immediately followed,
// in a single conn.Write, by the "GET / HTTP/1.1" ack-quirk line — the exact
// wire shape behind two historical bugs that shared one root cause: a Data
// block's body arriving in the same read/write batch as trailing bytes right
// after it.
//
// In EEWSock.pm (the original Perl client, since removed from this repo)
// that shape could stall parse_body forever: it required the receive buffer
// to be *exactly* Content-Length bytes before firing its data callback, and
// a trailing GET-ping bundled into the same recv() meant that exact match
// could never be reached again for that buffer state.
//
// In the Go client it instead exposed a narrower bug in Client.Run: frames
// FrameParser.Feed had already finished parsing before hitting the
// GET-ping's ack-write failure were discarded along with the error, instead
// of being dispatched first (see internal/wni/client_internal_test.go).
//
// This is the deliberate inverse of SendGETPing, whose own doc comment notes
// it is sent as an isolated write specifically so it can't coalesce with a
// following Data block; SendDataThenGETPing exists so tests (and, via
// cmd/wnisim's -get-ping-mode flag, manual verification against a real
// client) can reproduce that coalescing on purpose.
func (s *Session) SendDataThenGETPing(body []byte) error {
	combined := append(dataBlockHeader(body), body...)
	combined = append(combined, []byte("GET / HTTP/1.1\n")...)
	return s.writeRaw(combined)
}

// Close closes the underlying connection.
func (s *Session) Close() error {
	return s.conn.Close()
}
