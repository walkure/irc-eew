package irc

import "testing"

func TestStripLinks(t *testing.T) {
	in := "2026/08/09 08:02:14 第1報 (2026/08/09 08:02:06発生)震央:<http://maps.google.com/maps?q=32.5,130.5|N32.5/E130.5>(熊本県天草・芦北地方)深さ10km 最大:M3.7 震度3"
	want := "2026/08/09 08:02:14 第1報 (2026/08/09 08:02:06発生)震央:N32.5/E130.5(熊本県天草・芦北地方)深さ10km 最大:M3.7 震度3"
	if got := StripLinks(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripLinks_NoLinks(t *testing.T) {
	in := "2026/08/09 08:03:24 第7報(最終) (2026/08/09 08:02:06発生) 取り消されました"
	if got := StripLinks(in); got != in {
		t.Errorf("got %q, want unchanged %q", got, in)
	}
}
