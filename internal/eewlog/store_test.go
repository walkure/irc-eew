package eewlog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/walkure/irc-eew/internal/eewlog"
)

func TestOpen_Succeeds_WritableDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := eewlog.Open(dir); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// The self-test's temp subdir must not be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected the self-test to clean up after itself, found: %v", entries)
	}
}

func TestOpen_Fails_NonexistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := eewlog.Open(dir); err == nil {
		t.Fatal("expected an error for a nonexistent logdir")
	}
}

func TestFinalPath_MatchesViewerLayout(t *testing.T) {
	// This must match the real archive layout in eewlog/ exactly (e.g.
	// eewlog/2011/03/12/20110312042346.81) — internal/eewview depends on it.
	got := eewlog.FinalPath("/data", "20110312042346", 81)
	want := filepath.Join("/data", "2011", "03", "12", "20110312042346.81")
	if got != want {
		t.Errorf("FinalPath: got %q, want %q", got, want)
	}
}

func TestWriteTempAndFinalize(t *testing.T) {
	dir := t.TempDir()
	store, err := eewlog.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	body := []byte("37 03 00 110312042450 C11\n")
	tempPath, err := store.WriteTemp("abc123", body)
	if err != nil {
		t.Fatalf("WriteTemp: %v", err)
	}
	if got, err := os.ReadFile(tempPath); err != nil || string(got) != string(body) {
		t.Fatalf("temp file content mismatch: got %q, err %v", got, err)
	}

	finalPath, err := store.Finalize(tempPath, "20110312042346", 81)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	wantFinal := filepath.Join(dir, "2011", "03", "12", "20110312042346.81")
	if finalPath != wantFinal {
		t.Errorf("Finalize path: got %q, want %q", finalPath, wantFinal)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Errorf("expected temp file to be gone after Finalize, stat err: %v", err)
	}
	if got, err := os.ReadFile(finalPath); err != nil || string(got) != string(body) {
		t.Errorf("final file content mismatch: got %q, err %v", got, err)
	}
}
