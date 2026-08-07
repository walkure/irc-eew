package decoder

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDecode_RealCorpus runs Decode over every real historical telegram in
// the eewlog/ archive (45,802 files spanning 2010-2023 at last count) that
// this repository happens to have on disk locally (gitignored — not part of
// the committed source, and not guaranteed to exist in every environment
// this test runs in, hence the Skip below). It exists to catch panics or
// wildly-out-of-range values across 13 years of real WNI wire formats that
// the small hand-picked fixtures in decoder_test.go can't be expected to
// cover on their own.
//
// Set EEWLOG_CORPUS_DIR to point at a different location; defaults to the
// repo-root eewlog/ directory (two levels up from this package).
func TestDecode_RealCorpus(t *testing.T) {
	dir := os.Getenv("EEWLOG_CORPUS_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "eewlog")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("corpus directory %s not available (%v) — skipping; this is expected outside the author's local checkout", dir, err)
	}

	var (
		total, empty, decoded             int
		withEqID, withHypocenter, withEBI int
	)

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		total++

		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: read error: %v", path, err)
			return nil
		}
		if len(body) == 0 {
			empty++
			return nil
		}

		tel := decodeRecovering(t, path, body)
		if tel == nil {
			return nil
		}
		decoded++

		if tel.EqID != "" {
			withEqID++
			if len(tel.EqID) != 14 {
				t.Errorf("%s: EqID %q is not 14 digits", path, tel.EqID)
			}
		}
		if tel.CenterCode != "" {
			withHypocenter++
			if tel.CenterLat < -90 || tel.CenterLat > 90 {
				t.Errorf("%s: implausible CenterLat %v", path, tel.CenterLat)
			}
			if tel.CenterLng < -180 || tel.CenterLng > 180 {
				t.Errorf("%s: implausible CenterLng %v", path, tel.CenterLng)
			}
			if tel.Magnitude < 0 || tel.Magnitude > 10 {
				t.Errorf("%s: implausible Magnitude %v", path, tel.Magnitude)
			}
		}
		if len(tel.EBI) > 0 {
			withEBI++
			for area, e := range tel.EBI {
				if len(area) != 3 {
					t.Errorf("%s: EBI area code %q is not 3 digits", path, area)
				}
				if len(e.Time) != 6 && e.Time != "//////" {
					t.Errorf("%s: EBI[%s].Time %q has unexpected length", path, area, e.Time)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}

	t.Logf("corpus: %d files, %d empty, %d decoded (%d with eq_id, %d with hypocenter, %d with EBI)",
		total, empty, decoded, withEqID, withHypocenter, withEBI)
	if decoded == 0 {
		t.Fatal("decoded 0 files from a non-empty corpus directory — decoder likely broken")
	}
}

func decodeRecovering(t *testing.T, path string, body []byte) (tel *Telegram) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: Decode panicked: %v", path, r)
			tel = nil
		}
	}()
	return Decode(body)
}
