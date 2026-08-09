package irc

import "regexp"

// linkPattern matches internal/eewmsg.Format's Slack-style <url|label>
// markup (e.g. "震央:<http://maps.google.com/...|東京都>"), mirroring
// irc-eew.pl's remove_link() regex (`s/\<(.*?)\|(.*?)\>/$2/g`) exactly.
var linkPattern = regexp.MustCompile(`<(.*?)\|(.*?)>`)

// StripLinks removes eewmsg.Format's Slack link markup, leaving just the
// label — IRC has no equivalent hyperlink syntax.
func StripLinks(s string) string {
	return linkPattern.ReplaceAllString(s, "$2")
}
