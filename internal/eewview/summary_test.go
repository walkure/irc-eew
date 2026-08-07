package eewview

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

const fixtureDir = "testdata/eewlog/2011/03/12"

func TestListSummaryText_NewReport(t *testing.T) {
	tel := decodeFixture(t, fixtureDir+"/20110312042346.1")
	got := ListSummaryText(tel)

	if !strings.Contains(got, "第01報") {
		t.Errorf("expected zero-padded report number, got %q", got)
	}
	if strings.Contains(got, "(最終)") {
		t.Errorf("report #1 should not be marked final, got %q", got)
	}
	if !strings.HasPrefix(got, "04:24:50第01報") {
		t.Errorf("expected time-only (no year) warn timestamp, got %q", got)
	}
	if !strings.Contains(got, "04:23:48発生") {
		t.Errorf("expected time-only (no year) eq timestamp, got %q", got)
	}
	if strings.Contains(got, "2011/") {
		t.Errorf("list summary should omit the year, got %q", got)
	}
}

func TestListSummaryText_FinalReport(t *testing.T) {
	tel := decodeFixture(t, fixtureDir+"/20110312042346.81")
	if !isFinalForViewer(tel) {
		t.Fatalf("fixture NCNType=%d expected to be >=8 (final for viewer)", tel.NCNType)
	}
	got := ListSummaryText(tel)
	if !strings.Contains(got, "第81報(最終)") {
		t.Errorf("expected final marker, got %q", got)
	}
}

func TestListSummaryText_Cancellation(t *testing.T) {
	tel := decodeFixture(t, fixtureDir+"/20110312134730.3")
	got := ListSummaryText(tel)
	if !strings.Contains(got, "取り消し") {
		t.Errorf("expected cancellation text, got %q", got)
	}
	if strings.Contains(got, "震央") {
		t.Errorf("cancellation summary should not include hypocenter info, got %q", got)
	}
}

func TestDetailSummaryText_IncludesYearAndFixesDateBug(t *testing.T) {
	tel := decodeFixture(t, fixtureDir+"/20110312042346.1")
	got := DetailSummaryText(tel)

	if !strings.Contains(got, "2011/03/12 04:24:50") {
		t.Errorf("expected full warn-time date, got %q", got)
	}
	if !strings.Contains(got, "2011/03/12 04:23:48発生") {
		t.Errorf("expected full eq-time date computed independently, got %q", got)
	}
}

func TestIsFinalForViewer_DiffersFromDecoderIsFinal(t *testing.T) {
	// A telegram with NCNType in [1,7] is "final" under decoder.IsFinal()
	// (>0, the notifier's definition) but not under isFinalForViewer (>=8,
	// this viewer's own definition) — assert the two are genuinely
	// distinct rather than accidentally unified.
	tel := &decoder.Telegram{NCNType: 6}
	if !tel.IsFinal() {
		t.Fatal("test setup: expected decoder.IsFinal() to be true for NCNType=6")
	}
	if isFinalForViewer(tel) {
		t.Error("isFinalForViewer(NCNType=6) should be false (6 means correction, not final)")
	}
}
