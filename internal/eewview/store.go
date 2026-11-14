package eewview

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/walkure/irc-eew/internal/eewzip"
)

// store is the viewer's view of the archive: the loose
// <DataDir>/YYYY/MM/DD/<file> tree and the <DataDir>/YYYY.zip files of
// packed years (internal/eewzip), merged so a reader never sees which one
// a telegram is in. A file present both loose and packed counts once (the
// loose copy is read).
//
// A year's zip index is read the first time that year is looked at and
// kept; the zip's size and modification time are checked on every look,
// and a changed zip (re-packed) or a removed one is picked up then. The
// zip itself is not kept open: entries are read by offset from a freshly
// opened file and CRC-checked, so the packer can replace the zip at any
// time (also on Windows, where an open file can't be replaced).
type store struct {
	dir string

	mu   sync.Mutex
	zips map[int]*zipIndex
}

// zipIndex is what the viewer keeps of one zip: just enough to read an
// entry without parsing the zip's central directory again.
type zipIndex struct {
	path    string
	modTime time.Time
	size    int64
	days    map[[2]int][]zipEntry // [month, day] -> entries
}

type zipEntry struct {
	file   string // file name inside the day directory
	offset int64  // of the entry's (compressed) data
	csize  int64
	usize  int64
	method uint16
	crc    uint32
}

func newStore(dir string) *store {
	return &store{dir: dir, zips: map[int]*zipIndex{}}
}

// dayFile is one archived telegram of a day.
type dayFile struct {
	name string // file name inside the day directory
	read func() ([]byte, error)
}

// years lists the years with a directory or a zip.
func (s *store) years() []int {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	set := map[int]bool{}
	for _, e := range entries {
		if e.IsDir() {
			if n, ok := numericName(e.Name()); ok {
				set[n] = true
			}
			continue
		}
		if y, ok := eewzip.ParseZipName(e.Name()); ok {
			set[y] = true
		}
	}
	return sortedKeys(set)
}

func (s *store) hasYear(year int) bool {
	return dirExists(s.yearDir(year)) || fileExists(filepath.Join(s.dir, eewzip.ZipName(year)))
}

func (s *store) months(year int) []int {
	set := map[int]bool{}
	for _, n := range numericSubdirs(s.yearDir(year)) {
		if v, ok := numericName(n); ok {
			set[v] = true
		}
	}
	if z := s.zipFor(year); z != nil {
		for k := range z.days {
			set[k[0]] = true
		}
	}
	return sortedKeys(set)
}

func (s *store) hasMonth(year, month int) bool { return contains(s.months(year), month) }

func (s *store) days(year, month int) []int {
	set := map[int]bool{}
	for _, n := range numericSubdirs(s.monthDir(year, month)) {
		if v, ok := numericName(n); ok {
			set[v] = true
		}
	}
	if z := s.zipFor(year); z != nil {
		for k := range z.days {
			if k[0] == month {
				set[k[1]] = true
			}
		}
	}
	return sortedKeys(set)
}

func (s *store) hasDay(year, month, day int) bool { return contains(s.days(year, month), day) }

// dayFiles lists the telegrams archived for one day: loose files, then
// packed ones not also present loose.
func (s *store) dayFiles(year, month, day int) []dayFile {
	var files []dayFile
	seen := map[string]bool{}
	dayDir := filepath.Join(s.monthDir(year, month), twoDigits(day))
	if entries, err := os.ReadDir(dayDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dayDir, e.Name())
			seen[e.Name()] = true
			files = append(files, dayFile{name: e.Name(), read: func() ([]byte, error) { return os.ReadFile(path) }})
		}
	}
	z := s.zipFor(year)
	if z == nil {
		return files
	}
	var packed []zipEntry
	for _, e := range z.days[[2]int{month, day}] {
		if !seen[e.file] {
			packed = append(packed, e)
		}
	}
	if len(packed) == 0 {
		return files
	}
	bodies, err := z.readEntries(packed)
	for i, e := range packed {
		var body []byte
		if err == nil {
			body = bodies[i]
		}
		files = append(files, dayFile{name: e.file, read: func() ([]byte, error) { return body, err }})
	}
	return files
}

// readEntries reads entries of the zip with one open of the file.
func (z *zipIndex) readEntries(entries []zipEntry) ([][]byte, error) {
	f, err := os.Open(z.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([][]byte, len(entries))
	for i, e := range entries {
		b, err := e.read(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", z.path, e.file, err)
		}
		out[i] = b
	}
	return out, nil
}

func (e zipEntry) read(f io.ReaderAt) ([]byte, error) {
	var r io.Reader = io.NewSectionReader(f, e.offset, e.csize)
	switch e.method {
	case zip.Store:
	case zip.Deflate:
		fr := flate.NewReader(r)
		defer fr.Close()
		r = fr
	default:
		return nil, fmt.Errorf("unsupported zip method %d", e.method)
	}
	b, err := io.ReadAll(io.LimitReader(r, e.usize+1))
	if err != nil {
		return nil, err
	}
	// The zip may have been replaced since it was indexed: a size or CRC
	// mismatch is reported instead of showing wrong bytes.
	if int64(len(b)) != e.usize || crc32.ChecksumIEEE(b) != e.crc {
		return nil, errors.New("zip entry does not match its index (zip replaced?)")
	}
	return b, nil
}

// zipFor returns year's zip index, (re)loading it if the zip is new or
// changed, or nil if the year has no usable zip.
func (s *store) zipFor(year int) *zipIndex {
	path := filepath.Join(s.dir, eewzip.ZipName(year))
	fi, statErr := os.Stat(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	cur := s.zips[year]
	if cur != nil && statErr == nil && cur.modTime.Equal(fi.ModTime()) && cur.size == fi.Size() {
		return cur
	}
	delete(s.zips, year)
	if statErr != nil {
		if !errors.Is(statErr, fs.ErrNotExist) {
			logZipError(path, statErr)
		}
		return nil
	}
	z, err := openZipIndex(path, year, fi)
	if err != nil {
		logZipError(path, err)
		return nil
	}
	s.zips[year] = z
	return z
}

func openZipIndex(path string, year int, fi fs.FileInfo) (*zipIndex, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	z := &zipIndex{path: path, modTime: fi.ModTime(), size: fi.Size(), days: map[[2]int][]zipEntry{}}
	for _, f := range rc.File {
		y, m, d, file, ok := eewzip.ParseEntryName(f.Name)
		if !ok || y != year {
			continue
		}
		off, err := f.DataOffset()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		k := [2]int{m, d}
		z.days[k] = append(z.days[k], zipEntry{
			file:   file,
			offset: off,
			csize:  int64(f.CompressedSize64),
			usize:  int64(f.UncompressedSize64),
			method: f.Method,
			crc:    f.CRC32,
		})
	}
	return z, nil
}

func logZipError(path string, err error) {
	slog.Warn("eew-view: ignoring unreadable archive zip", "path", path, "error", err)
}

func (s *store) yearDir(year int) string { return filepath.Join(s.dir, strconv.Itoa(year)) }

func (s *store) monthDir(year, month int) string {
	return filepath.Join(s.yearDir(year), twoDigits(month))
}

func twoDigits(n int) string { return fmt.Sprintf("%02d", n) }

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func numericName(s string) (int, bool) {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
