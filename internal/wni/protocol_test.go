package wni

import (
	"bytes"
	"strconv"
	"testing"
)

func dataBlockBytes(body []byte, md5hex string) []byte {
	header := "X-WNI-ID: Data\nContent-Length: " + strconv.Itoa(len(body)) + "\nX-WNI-Data-MD5: " + md5hex + "\n\n"
	return append([]byte(header), body...)
}

func TestFrameParser_DataBlock(t *testing.T) {
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	body := []byte("37 03 00 100430002152 C11\n100430001908\n")
	input := dataBlockBytes(body, "abc123")

	frames, err := p.Feed(input)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	f := frames[0]
	if f.Kind != FrameData {
		t.Fatalf("expected FrameData, got %v", f.Kind)
	}
	if !bytes.Equal(f.Body, body) {
		t.Fatalf("body mismatch: got %q want %q", f.Body, body)
	}
	if f.MD5 != "abc123" {
		t.Fatalf("md5 mismatch: got %q", f.MD5)
	}
}

func TestFrameParser_ResponseAndKeepAlive(t *testing.T) {
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	input := []byte("HTTP/1.0 200 OK\nX-WNI-ID: Response\nX-WNI-Result: OK\n\nX-WNI-ID: Keep-Alive\n\n")
	frames, err := p.Feed(input)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].Kind != FrameResponse {
		t.Fatalf("frame 0: expected FrameResponse, got %v", frames[0].Kind)
	}
	if frames[0].Header["X-WNI-Result"] != "OK" {
		t.Fatalf("frame 0: unexpected header: %+v", frames[0].Header)
	}
	if frames[1].Kind != FrameKeepAlive {
		t.Fatalf("frame 1: expected FrameKeepAlive, got %v", frames[1].Kind)
	}
}

func TestFrameParser_GetPingTriggersAck(t *testing.T) {
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	frames, err := p.Feed([]byte("GET / HTTP/1.1\n"))
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("expected no frames from a bare GET ping, got %d", len(frames))
	}
	if !bytes.Contains(ack.Bytes(), []byte("200 OK")) {
		t.Fatalf("expected an ack containing 200 OK, got %q", ack.String())
	}
	if !bytes.Contains(ack.Bytes(), []byte("X-WNI-ID: Response")) {
		t.Fatalf("expected ack to identify itself as a Response, got %q", ack.String())
	}
}

func TestFrameParser_GetPingMidStreamDoesNotLoseFollowingFrame(t *testing.T) {
	// The GET ping wipes the parser's buffer (matching EEWSock.pm), so a
	// frame sent as part of the *same* Feed call immediately after the ping
	// would be lost — this documents that constraint rather than asserting
	// data survives it; real servers are expected to send the ping as an
	// isolated write. Here we assert behavior when the ping is delivered in
	// its own Feed call and a Data block follows in a separate one, which is
	// the supported usage.
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	if _, err := p.Feed([]byte("GET / HTTP/1.1\n")); err != nil {
		t.Fatalf("Feed(ping): %v", err)
	}

	body := []byte("telegram-body")
	frames, err := p.Feed(dataBlockBytes(body, "deadbeef"))
	if err != nil {
		t.Fatalf("Feed(data): %v", err)
	}
	if len(frames) != 1 || frames[0].Kind != FrameData || !bytes.Equal(frames[0].Body, body) {
		t.Fatalf("expected the Data block to parse cleanly after the ping, got %+v", frames)
	}
}

func TestFrameParser_PartialReadsAtEveryByteOffset(t *testing.T) {
	body := []byte("288 N372 E1351 380 43 // RK662// RT10/// RC/////\n9999=\n")
	full := dataBlockBytes(body, "0123456789abcdef0123456789abcdef")

	for split := 0; split <= len(full); split++ {
		var ack bytes.Buffer
		p := NewFrameParser(&ack)

		frames1, err := p.Feed(full[:split])
		if err != nil {
			t.Fatalf("split=%d Feed(part1): %v", split, err)
		}
		frames2, err := p.Feed(full[split:])
		if err != nil {
			t.Fatalf("split=%d Feed(part2): %v", split, err)
		}
		all := append(frames1, frames2...)
		if len(all) != 1 || all[0].Kind != FrameData || !bytes.Equal(all[0].Body, body) {
			t.Fatalf("split=%d: expected exactly one matching Data frame, got %+v", split, all)
		}
	}
}

func TestFrameParser_MultipleFramesInOneRead(t *testing.T) {
	// Two Data blocks arriving concatenated in a single read. EEWSock.pm's
	// Perl equivalent uses a fragile exact-length comparison here that would
	// stall forever in this scenario (see comment in protocol.go); this
	// test locks in the deliberately more robust Go behavior.
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	body1 := []byte("first-telegram")
	body2 := []byte("second-telegram")
	combined := append(dataBlockBytes(body1, "aaa"), dataBlockBytes(body2, "bbb")...)

	frames, err := p.Feed(combined)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames from a single combined read, got %d", len(frames))
	}
	if !bytes.Equal(frames[0].Body, body1) || !bytes.Equal(frames[1].Body, body2) {
		t.Fatalf("frame bodies mismatch: %+v", frames)
	}
}

func TestFrameParser_DataBodyThenGETPingInSameFeedCall(t *testing.T) {
	// Reproduces the shared trigger shape behind two historical bugs (see
	// wnisim.Session.SendDataThenGETPing's doc comment): a Data block's body
	// arriving in the same batch as trailing bytes right after it — here the
	// GET-ping quirk line, rather than another block's headers (that variant
	// is already covered by TestFrameParser_MultipleFramesInOneRead). This
	// locks in that the already-complete Data frame comes back and the ack
	// still gets written for this exact shape.
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	body := []byte("telegram-body")
	combined := append(dataBlockBytes(body, "cafef00d"), []byte("GET / HTTP/1.1\n")...)

	frames, err := p.Feed(combined)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(frames) != 1 || frames[0].Kind != FrameData || !bytes.Equal(frames[0].Body, body) {
		t.Fatalf("expected the Data block to parse cleanly, got %+v", frames)
	}
	if !bytes.Contains(ack.Bytes(), []byte("200 OK")) {
		t.Fatalf("expected the trailing GET ping to still be acked, got %q", ack.String())
	}
}

func TestFrameParser_IgnoresUnrecognizedStatusLine(t *testing.T) {
	var ack bytes.Buffer
	p := NewFrameParser(&ack)

	// A bare status-line-like line with no colon and non-empty content
	// should be silently skipped, matching EEWSock.pm's fallthrough.
	input := append([]byte("HTTP/1.0 200 OK\n"), []byte("X-WNI-ID: Keep-Alive\n\n")...)
	frames, err := p.Feed(input)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(frames) != 1 || frames[0].Kind != FrameKeepAlive {
		t.Fatalf("expected a single KeepAlive frame, got %+v", frames)
	}
}
