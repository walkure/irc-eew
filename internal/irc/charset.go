// Package irc notifies IRC channels of decoded EEW telegrams, using
// github.com/fluffle/goirc/client as the underlying protocol implementation.
package irc

import (
	"fmt"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// EncodeText converts s (a UTF-8 Go string) into the wire bytes for the
// given charset name, returned as a Go string. goirc writes Notice/Privmsg/
// Join string bytes to the socket verbatim with no rune-based processing, so
// the returned string's bytes are exactly what goes on the wire. An empty
// charset or "utf-8" is a no-op passthrough. The same conversion applies to
// both message bodies and channel names.
func EncodeText(charset, s string) (string, error) {
	enc, ok := charsetEncoder(charset)
	if !ok {
		return "", fmt.Errorf("irc: unknown charset %q", charset)
	}
	if enc == nil {
		return s, nil
	}
	out, _, err := transform.String(enc, s)
	if err != nil {
		return "", fmt.Errorf("irc: encoding %q to charset %q: %w", s, charset, err)
	}
	return out, nil
}

// charsetEncoder resolves a config charset name to a transformer (nil means
// a no-op passthrough). ok is false for an unrecognized name — an unset
// Charset in config.IRCServerConfig defaults to "utf-8" (the empty-string
// case here), unlike IRCSock.pm's implicit ISO-2022-JP fallback.
func charsetEncoder(charset string) (transform.Transformer, bool) {
	switch charset {
	case "", "utf-8", "UTF-8":
		return nil, true
	case "iso-2022-jp", "ISO-2022-JP":
		return japanese.ISO2022JP.NewEncoder(), true
	default:
		return nil, false
	}
}
