package eewview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/walkure/irc-eew/internal/decoder"
)

// TestRenderRealCorpus_NoPanics runs both summary formatters and the
// detail-field builder over every real historical telegram in eewlog/ (if
// present locally — gitignored, not guaranteed to exist in CI, skips
// gracefully otherwise), mirroring internal/decoder's TestDecode_RealCorpus.
// Catches formatting-code panics (nil map access, short digit strings,
// etc.) across 13 years of real wire-format variance at near-zero cost.
func TestRenderRealCorpus_NoPanics(t *testing.T) {
	dir := os.Getenv("EEWLOG_CORPUS_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "eewlog")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("corpus directory %s not available (%v) — skipping", dir, err)
	}

	var total int
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil || len(body) == 0 {
			return nil
		}
		total++
		renderRecovering(t, path, body)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	t.Logf("rendered %d real telegrams without panics", total)
	if total == 0 {
		t.Fatal("rendered 0 files from a non-empty corpus directory")
	}
}

func renderRecovering(t *testing.T, path string, body []byte) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: rendering panicked: %v", path, r)
		}
	}()
	tel := decoder.Decode(body)
	_ = ListSummaryText(tel)
	_ = DetailSummaryText(tel)
	_ = buildFields(tel)
	_ = buildEBIRows(tel)
}
