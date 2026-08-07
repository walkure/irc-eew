// Package eewmsg formats a decoded EEW telegram into the Slack message text
// and title, porting eew_callback's message-building logic from irc-eew.pl
// (lines ~164-182).
package eewmsg

import (
	"fmt"

	"github.com/walkure/irc-eew/internal/decoder"
)

// Format renders the Slack message text and username/title for a decoded
// telegram.
//
// Deliberate fix vs. the Perl original: irc-eew.pl's eew_callback builds the
// eq-occurrence date using `$wd[2]` (the *warn-report* day) instead of
// `$ed[2]` (the *eq-occurrence* day) — a copy-paste bug (irc-eew.pl:167).
// This port uses the correct eq-occurrence day.
func Format(t *decoder.Telegram) (text, title string) {
	warnParts := splitPairs(t.WarnTime)
	eqParts := splitPairs(t.EqTime)

	warnmsg := fmt.Sprintf("20%s/%s/%s %s:%s:%s",
		at(warnParts, 0), at(warnParts, 1), at(warnParts, 2),
		at(warnParts, 3), at(warnParts, 4), at(warnParts, 5))
	eqmsg := fmt.Sprintf("20%s/%s/%s %s:%s:%s",
		at(eqParts, 0), at(eqParts, 1), at(eqParts, 2),
		at(eqParts, 3), at(eqParts, 4), at(eqParts, 5))

	times := fmt.Sprintf(" 第%d報", t.WarnNum)
	if t.IsFinal() {
		times += "(最終)"
	}

	if t.IsCancellation() {
		text = warnmsg + times + " (" + eqmsg + "発生) 取り消されました"
	} else {
		mapURI := fmt.Sprintf("http://maps.google.com/maps?q=%.1f,%.1f", t.CenterLat, t.CenterLng)
		center := fmt.Sprintf("震央:<%s|N%.1f/E%.1f>(%s)深さ%dkm",
			mapURI, t.CenterLat, t.CenterLng, t.CenterName, t.CenterDepth)
		magnitude := fmt.Sprintf(" 最大:M%.1f 震度%s", t.Magnitude, t.Shindo)
		text = warnmsg + times + " (" + eqmsg + "発生)" + center + magnitude
	}

	title = "緊急地震速報"
	if t.WarnType != "" {
		title = t.WarnType + title
	}
	return text, title
}

// splitPairs splits a digit string into consecutive 2-character chunks,
// matching Perl's `$s =~ /\d\d/og` in list context.
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
