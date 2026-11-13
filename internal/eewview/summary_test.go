package eewview

import (
	"os"
	"path/filepath"
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

func legacyFixture(t *testing.T, path string) *record {
	t.Helper()
	return legacyRecord(filepath.Base(path), decodeFixture(t, path))
}

const fixtureDir = "testdata/eewlog/2011/03/12"

func TestListSummaryText_NewReport(t *testing.T) {
	got := ListSummaryText(legacyFixture(t, fixtureDir+"/20110312042346.1"))

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
	got := ListSummaryText(legacyRecord("20110312042346.81", tel))
	if !strings.Contains(got, "第81報(最終)") {
		t.Errorf("expected final marker, got %q", got)
	}
}

func TestListSummaryText_Cancellation(t *testing.T) {
	got := ListSummaryText(legacyFixture(t, fixtureDir+"/20110312134730.3"))
	if !strings.Contains(got, "取り消し") {
		t.Errorf("expected cancellation text, got %q", got)
	}
	if strings.Contains(got, "震央") {
		t.Errorf("cancellation summary should not include hypocenter info, got %q", got)
	}
}

func TestDetailSummaryText_IncludesYearAndFixesDateBug(t *testing.T) {
	got := DetailSummaryText(legacyFixture(t, fixtureDir+"/20110312042346.1"))

	if !strings.Contains(got, "2011/03/12 04:24:50") {
		t.Errorf("expected full warn-time date, got %q", got)
	}
	if !strings.Contains(got, "2011/03/12 04:23:48発生") {
		t.Errorf("expected full eq-time date computed independently, got %q", got)
	}
}

// Unknown legacy values ("/"-filled codes) read 不明 instead of the 0 the
// decoder's numeric fields hold.
func TestLegacyRecord_UnknownsReadUnknown(t *testing.T) {
	tel := decodeFixture(t, fixtureDir+"/20110312042346.1")
	tel.CenterUnknown, tel.DepthUnknown, tel.MagnitudeUnknown = true, true, true
	got := ListSummaryText(legacyRecord("20110312042346.1", tel))
	for _, want := range []string{"震央:不明(", "深さ不明", "最大:M不明"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing %q", got, want)
		}
	}
	if strings.Contains(got, "0.0") || strings.Contains(got, "0km") {
		t.Errorf("unknown shown as zero: %q", got)
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

func TestLegacyRecord_AssumedHypocenter(t *testing.T) {
	raw := "36 04 00 160414212639 C11\n160414212636\nND20160414212638 NCN002 JD////////////// JN///\n" +
		"741 N328 E1307 010 10 04 RK11811 RT/09// RC0////\n9999=\n"
	r := legacyRecord("20160414212638.2", decoder.Decode([]byte(raw)))
	got := ListSummaryText(r)
	if !strings.Contains(got, "震央:N32.8/E130.7(") || !strings.Contains(got, "深さ仮定 最大:M仮定") {
		t.Errorf("%q", got)
	}
	fields := buildFields(r)
	found := false
	for _, f := range fields {
		if f.Label == "震央位置" && strings.Contains(string(f.Value), "(仮定震源要素)") {
			found = true
		}
	}
	if !found {
		t.Errorf("震央位置 should be marked 仮定震源要素: %+v", fields)
	}
}
