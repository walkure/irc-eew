package eewview

import (
	"strings"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// decodeShiftJIS converts Shift-JIS bytes (as WNI telegrams' free-text
// preamble — sender org name, half-width katakana — uses) to a UTF-8
// string, for the raw-telegram-dump display only. internal/decoder.Decode
// itself is intentionally SJIS-agnostic (its regexes only match ASCII
// digit/letter substrings, verified correct against the full real corpus);
// this is purely a display concern, isolated here.
//
// japanese.ShiftJIS's decoder is lenient (replaces unmappable sequences
// with the Unicode replacement character rather than erroring), so this
// never fails an otherwise-valid page render over one bad byte in an
// archival file.
func decodeShiftJIS(raw []byte) string {
	decoded, _, err := transform.Bytes(japanese.ShiftJIS.NewDecoder(), raw)
	if err != nil {
		return string(raw)
	}
	return string(decoded)
}

// markControlChars replaces the SOH/STX/ETX control bytes WNI telegrams use
// as section markers with visible labels, matching HTML/eew-show.pl's raw
// dump formatting exactly.
func markControlChars(s string) string {
	r := strings.NewReplacer(
		"\x01", "[SOH]\n",
		"\x02", "[STX]\n",
		"\x03", "[ETX]\n",
	)
	return r.Replace(s)
}
