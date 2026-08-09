// Package eewlog persists raw EEW telegrams to disk, preserving the exact
// directory/filename layout internal/eewview (a separate archive viewer
// with its own deploy pipeline) depends on to re-read archived telegrams.
package eewlog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store manages raw telegram persistence under one directory.
type Store struct {
	dir string
}

// Open verifies dir is writable — mirroring irc-eew.pl's startup self-test
// (create a temp subdir, write a file into it, then remove both) — before
// the daemon starts its main loop, so a misconfigured logdir fails fast
// instead of silently dropping every telegram.
func Open(dir string) (*Store, error) {
	tmpDir, err := os.MkdirTemp(dir, "tmi-")
	if err != nil {
		return nil, fmt.Errorf("eewlog: logdir %s is not writable: %w", dir, err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "tmf")
	if err := os.WriteFile(tmpFile, []byte("TEST\n"), 0o644); err != nil {
		return nil, fmt.Errorf("eewlog: logdir %s is not writable: %w", dir, err)
	}

	return &Store{dir: dir}, nil
}

// WriteTemp writes body under the store's directory as "<unixtime>.<md5>",
// matching irc-eew.pl's eew_callback (which saves the raw bytes before
// decoding, so eq_id/warn_num for the final name aren't known yet).
func (s *Store) WriteTemp(md5 string, body []byte) (string, error) {
	name := fmt.Sprintf("%d.%s", time.Now().Unix(), md5)
	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("eewlog: writing %s: %w", path, err)
	}
	return path, nil
}

// FinalPath computes the archival path for a decoded telegram:
// <dir>/YYYY/MM/DD/<eqID>.<warnNum>. This exactly matches irc-eew.pl's
// get_fn_from_eqid and is also relied on by internal/eewview — do not
// change this layout without also updating (or coordinating with) that
// viewer.
func FinalPath(dir, eqID string, warnNum int) string {
	name := fmt.Sprintf("%s.%d", eqID, warnNum)
	var year, month, day string
	if len(eqID) >= 8 {
		year, month, day = eqID[0:4], eqID[4:6], eqID[6:8]
	}
	return filepath.Join(dir, year, month, day, name)
}

// Finalize moves a temp-named file (from WriteTemp) to its archival path
// (from FinalPath), creating any missing parent directories.
func (s *Store) Finalize(tempPath, eqID string, warnNum int) (string, error) {
	finalPath := FinalPath(s.dir, eqID, warnNum)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return "", fmt.Errorf("eewlog: creating %s: %w", filepath.Dir(finalPath), err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("eewlog: renaming %s to %s: %w", tempPath, finalPath, err)
	}
	return finalPath, nil
}
