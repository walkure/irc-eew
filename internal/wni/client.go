package wni

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"time"
)

// Credentials mirrors the WNIEEW section of the daemon's YAML config.
type Credentials struct {
	User      string
	Passwd    string
	PasswdMD5 string // if set, used verbatim in place of md5(Passwd)
}

func (c Credentials) passwordHash() string {
	if c.PasswdMD5 != "" {
		return c.PasswdMD5
	}
	sum := md5.Sum([]byte(c.Passwd))
	return hex.EncodeToString(sum[:])
}

// Client is one logged-in (or logging-in) connection to a WNI FastCaster
// server. It owns the raw net.Conn and the FrameParser reading from it.
type Client struct {
	conn   net.Conn
	parser *FrameParser
}

// Dial opens a TCP connection to addr ("host:port") with the same
// keep-alive tuning as EEWSock.pm (SO_KEEPALIVE, ~180s idle, 15s interval).
func Dial(ctx context.Context, addr string) (*Client, error) {
	d := net.Dialer{
		KeepAliveConfig: net.KeepAliveConfig{
			Enable:   true,
			Idle:     180 * time.Second,
			Interval: 15 * time.Second,
		},
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wni: dial %s: %w", addr, err)
	}
	return &Client{conn: conn, parser: NewFrameParser(conn)}, nil
}

// Login sends the pseudo-HTTP "GET /login" request with the X-WNI-* auth
// headers, matching EEWSock::logon exactly (including the hardcoded
// X-WNI-Terminal-ID and X-WNI-Application-Version values the real WNI
// service expects).
func (c *Client) Login(creds Credentials) error {
	req := "GET /login HTTP/1.0\n" +
		"User-Agent: FastCaster/1.0 powered by weathernews.\n" +
		"Accept: */*\n" +
		"Cache-Control: no-cache\n" +
		"X-WNI-Account: " + creds.User + "\n" +
		"X-WNI-Password: " + creds.passwordHash() + "\n" +
		"X-WNI-Application-Version: 2.2.4.0\n" +
		"X-WNI-Authentication-Method: MDB_MWS\n" +
		"X-WNI-ID: Login\n" +
		"X-WNI-Protocol-Version: 2.1\n" +
		"X-WNI-Terminal-ID: 211363088\n" +
		"X-WNI-Time: " + wniTimestamp() + "\n" +
		"\n"
	if _, err := c.conn.Write([]byte(req)); err != nil {
		return fmt.Errorf("wni: sending login request: %w", err)
	}
	return nil
}

// Handlers are invoked synchronously from Run as frames are decoded.
type Handlers struct {
	OnData      func(md5 string, body []byte)
	OnKeepAlive func()
	OnResponse  func(headers map[string]string)
}

// Run reads from the connection until ctx is canceled, idleTimeout elapses
// with no bytes received, or the connection is closed/errors, dispatching
// decoded frames to h as they complete. The caller is responsible for
// reconnecting (Run intentionally does not retry — that policy belongs in
// the orchestration layer, see internal/app).
func (c *Client) Run(ctx context.Context, idleTimeout time.Duration, h Handlers) error {
	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return fmt.Errorf("wni: set read deadline: %w", err)
		}
		n, err := c.conn.Read(buf)
		if err != nil {
			return err
		}
		// Feed can return frames alongside a non-nil error (e.g. the
		// ack-write for the GET-ping quirk fails after a Data frame earlier
		// in the same batch already parsed cleanly) — those frames must
		// still be dispatched before Run returns, or a fully-received
		// telegram is silently lost instead of just triggering a reconnect.
		frames, feedErr := c.parser.Feed(buf[:n])
		for _, f := range frames {
			switch f.Kind {
			case FrameData:
				if h.OnData != nil {
					h.OnData(f.MD5, f.Body)
				}
			case FrameKeepAlive:
				if h.OnKeepAlive != nil {
					h.OnKeepAlive()
				}
			case FrameResponse:
				if h.OnResponse != nil {
					h.OnResponse(f.Header)
				}
			}
		}
		if feedErr != nil {
			return feedErr
		}
	}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
