package eewview

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/eewzip"
)

// copyTree copies the fixture archive into a fresh directory.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func pack(t *testing.T, dir string, year int) {
	t.Helper()
	if _, err := eewzip.Pack(eewzip.PackOptions{Dir: dir, Year: year}); err != nil {
		t.Fatalf("Pack %d: %v", year, err)
	}
}

func storeServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	h, err := NewServer(Config{DataDir: dir, PathBase: "./", ViewerPath: "eew-show"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func fetch(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

var nameLinkRE = regexp.MustCompile(`eew-show\?name=([^"&]+)`)

// Every page reads the same whether the fixture years are loose, packed
// into zips with the loose files removed, or both at once.
func TestStore_PackedArchiveReadsLikeLoose(t *testing.T) {
	loose := copyTree(t, fixtureEewlogRoot)

	packed := copyTree(t, fixtureEewlogRoot)
	for _, y := range []int{2011, 2026} {
		pack(t, packed, y)
		if err := os.RemoveAll(filepath.Join(packed, filepath.FromSlash(itoa(y)))); err != nil {
			t.Fatal(err)
		}
	}

	both := copyTree(t, fixtureEewlogRoot)
	pack(t, both, 2011)
	pack(t, both, 2026)

	ref := storeServer(t, loose)
	others := map[string]*httptest.Server{"packed": storeServer(t, packed), "both": storeServer(t, both)}

	pages := []string{
		"/", "/?year=2011", "/?year=2011&month=3", "/?year=2026", "/?year=2026&month=11",
		"/?year=2011&month=3&day=12", "/?year=2026&month=11&day=12", "/?year=2026&month=11&day=13",
		"/eew-show?name=99999999999999.1", "/eew-show?name=../x",
	}
	for _, p := range pages[5:8] {
		_, body := fetch(t, ref, p)
		for _, m := range nameLinkRE.FindAllStringSubmatch(body, -1) {
			pages = append(pages, "/eew-show?name="+m[1])
		}
	}
	if len(pages) < 20 {
		t.Fatalf("only %d pages collected; fixture listing broken?", len(pages))
	}

	for _, p := range pages {
		wantCode, want := fetch(t, ref, p)
		for label, srv := range others {
			code, got := fetch(t, srv, p)
			if code != wantCode || got != want {
				t.Errorf("%s %s: status %d (want %d), body differs: %v", label, p, code, wantCode, got != want)
			}
		}
	}
}

// A re-packed zip is picked up without restarting the viewer.
func TestStore_PicksUpRepackedZip(t *testing.T) {
	dir := copyTree(t, fixtureEewlogRoot)
	pack(t, dir, 2011)
	if err := os.RemoveAll(filepath.Join(dir, "2011")); err != nil {
		t.Fatal(err)
	}
	srv := storeServer(t, dir)

	if _, body := fetch(t, srv, "/?year=2011&month=3"); strings.Contains(body, "day=13") {
		t.Fatal("day 13 listed before it exists")
	}

	src := filepath.Join(fixtureEewlogRoot, "2011", "03", "12", "20110312042346.1")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(dir, "2011", "03", "13", "20110312042346.1")
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, b, 0o644); err != nil {
		t.Fatal(err)
	}
	pack(t, dir, 2011)
	// Make sure the zip's modification time moves even on coarse clocks.
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(dir, "2011.zip"), future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "2011")); err != nil {
		t.Fatal(err)
	}

	if _, body := fetch(t, srv, "/?year=2011&month=3"); !strings.Contains(body, "day=13") {
		t.Error("re-packed zip not picked up: day 13 missing")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
