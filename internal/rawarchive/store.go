// Package rawarchive saves DMDATA.JP WebSocket `data` messages exactly as
// received, one file per message, before anything tries to interpret them.
// The bytes are never decoded, re-encoded or normalized.
//
// Its files live in the same tree as the legacy WNI code-telegram archive
// (<eq_id>.<warn_num>, written before the DMDATA migration) and are told
// apart by name.
//
// Layout (read by internal/eewview, keep in sync):
//
//	<dir>/YYYY/MM/DD/<EventID>.<Serial>_<received>_<id>.json
//
// dated by the earthquake (the EventID's date, like the legacy archive),
// where EventID and Serial come from the DMDATA envelope's
// xmlReport.head (no XML parsing needed) and <received> is the receive
// time as YYYYMMDDhhmmss.mmm in the process's local time zone
// (Asia/Tokyo in the image). Serial alone isn't unique (a cancellation or
// correction repeats it), hence the receive time and message id. When the
// envelope carries no usable EventID/Serial the file falls back to
//
//	<dir>/YYYY/MM/DD/<received>_<id>.json
//
// dated by the receive time.
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

// Dir returns the archive root.
func (s *Store) Dir() string { return s.dir }

// idRE accepts DMDATA's hex message ids; anything else (including an empty
// id from a malformed message) is replaced by a hash of the raw bytes so
// it can never escape the target directory.
var idRE = regexp.MustCompile(`^[0-9a-fA-F]{1,128}$`)

var (
	eventIDRE = regexp.MustCompile(`^\d{14}$`)
	serialRE  = regexp.MustCompile(`^\d{1,4}$`)
)

// ReceivedLayout is the time layout of the <received> filename part.
const ReceivedLayout = "20060102150405.000"

// Save writes raw (the message exactly as received) and returns its path.
// eventID and serial are the envelope's xmlReport.head values (may be
// empty). A message whose id was already saved in the same directory is
// not written again; existed reports that case.
func (s *Store) Save(id string, raw []byte, eventID, serial string) (path string, existed bool, err error) {
	if !idRE.MatchString(id) {
		sum := sha512.Sum384(raw)
		id = "x" + hex.EncodeToString(sum[:])
	}
	now := s.now()
	received := now.Format(ReceivedLayout)

	var dayDir, name string
	if eventIDRE.MatchString(eventID) && serialRE.MatchString(serial) {
		dayDir = filepath.Join(s.dir, eventID[0:4], eventID[4:6], eventID[6:8])
		name = eventID + "." + serial + "_" + received + "_" + id + ".json"
	} else {
		dayDir = filepath.Join(s.dir, now.Format("2006"), now.Format("01"), now.Format("02"))
		name = received + "_" + id + ".json"
	}
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", false, fmt.Errorf("rawarchive: %w", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dayDir, "*_"+id+".json")); len(matches) > 0 {
		return matches[0], true, nil
	}
	path = filepath.Join(dayDir, name)

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
