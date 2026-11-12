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

// isFinalForViewer reports whether a legacy telegram is a "最終" (final)
// report using this viewer's own threshold (NCN_type >= 8, matching
// HTML/index.pl and HTML/eew-show.pl exactly) — deliberately distinct from
// decoder.Telegram.IsFinal() (NCNType > 0, matching the notifier's
// irc-eew.pl original). Per the NCN_type code table, only 8/9 mean
// "final/last report"; 6/7 mean "correction". The two original Perl
// components already disagreed on this threshold; unifying them here would
// silently change what "(最終)" has always meant on this archive viewer.
func isFinalForViewer(t *decoder.Telegram) bool {
	return t.NCNType >= 8
}

// center renders "震央:N38.5/E140.8(宮城県北部)深さ10km", or
// "震央:不明(…)" when the position is unknown.
func center(r *record) string {
	pos := "不明"
	if r.PositionKnown {
		pos = fmt.Sprintf("N%.1f/E%.1f", r.Lat, r.Lng)
	}
	name := r.CenterName
	if name == "" {
		name = "不明"
	}
	return "震央:" + pos + "(" + name + ")深さ" + r.DepthText
}

// ListSummaryText renders the one-line summary shown next to each report
// in the list view, matching HTML/index.pl's get_eew_summary. Unlike the
// detail view's summary, this omits the year (redundant with the
// surrounding day breadcrumb) and zero-pads the report number.
func ListSummaryText(r *record) string {
	if r.Unparsable {
		return fmt.Sprintf("第%02d報 (解析できない電文)", r.Serial)
	}
	timesSuffix := "　　　　" // visual padding to roughly match "(最終)"'s width when not final
	if r.Final {
		timesSuffix = "(最終)"
	}
	if r.Corrected {
		timesSuffix += "(訂正)"
	}
	times := fmt.Sprintf("第%02d報%s", r.Serial, timesSuffix)
	occurred := ""
	if r.OriginClock != "" {
		occurred = " (" + r.OriginClock + "発生)"
	}

	var msg string
	if r.Cancelled {
		msg = r.ReportClock + times + occurred + " 取り消し"
	} else {
		msg = r.ReportClock + times + occurred + center(r) + " 最大:" + r.MagText + " 震度" + r.MaxIntText
	}
	if len(r.Areas) > 0 {
		msg += " (地域予想震度情報あり)"
	}
	return msg
}

// DetailSummaryText renders the one-line summary used for the detail
// page's <title> and header, matching HTML/eew-show.pl's eew_summary.
// Unlike the list view, this includes the full year-prefixed date and does
// not zero-pad the report number.
func DetailSummaryText(r *record) string {
	if r.Unparsable {
		return fmt.Sprintf("第%d報 (解析できない電文)", r.Serial)
	}
	times := fmt.Sprintf(" 第%d報", r.Serial)
	if r.Final {
		times += "(最終)"
	}
	if r.Corrected {
		times += "(訂正)"
	}
	occurred := ""
	if r.OriginFull != "" {
		occurred = " (" + r.OriginFull + "発生)"
	}
	if r.Cancelled {
		return r.ReportFull + times + occurred + " 取り消し"
	}
	return r.ReportFull + times + occurred + center(r) + " 最大:" + r.MagText + " 震度" + r.MaxIntText
}
