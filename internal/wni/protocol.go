// Package wni implements the client side of Weathernews (WNI) "FastCaster"
// EEW push protocol used by irc-eew.pl / EEWSock.pm: a bespoke pseudo-HTTP
// framing over a plain TCP connection (not real HTTP — no status line
// semantics, and the server occasionally sends a bare "GET / HTTP/1.1" line
// back to the client that must be acknowledged, see FrameParser).
package wni

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// FrameKind identifies which kind of block a Frame represents, mirroring
// the X-WNI-ID header values EEWSock.pm switches on.
type FrameKind int

const (
	FrameResponse FrameKind = iota
	FrameKeepAlive
	FrameData
	FrameUnknown
)

// Frame is one fully-parsed protocol block.
type Frame struct {
	Kind   FrameKind
	ID     string // raw X-WNI-ID value; populated for FrameUnknown, informational otherwise
	Header map[string]string
	MD5    string // X-WNI-Data-MD5, only meaningful for FrameData
	Body   []byte // only meaningful for FrameData
}

// FrameParser is a pure, I/O-free incremental parser for the WNI framing:
// line-oriented "Header: value" blocks terminated by a blank line, where a
// block with X-WNI-ID: Data switches to byte-counting mode (Content-Length
// raw bytes, not further line-delimited) before the next block can start.
//
// Feed may be called with arbitrarily-sized chunks (including chunks that
// split a header line or a Data body at any byte offset) — this is the
// reason it's a state machine rather than a line reader, matching the fact
// that EEWSock::parse_body has to cope with the same thing over a raw
// sysread() loop.
//
// When a line matching "GET / HTTP/1.1" arrives (the server-initiated ack
// quirk described in the package doc), FrameParser writes a synthesized ack
// to ackWriter immediately, matching EEWSock.pm's send_ack.
type FrameParser struct {
	ackWriter io.Writer

	buf     []byte
	header  map[string]string
	size    int
	hasSize bool
}

// NewFrameParser creates a FrameParser that writes protocol acks to w
// (typically the same net.Conn the frames are being read from).
func NewFrameParser(w io.Writer) *FrameParser {
	return &FrameParser{ackWriter: w, header: map[string]string{}}
}

// Feed appends newly-read bytes and returns any frames that became complete
// as a result. It never blocks and never reads from a connection itself.
func (p *FrameParser) Feed(data []byte) ([]Frame, error) {
	p.buf = append(p.buf, data...)

	var frames []Frame
	for {
		if !p.hasSize {
			idx := bytes.IndexByte(p.buf, '\n')
			if idx < 0 {
				break
			}
			line := bytes.TrimRight(p.buf[:idx], "\r")
			p.buf = p.buf[idx+1:]

			if bytes.Contains(line, []byte("GET / HTTP/1.1")) {
				if err := p.sendAck(); err != nil {
					return frames, fmt.Errorf("wni: sending ack: %w", err)
				}
				// Mirrors EEWSock::parse_body, which wipes its entire
				// receive buffer on this line — the real server appears to
				// always send this ping as its own isolated write, so
				// nothing meaningful is ever lost in practice.
				p.header = map[string]string{}
				p.buf = nil
				continue
			}

			if colon := bytes.IndexByte(line, ':'); colon >= 0 {
				name := strings.TrimSpace(string(line[:colon]))
				val := strings.TrimSpace(string(line[colon+1:]))
				p.header[name] = val
				continue
			}

			if len(line) == 0 {
				id := p.header["X-WNI-ID"]
				switch id {
				case "Data":
					if cl, ok := p.header["Content-Length"]; ok {
						if n, err := strconv.Atoi(strings.TrimSpace(cl)); err == nil {
							p.size = n
							p.hasSize = true
						}
					}
				case "Response":
					frames = append(frames, Frame{Kind: FrameResponse, Header: cloneHeader(p.header)})
				case "Keep-Alive":
					frames = append(frames, Frame{Kind: FrameKeepAlive})
				default:
					frames = append(frames, Frame{Kind: FrameUnknown, ID: id, Header: cloneHeader(p.header)})
				}
				if !p.hasSize {
					p.header = map[string]string{}
				}
				continue
			}

			// A non-blank, non-header, non-GET-quirk line (e.g. a leading
			// "HTTP/1.0 200 OK" status line) — EEWSock.pm silently ignores
			// these too, so just move on to the next line.
			continue
		}

		// Body-accumulation mode. NOTE: EEWSock::parse_body requires the
		// buffer length to be *exactly* Content-Length before it dispatches
		// the callback, which means it can never make progress if a single
		// read happens to bundle a Data body together with any trailing
		// bytes (e.g. the next block's headers). Here we deliberately use
		// ">=" and slice off exactly `size` bytes, leaving any remainder to
		// be parsed as the start of the next frame — a strictly more robust
		// framing behavior, not a protocol change.
		if len(p.buf) < p.size {
			break
		}
		body := append([]byte(nil), p.buf[:p.size]...)
		p.buf = p.buf[p.size:]
		frames = append(frames, Frame{
			Kind:   FrameData,
			Header: cloneHeader(p.header),
			MD5:    p.header["X-WNI-Data-MD5"],
			Body:   body,
		})
		p.hasSize = false
		p.header = map[string]string{}
	}

	return frames, nil
}

func (p *FrameParser) sendAck() error {
	now := wniTimestamp()
	msg := "HTTP/1.0 200 OK\n" +
		"Content-Type: application/fast-cast\n" +
		"Server: FastCaster/1.0.0 (Unix)\n" +
		"X-WNI-ID: Response\n" +
		"X-WNI-Result: OK\n" +
		"X-WNI-Protocol-Version: 2.1\n" +
		"X-WNI-Time: " + now + "\n" +
		"\n"
	_, err := p.ackWriter.Write([]byte(msg))
	return err
}

func cloneHeader(h map[string]string) map[string]string {
	c := make(map[string]string, len(h))
	for k, v := range h {
		c[k] = v
	}
	return c
}

// wniTimestamp matches EEWSock.pm's wni_timestamp: "Y/m/d H:M:S.microseconds".
func wniTimestamp() string {
	now := time.Now()
	return now.Format("2006/01/02 15:04:05") + fmt.Sprintf(".%06d", now.Nanosecond()/1000)
}
