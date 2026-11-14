package rawarchive

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixedStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "raw"))
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 12, 9, 8, 7, 654_000_000, time.Local) }
	return s
}

func TestSave_LayoutAndVerbatimContent(t *testing.T) {
	s := fixedStore(t)
	raw := []byte(`{"type":"data","id":"abc123","body":"..."}`)
	path, existed, err := s.Save("abc123", raw, "", "")
	if err != nil || existed {
		t.Fatalf("Save = %q, %v, %v", path, existed, err)
	}
	want := filepath.Join(s.dir, "2026", "11", "12", "20261112090807.654_abc123.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, raw) {
		t.Errorf("content = %q, want verbatim %q", got, raw)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("leftover temp files: %v", entries)
	}
}

func TestSave_DuplicateIDNotRewritten(t *testing.T) {
	s := fixedStore(t)
	p1, _, _ := s.Save("abc", []byte("first"), "", "")
	p2, existed, err := s.Save("abc", []byte("second"), "", "")
	if err != nil || !existed || p1 != p2 {
		t.Fatalf("second Save = %q, %v, %v", p2, existed, err)
	}
	got, _ := os.ReadFile(p1)
	if string(got) != "first" {
		t.Errorf("content overwritten: %q", got)
	}
}

func TestSave_UnsafeOrMissingIDIsHashed(t *testing.T) {
	s := fixedStore(t)
	for _, id := range []string{"", "../../etc", "a/b"} {
		path, _, err := s.Save(id, []byte("raw:"+id), "", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(path, filepath.Join(s.dir, "2026", "11", "12")+string(filepath.Separator)) ||
			!strings.Contains(filepath.Base(path), "_x") {
			t.Errorf("id %q saved at unexpected path %q", id, path)
		}
	}
}

// With EventID/Serial from the envelope, the file is dated by the
// earthquake, not by when it was received (here: a quake just before
// midnight whose report arrives after it).
func TestSave_DatedByEventIDWhenKnown(t *testing.T) {
	s := fixedStore(t) // receives on 2026-11-12
	path, _, err := s.Save("abc", []byte("x"), "20261111235958", "3")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.dir, "2026", "11", "11", "20261111235958.3_20261112090807.654_abc.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	// A malformed EventID/Serial falls back to the receive date.
	for _, c := range [][2]string{{"2026111123595", "3"}, {"20261111235958", "x"}, {"../../etc/xx", "1"}} {
		p, _, err := s.Save("id"+c[1], []byte("y"), c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(p) != filepath.Join(s.dir, "2026", "11", "12") {
			t.Errorf("%v: fell back to %q", c, p)
		}
	}
}

// Saved files are world-readable like the legacy archive (CreateTemp's
// 0600 would hide them from anyone but the notifier's user).
func TestSave_FileIsWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	s := fixedStore(t)
	path, _, err := s.Save("abc", []byte("x"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, want 644", got)
	}
}
