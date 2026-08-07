package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/walkure/irc-eew/internal/eewlog"
	"github.com/walkure/irc-eew/internal/notify"
	"github.com/walkure/irc-eew/internal/slack"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "decoder", "testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func TestProcessor_NewReport_ArchivesAndSelectsBothTiers(t *testing.T) {
	dir := t.TempDir()
	store, err := eewlog.Open(dir)
	if err != nil {
		t.Fatalf("eewlog.Open: %v", err)
	}
	dispatcher := notify.NewDispatcher(
		[]slack.Hook{{Name: "all-1", URL: "https://example.com/all"}},
		[]slack.Hook{{Name: "limited-1", URL: "https://example.com/limited"}},
	)
	p := NewProcessor(store, dispatcher)

	result := p.Process("deadbeef", readFixture(t, "2011_new_report.txt"))

	wantText := "2011/03/12 04:24:50 第1報 (2011/03/12 04:23:48発生)" +
		"震央:<http://maps.google.com/maps?q=38.5,140.8|N38.5/E140.8>(宮城県北部)深さ10km 最大:M6.8 震度6強"
	if result.Text != wantText {
		t.Errorf("Text mismatch:\n got  %q\n want %q", result.Text, wantText)
	}
	if result.Title != "高度利用者向け緊急地震速報" {
		t.Errorf("Title: got %q", result.Title)
	}
	if len(result.Hooks) != 2 {
		t.Errorf("expected both tiers for a new quake's first report, got %d hooks", len(result.Hooks))
	}

	wantArchive := filepath.Join(dir, "2011", "03", "12", "20110312042346.1")
	if _, err := os.Stat(wantArchive); err != nil {
		t.Errorf("expected archived file at %s: %v", wantArchive, err)
	}
}

func TestProcessor_RoutineReport_SkipsLimitedTier(t *testing.T) {
	dispatcher := notify.NewDispatcher(
		[]slack.Hook{{Name: "all-1"}},
		[]slack.Hook{{Name: "limited-1"}},
	)
	p := NewProcessor(nil, dispatcher) // archival disabled, matching an unset logdir

	p.Process("md5-1", readFixture(t, "2011_new_report.txt")) // establishes lastEqID
	result := p.Process("md5-2", readFixture(t, "2011_new_report.txt"))

	if len(result.Hooks) != 1 || result.Hooks[0].Name != "all-1" {
		t.Errorf("expected only the all-tier hook for a routine repeat, got %+v", result.Hooks)
	}
}

func TestProcessor_FinalReport_FiresLimitedTierEvenForKnownQuake(t *testing.T) {
	dispatcher := notify.NewDispatcher(
		[]slack.Hook{{Name: "all-1"}},
		[]slack.Hook{{Name: "limited-1"}},
	)
	p := NewProcessor(nil, dispatcher)

	p.Process("md5-1", readFixture(t, "2011_new_report.txt")) // same eq_id as the final report below
	result := p.Process("md5-2", readFixture(t, "2011_final_report.txt"))

	if len(result.Hooks) != 2 {
		t.Errorf("expected both tiers for a final report of an already-seen quake, got %d hooks", len(result.Hooks))
	}
	if !result.Telegram.IsFinal() {
		t.Error("expected the decoded telegram to report IsFinal() == true")
	}
}

func TestProcessor_Cancellation_NoArchiveHypocenterButStillNotifies(t *testing.T) {
	dir := t.TempDir()
	store, err := eewlog.Open(dir)
	if err != nil {
		t.Fatalf("eewlog.Open: %v", err)
	}
	dispatcher := notify.NewDispatcher(nil, []slack.Hook{{Name: "limited-1"}})
	p := NewProcessor(store, dispatcher)

	result := p.Process("md5-cancel", readFixture(t, "2011_cancellation.txt"))

	if result.Text != "2011/03/12 13:47:52 第3報 (2011/03/12 13:47:02発生) 取り消されました" {
		t.Errorf("Text: got %q", result.Text)
	}
	if len(result.Hooks) != 1 {
		t.Errorf("expected the limited hook to fire for a cancellation, got %+v", result.Hooks)
	}
	wantArchive := filepath.Join(dir, "2011", "03", "12", "20110312134730.3")
	if _, err := os.Stat(wantArchive); err != nil {
		t.Errorf("expected archived file at %s: %v", wantArchive, err)
	}
}
