package eewview

import (
	"fmt"

	"github.com/walkure/irc-eew/internal/decoder"
)

// splitPairs splits a digit string into consecutive 2-character chunks,
// matching Perl's `$s =~ /\d\d/og` in list context (see also
// internal/eewmsg.splitPairs, duplicated here rather than exported since
// it's a tiny, package-private formatting detail).
func splitPairs(s string) []string {
	var out []string
	for i := 0; i+2 <= len(s); i += 2 {
		out = append(out, s[i:i+2])
	}
	return out
}

func at(parts []string, i int) string {
	if i < 0 || i >= len(parts) {
		return ""
	}
	return parts[i]
}

// timeOnly renders a raw 12-digit YYMMDDhhmmss digit string's time portion
// as "HH:MM:SS".
func timeOnly(raw string) string {
	p := splitPairs(raw)
	return fmt.Sprintf("%s:%s:%s", at(p, 3), at(p, 4), at(p, 5))
}

// fullDateTime renders "20YY/MM/DD HH:MM:SS". Deliberately computed
// independently per call (never sharing state across a WarnTime/EqTime
// pair) so the two dates can never cross-contaminate — this is the exact
// bug class found and fixed in both irc-eew.pl's notifier
// (internal/eewmsg.Format) and this viewer's own original eew-show.pl
// (which built eq_time's displayed date using the warn-report day).
func fullDateTime(raw string) string {
	p := splitPairs(raw)
	return fmt.Sprintf("20%s/%s/%s %s:%s:%s", at(p, 0), at(p, 1), at(p, 2), at(p, 3), at(p, 4), at(p, 5))
}

// isFinalForViewer reports whether t is a "最終" (final) report using this
// viewer's own threshold (NCN_type >= 8, matching HTML/index.pl and
// HTML/eew-show.pl exactly) — deliberately distinct from
// decoder.Telegram.IsFinal() (NCNType > 0, matching the notifier's
// irc-eew.pl original). Per the NCN_type code table, only 8/9 mean
// "final/last report"; 6/7 mean "correction". The two original Perl
// components already disagreed on this threshold; unifying them here would
// silently change what "(最終)" has always meant on this archive viewer.
func isFinalForViewer(t *decoder.Telegram) bool {
	return t.NCNType >= 8
}

// ListSummaryText renders the one-line summary shown next to each file in
// the list view, matching HTML/index.pl's get_eew_summary. Unlike the
// detail view's summary, this omits the year (redundant with the
// surrounding day breadcrumb) and zero-pads the report number.
func ListSummaryText(t *decoder.Telegram) string {
	timesSuffix := "　　　　" // visual padding to roughly match "(最終)"'s width when not final
	if isFinalForViewer(t) {
		timesSuffix = "(最終)"
	}
	times := fmt.Sprintf("第%02d報%s", t.WarnNum, timesSuffix)

	var msg string
	if t.IsCancellation() {
		msg = fmt.Sprintf("%s%s (%s発生) 取り消し", timeOnly(t.WarnTime), times, timeOnly(t.EqTime))
	} else {
		center := fmt.Sprintf("震央:N%.1f/E%.1f(%s)深さ%dkm", t.CenterLat, t.CenterLng, t.CenterName, t.CenterDepth)
		magnitude := fmt.Sprintf(" 最大:M%.1f 震度%s", t.Magnitude, t.Shindo)
		msg = fmt.Sprintf("%s%s (%s発生)%s%s", timeOnly(t.WarnTime), times, timeOnly(t.EqTime), center, magnitude)
	}
	if len(t.EBI) > 0 {
		msg += " (地域予想震度情報あり)"
	}
	return msg
}

// DetailSummaryText renders the one-line summary used for the detail
// page's <title> and header, matching HTML/eew-show.pl's eew_summary.
// Unlike the list view, this includes the full year-prefixed date and does
// not zero-pad the report number.
func DetailSummaryText(t *decoder.Telegram) string {
	times := fmt.Sprintf(" 第%d報", t.WarnNum)
	if isFinalForViewer(t) {
		times += "(最終)"
	}

	warnFull := fullDateTime(t.WarnTime)
	eqFull := fullDateTime(t.EqTime)

	if t.IsCancellation() {
		return fmt.Sprintf("%s%s (%s発生) 取り消し", warnFull, times, eqFull)
	}
	center := fmt.Sprintf("震央:N%.1f/E%.1f(%s)深さ%dkm", t.CenterLat, t.CenterLng, t.CenterName, t.CenterDepth)
	magnitude := fmt.Sprintf(" 最大:M%.1f 震度%s", t.Magnitude, t.Shindo)
	return fmt.Sprintf("%s%s (%s発生)%s%s", warnFull, times, eqFull, center, magnitude)
}
