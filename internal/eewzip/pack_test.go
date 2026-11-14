package eewzip

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel string, body []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readZip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = b
	}
	return got
}

func TestPack_LegacyOnlyThenMergeDMDATA(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte("37 03 00 261101120000 C11\r\n\x82\xa0 sjis bytes\r\n9999=")
	writeFile(t, dir, "2026/11/01/20261101120000.1", legacy)
	writeFile(t, dir, "2026/11/01/20261101120000.2", []byte("second"))
	writeFile(t, dir, "2026/11/13/20261113105223.1_20261113105229.815_ab.json", []byte(`{"type":"data"}`))
	writeFile(t, dir, "2026/11/13/.tmp-123", []byte("partial"))
	writeFile(t, dir, "2026/12/temp/07/20261207054002.1", []byte("stray"))
	writeFile(t, dir, "2025/01/01/20250101000000.1", []byte("other year"))

	res, err := Pack(PackOptions{Dir: dir, Year: 2026, LegacyOnly: true})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if res.Added != 2 || res.SkippedJSON != 1 || res.Entries != 2 || len(res.Irregular) != 2 {
		t.Errorf("legacy-only result = %+v", res)
	}
	got := readZip(t, filepath.Join(dir, "2026.zip"))
	if string(got["2026/11/01/20261101120000.1"]) != string(legacy) || len(got) != 2 {
		t.Errorf("legacy-only zip = %q", got)
	}

	// Loose files are left alone.
	if _, err := os.Stat(filepath.Join(dir, "2026/11/01/20261101120000.1")); err != nil {
		t.Errorf("loose file touched: %v", err)
	}

	// Re-running without LegacyOnly keeps the packed entries and adds the
	// DMDATA file.
	res, err = Pack(PackOptions{Dir: dir, Year: 2026})
	if err != nil {
		t.Fatalf("Pack (merge): %v", err)
	}
	if res.Kept != 2 || res.AlreadyPacked != 2 || res.Added != 1 || res.Entries != 3 {
		t.Errorf("merge result = %+v", res)
	}
	got = readZip(t, filepath.Join(dir, "2026.zip"))
	if string(got["2026/11/13/20261113105223.1_20261113105229.815_ab.json"]) != `{"type":"data"}` || len(got) != 3 {
		t.Errorf("merged zip = %q", got)
	}
	if string(got["2026/11/01/20261101120000.1"]) != string(legacy) {
		t.Error("kept entry changed by the merge")
	}

	// No temp files are left next to the zip.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestPack_RefusesLooseFileDifferingFromZip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "2026/11/01/20261101120000.1", []byte("original"))
	if _, err := Pack(PackOptions{Dir: dir, Year: 2026}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "2026/11/01/20261101120000.1", []byte("changed"))
	if _, err := Pack(PackOptions{Dir: dir, Year: 2026}); err == nil {
		t.Fatal("Pack accepted a loose file that differs from the packed one")
	}
	if got := readZip(t, filepath.Join(dir, "2026.zip")); string(got["2026/11/01/20261101120000.1"]) != "original" {
		t.Errorf("zip changed after a refused pack: %q", got)
	}
}

func TestPack_OutElsewhere(t *testing.T) {
	dir := t.TempDir()
	outDir := t.TempDir()
	writeFile(t, dir, "2011/03/11/20110311144617.1", []byte("x"))
	out := filepath.Join(outDir, "2011.zip")
	res, err := Pack(PackOptions{Dir: dir, Year: 2011, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entries != 1 {
		t.Errorf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "2011.zip")); err == nil {
		t.Error("zip written into Dir although Out points elsewhere")
	}
}

func TestNames(t *testing.T) {
	if y, ok := ParseZipName(ZipName(2011)); !ok || y != 2011 {
		t.Errorf("ParseZipName(ZipName(2011)) = %d, %v", y, ok)
	}
	for _, bad := range []string{"2011.zip.tmp", "x2011.zip", "201.zip", ".2011.zip.tmp-1"} {
		if _, ok := ParseZipName(bad); ok {
			t.Errorf("ParseZipName(%q) accepted", bad)
		}
	}
	n := EntryName(2011, 3, 12, "20110312042346.81")
	if n != "2011/03/12/20110312042346.81" {
		t.Errorf("EntryName = %q", n)
	}
	if y, m, d, f, ok := ParseEntryName(n); !ok || y != 2011 || m != 3 || d != 12 || f != "20110312042346.81" {
		t.Errorf("ParseEntryName = %d %d %d %q %v", y, m, d, f, ok)
	}
	if _, _, _, _, ok := ParseEntryName("2022/12/temp/07/x"); ok {
		t.Error("ParseEntryName accepted a nested path")
	}
}
