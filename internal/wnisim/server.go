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

// SendData sends a Data block: headers (with Content-Length and an
// X-WNI-Data-MD5 computed the same way the real server does, over the raw
// body bytes) followed immediately by the raw body.
func (s *Session) SendData(body []byte) error {
	sum := md5.Sum(body)
	header := fmt.Sprintf(
		"X-WNI-ID: Data\nContent-Length: %d\nX-WNI-Data-MD5: %s\n\n",
		len(body), hex.EncodeToString(sum[:]),
	)
	return s.writeRaw(append([]byte(header), body...))
}

// Close closes the underlying connection.
func (s *Session) Close() error {
	return s.conn.Close()
}
