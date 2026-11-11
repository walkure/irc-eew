// Package rawarchive saves DMDATA.JP WebSocket `data` messages exactly as
// received, one file per message, before anything tries to interpret them.
//
// It is deliberately separate from internal/eewlog: that package's
// <eqID>.<warnNum> layout is a contract with internal/eewview for the
// legacy WNI code-telegram archive, whereas this archive keeps the full
// DMDATA envelope (id, head, passing, format, compression, encoding, body)
// verbatim so nothing is lost while the payload format is still being
// worked out.
//
// Layout: <dir>/YYYY/MM/DD/<YYYYMMDDhhmmss.mmm>_<id>.json, dated by
// receive time in the process's local time zone (Asia/Tokyo in the image).
package rawarchive

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Store writes raw messages under one directory.
type Store struct {
	dir string
	now func() time.Time
}

// Open checks dir is writable (creating it if needed) so a misconfigured
// archive fails at startup rather than silently dropping every message.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("rawarchive: creating %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		return nil, fmt.Errorf("rawarchive: %s is not writable: %w", dir, err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return &Store{dir: dir, now: time.Now}, nil
}

// idRE accepts DMDATA's hex message ids; anything else (including an empty
// id from a malformed message) is replaced by a hash of the raw bytes so
// it can never escape the target directory.
var idRE = regexp.MustCompile(`^[0-9a-fA-F]{1,128}$`)

// Save writes raw (the message exactly as received) and returns its path.
// A message whose id was already saved the same day is not written again;
// existed reports that case.
func (s *Store) Save(id string, raw []byte) (path string, existed bool, err error) {
	if !idRE.MatchString(id) {
		sum := sha512.Sum384(raw)
		id = "x" + hex.EncodeToString(sum[:])
	}
	now := s.now()
	dayDir := filepath.Join(s.dir, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", false, fmt.Errorf("rawarchive: %w", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dayDir, "*_"+id+".json")); len(matches) > 0 {
		return matches[0], true, nil
	}
	path = filepath.Join(dayDir, now.Format("20060102150405.000")+"_"+id+".json")

	tmp, err := os.CreateTemp(dayDir, ".tmp-")
	if err != nil {
		return "", false, fmt.Errorf("rawarchive: %w", err)
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return "", false, fmt.Errorf("rawarchive: writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return "", false, fmt.Errorf("rawarchive: %w", err)
	}
	return path, false, nil
}
