package irc

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func TestEncodeText_UTF8Passthrough(t *testing.T) {
	in := "hello 地震 world"
	for _, charset := range []string{"", "utf-8", "UTF-8"} {
		got, err := EncodeText(charset, in)
		if err != nil {
			t.Fatalf("charset=%q: %v", charset, err)
		}
		if got != in {
			t.Errorf("charset=%q: got %q, want %q", charset, got, in)
		}
	}
}

func TestEncodeText_ISO2022JP(t *testing.T) {
	in := "緊急地震速報 #test-チャンネル"
	got, err := EncodeText("iso-2022-jp", in)
	if err != nil {
		t.Fatalf("EncodeText: %v", err)
	}

	// ISO-2022-JP is a 7-bit encoding: no byte should ever have the high
	// bit set. This matters beyond correctness — see internal/irc's design
	// notes on why a goirc channel-name casemap could corrupt 8-bit bytes.
	for i := 0; i < len(got); i++ {
		if got[i] >= 0x80 {
			t.Fatalf("byte %d of encoded text is 8-bit: %#x", i, got[i])
		}
	}

	// The non-ASCII portion should have switched into the JIS X 0208 charset.
	if !strings.Contains(got, "\x1b$B") {
		t.Errorf("expected a JIS X 0208 designator escape sequence, got %q", got)
	}

	decoded, _, err := transform.String(japanese.ISO2022JP.NewDecoder(), got)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != in {
		t.Errorf("round-trip: got %q, want %q", decoded, in)
	}
}

func TestEncodeText_ISO2022JP_ASCIIOnly(t *testing.T) {
	got, err := EncodeText("iso-2022-jp", "#eew-notice")
	if err != nil {
		t.Fatalf("EncodeText: %v", err)
	}
	if got != "#eew-notice" {
		t.Errorf("pure-ASCII input should pass through unchanged, got %q", got)
	}
}

func TestEncodeText_UnknownCharset(t *testing.T) {
	if _, err := EncodeText("shift_jis", "x"); err == nil {
		t.Fatal("expected an error for an unrecognized charset name")
	}
}
