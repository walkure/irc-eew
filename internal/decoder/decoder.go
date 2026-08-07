// Package decoder parses the JMA "advanced/high-grade-user" Earthquake
// Early Warning (EEW) telegram format WNI relays over FastCaster, porting
// Earthquake::EEW::Decoder::read_data (lib/Earthquake/EEW/Decoder.pm in
// this repository, the version fetched at Docker build time from
// github.com/walkure/Earthquake_EEW_Decoder) line-for-line.
//
// The lookup tables in tables_gen.go are mechanically generated from that
// same Perl source (see internal/decoder/gen) rather than hand-transcribed.
package decoder

import (
	"regexp"
	"strconv"
	"strings"
)

// Telegram is the decoded form of one EEW wire telegram.
type Telegram struct {
	CodeType, Code       string
	Section, SectionCode string
	MsgType              string
	MsgTypeCode          int    // 10 == cancellation (取り消し)
	WarnTime             string // raw 12-digit YYMMDDhhmmss (report issue time)
	CommandCode          string

	EqTime string // raw 12-digit YYMMDDhhmmss (quake detection time)

	EqID     string // 14-digit YYYYMMDDhhmmss
	WarnType string
	WarnCode string
	NCNType  int // revision/finality digit (0 = normal; >0 = amended/final)
	WarnNum  int

	CenterCode, CenterName string
	CenterLat, CenterLng   float64
	CenterDepth            int
	Magnitude              float64
	Shindo, ShindoCode     string

	CenterAccurateCode    string
	CenterAccurate        string
	DepthAccurateCode     string
	DepthAccurate         string
	MagnitudeAccurateCode string
	MagnitudeAccurate     string
	IsSeaCode             string
	IsSea                 string
	IsWarnded             string

	EBI map[string]EBIEntry
}

// IsFinal reports whether this is an amended or final report, matching
// irc-eew.pl's `$d->{NCN_type} > 0` check (used for the "(最終)" message
// suffix and for the all-vs-limited Slack hook dedup decision).
func (t *Telegram) IsFinal() bool { return t.NCNType > 0 }

// IsCancellation reports whether this telegram cancels a previous warning.
func (t *Telegram) IsCancellation() bool { return t.MsgTypeCode == 10 }

// EBIEntry is one area's forecast-intensity entry from an EBI line.
type EBIEntry struct {
	Name                     string
	Shindo1, Shindo1Code     string
	Shindo2, Shindo2Code     string
	Time                     string
	IsWarnded, IsWarndedCode string
	Arrive, ArriveCode       string
}

var (
	headerRE     = regexp.MustCompile(`(\d{2}) (\d{2}) (\d{2}) (\d{12}) C(\d{2})`)
	eqTimeRE     = regexp.MustCompile(`^(\d{12})`)
	ndRE         = regexp.MustCompile(`ND(\d{14}) ([A-Z]+)(\d+)`)
	hypocenterRE = regexp.MustCompile(`([0-9/]{3}) N([0-9/]{3}) E([0-9/]{4}) ([0-9/]{3}) ([0-9/]{2}) ([0-7\-+/]{2}) RK([0-9/]{5}) RT([0-9/]{5}) RC([0-9/]{5})`)
	ebiRE        = regexp.MustCompile(`(\d{3}) S([0-7\-+]{2})([0-7\-+/]{2}) ([0-9/]{6}) ([01]{2})`)
)

// Decode parses a raw telegram body. It mirrors read_data's line-by-line,
// first-match-wins (Perl elsif chain) dispatch: each line is tried against
// the five patterns in order and at most one applies.
func Decode(raw []byte) *Telegram {
	t := &Telegram{}
	for _, line := range splitLines(string(raw)) {
		switch {
		case tryHeader(t, line):
		case tryEqTime(t, line):
		case tryND(t, line):
		case tryHypocenter(t, line):
		case tryEBI(t, line):
		}
	}
	return t
}

// splitLines mirrors Perl's `split /[\r\n]/, $str`: splits on any CR or LF
// byte individually (not as a combined CRLF unit), matching the character
// class in the original regex.
func splitLines(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '\r' || r == '\n' })
}

func tryHeader(t *Telegram, line string) bool {
	m := headerRE.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	t.Code = m[1]
	t.CodeType = CodeType[m[1]]
	t.SectionCode = m[2]
	t.Section = Section[m[2]]
	t.MsgTypeCode = parseIntOrZero(m[3])
	t.MsgType = MsgType[m[3]]
	t.WarnTime = m[4]
	t.CommandCode = m[5]
	return true
}

func tryEqTime(t *Telegram, line string) bool {
	m := eqTimeRE.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	t.EqTime = m[1]
	return true
}

func tryND(t *Telegram, line string) bool {
	m := ndRE.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	t.EqID = m[1]
	t.WarnCode = m[2]
	t.WarnType = WarnType[m[2]]

	trailing := m[3]
	ncnDigit := trailing[:1]
	t.NCNType = parseIntOrZero(ncnDigit)
	if len(trailing) >= 3 {
		t.WarnNum = parseIntOrZero(trailing[1:3])
	} else if len(trailing) > 1 {
		t.WarnNum = parseIntOrZero(trailing[1:])
	}
	return true
}

func tryHypocenter(t *Telegram, line string) bool {
	m := hypocenterRE.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	t.CenterCode = m[1]
	t.CenterName = EpicenterCode[m[1]]
	t.CenterLat = float64(parseIntOrZero(m[2])) / 10
	t.CenterLng = float64(parseIntOrZero(m[3])) / 10
	t.CenterDepth = parseIntOrZero(m[4])
	t.Magnitude = float64(parseIntOrZero(m[5])) / 10
	t.ShindoCode = m[6]
	t.Shindo = Shindo[m[6]]

	rk := m[7]
	t.CenterAccurateCode = charAt(rk, 0)
	t.CenterAccurate = RK1[t.CenterAccurateCode]
	t.DepthAccurateCode = charAt(rk, 1)
	t.DepthAccurate = RK2[t.DepthAccurateCode]
	t.MagnitudeAccurateCode = charAt(rk, 2)
	t.MagnitudeAccurate = RK3[t.MagnitudeAccurateCode]

	rt := m[8]
	t.IsSeaCode = charAt(rt, 0)
	t.IsSea = RT1[t.IsSeaCode]
	t.IsWarnded = charAt(rt, 1)
	return true
}

func tryEBI(t *Telegram, line string) bool {
	matches := ebiRE.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return false
	}
	if t.EBI == nil {
		t.EBI = map[string]EBIEntry{}
	}
	for _, m := range matches {
		area := m[1]
		t.EBI[area] = EBIEntry{
			Name:          AreaCode[area],
			Shindo1Code:   m[2],
			Shindo1:       Shindo[m[2]],
			Shindo2Code:   m[3],
			Shindo2:       Shindo[m[3]],
			Time:          m[4],
			IsWarndedCode: charAt(m[5], 0),
			IsWarnded:     EBIY1[charAt(m[5], 0)],
			ArriveCode:    charAt(m[5], 1),
			Arrive:        EBIY2[charAt(m[5], 1)],
		}
	}
	return true
}

func charAt(s string, i int) string {
	if i < 0 || i >= len(s) {
		return ""
	}
	return s[i : i+1]
}

// parseIntOrZero mirrors Perl's lenient numeric string coercion (e.g. the
// literal "///" placeholder used in cancelled/unset fields numifies to 0
// with no error under `use warnings` disabled, which Decoder.pm has).
func parseIntOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
