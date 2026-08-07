package eewmsg

import (
	"os"
	"strings"
	"testing"

	"github.com/walkure/irc-eew/internal/decoder"
)

func decodeFixture(t *testing.T, path string) *decoder.Telegram {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return decoder.Decode(b)
}

// Expected text/title values were read directly off the ~10-years-in-
// production Perl irc-eew.pl's own stdout when run against these exact
// fixture files through the wnisim fake WNI server (see the wni/decoder
// packages' fixtures, drawn from the same real 2011-03-12 event).

func TestFormat_NewReport(t *testing.T) {
	tel := decodeFixture(t, "../decoder/testdata/2011_new_report.txt")
	text, title := Format(tel)

	wantText := "2011/03/12 04:24:50 第1報 (2011/03/12 04:23:48発生)" +
		"震央:<http://maps.google.com/maps?q=38.5,140.8|N38.5/E140.8>(宮城県北部)深さ10km 最大:M6.8 震度6強"
	if text != wantText {
		t.Errorf("text mismatch:\n got  %q\n want %q", text, wantText)
	}
	if wantTitle := "高度利用者向け緊急地震速報"; title != wantTitle {
		t.Errorf("title: got %q, want %q", title, wantTitle)
	}
}

func TestFormat_FinalReport(t *testing.T) {
	tel := decodeFixture(t, "../decoder/testdata/2011_final_report.txt")
	text, _ := Format(tel)

	wantText := "2011/03/12 04:26:14 第81報(最終) (2011/03/12 04:23:48発生)" +
		"震央:<http://maps.google.com/maps?q=38.5,140.8|N38.5/E140.8>(宮城県北部)深さ10km 最大:M6.7 震度6強"
	if text != wantText {
		t.Errorf("text mismatch:\n got  %q\n want %q", text, wantText)
	}
}

func TestFormat_Cancellation(t *testing.T) {
	tel := decodeFixture(t, "../decoder/testdata/2011_cancellation.txt")
	text, _ := Format(tel)

	wantText := "2011/03/12 13:47:52 第3報 (2011/03/12 13:47:02発生) 取り消されました"
	if text != wantText {
		t.Errorf("text mismatch:\n got  %q\n want %q", text, wantText)
	}
}

// TestFormat_UsesEqOccurrenceDayNotWarnReportDay locks in the deliberate fix
// of irc-eew.pl's date bug (eew_callback used `$wd[2]`, the warn-report day,
// instead of `$ed[2]`, the eq-occurrence day, when building the "発生"
// (occurred) timestamp). The fixture is a real telegram for a quake that
// occurred at 2013-04-17 23:59:43 but was warned about at 2013-04-18
// 00:00:19 — i.e. the warn-report day (18) and eq-occurrence day (17)
// genuinely differ, which is exactly the case the original bug got wrong.
func TestFormat_UsesEqOccurrenceDayNotWarnReportDay(t *testing.T) {
	tel := decodeFixture(t, "testdata/2013_midnight_straddle.txt")
	if tel.WarnTime[4:6] == tel.EqTime[4:6] {
		t.Fatalf("fixture no longer straddles midnight (warn day == eq day); need a different fixture to exercise the bug fix")
	}

	text, _ := Format(tel)

	if got, wantSubstr := text, "2013/04/17 23:59:43発生"; !strings.Contains(got, wantSubstr) {
		t.Errorf("expected the correct eq-occurrence date (2013/04/17) in text, got %q", got)
	}
	if bad := "2013/04/18 23:59:43発生"; strings.Contains(text, bad) {
		t.Errorf("text still reproduces the original date bug (used warn-report day): %q", text)
	}
	// The warn-report timestamp itself should still show its own (correct) day.
	if got, wantSubstr := text, "2013/04/18 00:00:19"; !strings.Contains(got, wantSubstr) {
		t.Errorf("expected the warn-report date/time in text, got %q", got)
	}
}
