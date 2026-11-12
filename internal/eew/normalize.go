package eew

import "strings"

// JMA XML text has been seen carrying stray line breaks inside element
// values (e.g. a newline after 千島列島 in ReduceName during the 2026-10-16
// Kamchatka/Kuril event that broke docomo's area mail). Every value is
// therefore normalized before use; the raw bytes are never touched (they
// are archived verbatim elsewhere).

// whitespace is the set of "blank" characters normalization deals with:
// ASCII space, tab, CR, LF and the full-width (ideographic) space.
const whitespace = " \t\r\n　"

// Key normalizes a value used for a decision (comparison, enum lookup,
// number parsing) by deleting every whitespace character, wherever it is.
// Such values never legitimately contain any. No other transformation
// (e.g. NFKC) is applied, so unexpected characters stay visible as
// unrecognized values instead of being coerced into plausible ones.
func Key(s string) string {
	if !strings.ContainsAny(s, whitespace) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !strings.ContainsRune(whitespace, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Display normalizes a value shown to people (epicenter name, free text):
// leading/trailing whitespace is trimmed and each run of CR/LF inside is
// replaced by one ASCII space. Other inner spacing is kept, since it can
// separate meaningful parts of free text.
func Display(s string) string {
	s = strings.Trim(s, whitespace)
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inBreak := false
	for _, r := range s {
		if r == '\r' || r == '\n' {
			if !inBreak {
				b.WriteByte(' ')
				inBreak = true
			}
			continue
		}
		inBreak = false
		b.WriteRune(r)
	}
	return b.String()
}

// StripLineBreaks removes every CR and LF from s. It is the last line of
// defense applied to a finished notification text: IRC cannot carry a line
// break inside a message (goirc truncates at the first one), and a stray
// break would silently drop the rest of the notice.
func StripLineBreaks(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}
