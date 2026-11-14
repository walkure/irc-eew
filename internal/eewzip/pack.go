package eewzip

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PackOptions configures Pack.
type PackOptions struct {
	// Dir is the archive root (logdir).
	Dir string
	// Year is the year directory to pack.
	Year int
	// Out is the zip to write; empty means <Dir>/<Year>.zip. An existing
	// zip at Out is merged: its entries are kept and only loose files it
	// doesn't have yet are added, so packing is safe to re-run.
	Out string
	// LegacyOnly packs only legacy WNI telegrams (every file not ending in
	// .json), for a year in which DMDATA telegrams may still arrive.
	LegacyOnly bool
}

// PackResult reports what Pack did.
type PackResult struct {
	Out string
	// Entries is the number of entries in the written zip.
	Entries int
	// Kept were already in the existing zip at Out.
	Kept int
	// Added are loose files newly packed.
	Added int
	// AlreadyPacked are loose files whose identical bytes were already in
	// the existing zip (not added twice).
	AlreadyPacked int
	// SkippedJSON are DMDATA files left out because of LegacyOnly.
	SkippedJSON int
	// Irregular are files under the year directory that are not in the
	// YYYY/MM/DD/<file> layout (or are dot files, e.g. a notifier temp
	// file); they are not packed. Paths are relative to Dir.
	Irregular []string
}

// source is where an entry's bytes come from.
type source struct {
	zf   *zip.File // from the existing zip
	path string    // or from a loose file
	fi   fs.FileInfo
}

func (s source) read() ([]byte, error) {
	if s.zf != nil {
		rc, err := s.zf.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return os.ReadFile(s.path)
}

// Pack writes the year's archived files into one zip (see the package
// doc), verifies that every entry reads back byte for byte equal to its
// source, and only then moves it into place at Out. It never modifies or
// deletes the loose files.
func Pack(opt PackOptions) (*PackResult, error) {
	out := opt.Out
	if out == "" {
		out = filepath.Join(opt.Dir, ZipName(opt.Year))
	}
	res := &PackResult{Out: out}
	sources := map[string]source{}

	// Existing zip entries.
	var old *zip.ReadCloser
	if _, err := os.Stat(out); err == nil {
		old, err = zip.OpenReader(out)
		if err != nil {
			return nil, fmt.Errorf("eewzip: opening existing %s: %w", out, err)
		}
		defer func() {
			if old != nil {
				old.Close()
			}
		}()
		for _, f := range old.File {
			if _, dup := sources[f.Name]; dup {
				return nil, fmt.Errorf("eewzip: existing %s has %s twice", out, f.Name)
			}
			sources[f.Name] = source{zf: f}
			res.Kept++
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if err := collectLoose(opt, out, sources, res); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)

	tmp, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".tmp-")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if err := writeZip(tmp, names, sources); err != nil {
		return nil, err
	}
	// CreateTemp makes it 0600; the viewer may run as another user.
	if err := tmp.Chmod(0o644); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := verify(tmpName, names, sources); err != nil {
		return nil, err
	}

	// The old zip has to be closed before it is replaced (Windows).
	if old != nil {
		old.Close()
		old = nil
	}
	if err := os.Rename(tmpName, out); err != nil {
		return nil, err
	}
	keep = true
	res.Entries = len(names)
	return res, nil
}

func writeZip(w io.Writer, names []string, sources map[string]source) error {
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})
	for _, n := range names {
		s := sources[n]
		if s.zf != nil {
			// Already compressed in the old zip: copy as is.
			if err := zw.Copy(s.zf); err != nil {
				return fmt.Errorf("eewzip: copying %s: %w", n, err)
			}
			continue
		}
		body, err := s.read()
		if err != nil {
			return err
		}
		ew, err := zw.CreateHeader(&zip.FileHeader{
			Name:     n,
			Method:   zip.Deflate,
			Modified: s.fi.ModTime(),
		})
		if err != nil {
			return err
		}
		if _, err := ew.Write(body); err != nil {
			return err
		}
	}
	return zw.Close()
}

// verify re-reads every entry of the written zip (CRC-checked by
// archive/zip) and compares it with its source.
func verify(path string, names []string, sources map[string]source) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("eewzip: verify: %w", err)
	}
	defer zr.Close()
	if len(zr.File) != len(names) {
		return fmt.Errorf("eewzip: verify: %d entries written, want %d", len(zr.File), len(names))
	}
	for i, f := range zr.File {
		if f.Name != names[i] {
			return fmt.Errorf("eewzip: verify: entry %d is %s, want %s", i, f.Name, names[i])
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("eewzip: verify %s: %w", f.Name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("eewzip: verify %s: %w", f.Name, err)
		}
		want, err := sources[f.Name].read()
		if err != nil {
			return fmt.Errorf("eewzip: verify %s: reading source: %w", f.Name, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("eewzip: verify %s: bytes differ from source", f.Name)
		}
	}
	return nil
}

// collectLoose adds the year directory's files to sources (see
// PackOptions/PackResult for what is taken and what is reported).
func collectLoose(opt PackOptions, out string, sources map[string]source, res *PackResult) error {
	yearDir := filepath.Join(opt.Dir, fmt.Sprintf("%04d", opt.Year))
	err := filepath.WalkDir(yearDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(opt.Dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		y, _, _, file, ok := ParseEntryName(rel)
		if !ok || y != opt.Year || strings.HasPrefix(file, ".") || !d.Type().IsRegular() {
			res.Irregular = append(res.Irregular, rel)
			return nil
		}
		if opt.LegacyOnly && strings.HasSuffix(file, ".json") {
			res.SkippedJSON++
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		loose := source{path: path, fi: fi}
		prev, ok := sources[rel]
		if !ok {
			sources[rel] = loose
			res.Added++
			return nil
		}
		if err := sameBytes(prev, loose); err != nil {
			return fmt.Errorf("eewzip: %s vs the copy in %s: %w", path, out, err)
		}
		res.AlreadyPacked++
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil

}

// sameBytes checks that a loose file equals the copy already packed.
func sameBytes(packed, loose source) error {
	a, err := packed.read()
	if err != nil {
		return err
	}
	b, err := loose.read()
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return errors.New("bytes differ")
	}
	return nil
}
