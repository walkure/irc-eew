package eewview

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walkure/irc-eew/internal/dmdatasim"
)

const chibaDay = "/?year=2026&month=11&day=12"

// The four 2026-11-12 千葉県北東部 reports received live in the shadow
// stack, stored exactly as DMDATA delivered them.
func TestDMDATA_DayListingFromRealArchive(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, chibaDay)
	want := []string{
		"22:27:35第01報　　　　 (22:27:19発生)震央:N35.8/E140.7(千葉県北東部)深さ50km 最大:M4.1 震度2",
		"22:27:36第02報　　　　 (22:27:19発生)震央:N35.8/E140.7(千葉県北東部)深さ40km 最大:M4.1 震度3",
		"22:27:56第03報　　　　 (22:27:19発生)震央:N35.8/E140.7(千葉県北東部)深さ50km 最大:M4.1 震度2",
		"22:28:04第04報(最終) (22:27:19発生)震央:N35.8/E140.7(千葉県北東部)深さ50km 最大:M4.1 震度2",
	}
	last := -1
	for _, w := range want {
		i := strings.Index(body, w)
		if i < 0 {
			t.Fatalf("missing %q in listing:\n%s", w, body)
		}
		if i < last {
			t.Errorf("%q out of order", w)
		}
		last = i
	}
	if !strings.Contains(body, `href="./eew-show?name=20261112222726.1_20261112222735.390_`) {
		t.Errorf("expected detail links by file name, body: %s", body)
	}
}

func TestDMDATA_DetailFromRealArchive(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := get(t, srv, chibaDay)
	i := strings.Index(body, "eew-show?name=20261112222726.4_")
	if i < 0 {
		t.Fatalf("no link to report 4: %s", body)
	}
	name := body[i+len("eew-show?name=") : i+strings.Index(body[i:], `"`)]
	page := get(t, srv, "/eew-show?name="+name)

	for _, w := range []string{
		"<title>2026/11/12 22:28:04 第4報(最終) (2026/11/12 22:27:19発生)震央:N35.8/E140.7(千葉県北東部)深さ50km 最大:M4.1 震度2</title>",
		"地震ID</td><td>20261112222726",
		"通知種類</td><td>通常",
		"通知内容</td><td>緊急地震速報（地震動予報）",
		"通知対象</td><td>高度利用者向け",
		"報数</td><td>第4報(最終)",
		"発生日時</td><td>2026/11/12 22:27:19",
		"検知日時</td><td>",
		"通知日時</td><td>2026/11/12 22:28:04",
		"震源深度</td><td>50km(",
		"マグニチュード</td><td>M4.1(",
		"最大震度</td><td>2",
		"maps.google.com/maps?output=embed",
	} {
		if !strings.Contains(page, w) {
			t.Errorf("detail page missing %q:\n%s", w, page)
		}
	}
	if strings.Contains(page, "<pre>") || strings.Contains(page, "H4sI") {
		t.Errorf("raw data should not be shown")
	}
}

// writeDMDATA stores a VXSE45 XML fixture the way the notifier archives a
// DMDATA delivery.
func writeDMDATA(t *testing.T, dataDir, xmlFixture, name string, edit func(string) string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "eew", "testdata", xmlFixture))
	if err != nil {
		t.Fatal(err)
	}
	x := string(b)
	if edit != nil {
		x = edit(x)
	}
	dir := filepath.Join(dataDir, name[0:4], name[4:6], name[6:8])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := dmdatasim.DataMessage(dmdatasim.Telegram{Body: []byte(x)})
	if err := os.WriteFile(filepath.Join(dir, name+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func serverFor(t *testing.T, dataDir string) *httptest.Server {
	t.Helper()
	h, err := NewServer(Config{DataDir: dataDir, PathBase: "./", ViewerPath: "eew-show"})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(h)
}

func get(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	return readBody(t, resp)
}

// JMA's 豊後水道 sample series: a warning report with per-area forecasts
// and long-period ground motion, then a cancellation that repeats Serial
// 32 and carries no origin time.
func TestDMDATA_SampleWarningAreasAndCancellation(t *testing.T) {
	dir := t.TempDir()
	writeDMDATA(t, dir, "jma_sample_77_01_04.xml", "20240417231454.4_20240417231459.100_aa01", nil)
	writeDMDATA(t, dir, "jma_sample_77_01_29.xml", "20240417231454.32_20240417231650.100_aa32",
		func(s string) string { return strings.Replace(s, "<Serial>29</Serial>", "<Serial>32</Serial>", 1) })
	writeDMDATA(t, dir, "jma_sample_77_01_33.xml", "20240417231454.32_20240417231700.100_aa33", nil)
	if err := os.WriteFile(filepath.Join(dir, "2024", "04", "17", "20240417231454.33_20240417231800.100_aa99.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := serverFor(t, dir)
	defer srv.Close()

	list := get(t, srv, "/?year=2024&month=4&day=17")
	for _, w := range []string{
		"23:14:59第04報　　　　 (23:14:48発生)震央:N33.2/E132.4(豊後水道)深さ30km 最大:M5.8 震度5弱 (地域予想震度情報あり)",
		// The cancellation shows its earthquake's origin time (from report 32).
		"23:17:00第32報　　　　 (23:14:47発生) 取り消し",
		"第33報 (解析できない電文)",
	} {
		if !strings.Contains(list, w) {
			t.Errorf("listing missing %q:\n%s", w, list)
		}
	}

	page := get(t, srv, "/eew-show?name=20240417231454.4_20240417231459.100_aa01")
	for _, w := range []string{
		"発信官署</td><td>気象庁本庁",
		"内陸/海域</td><td>海域",
		"震源深度</td><td>30km(IPF法(5点以上))",
		"マグニチュード</td><td>M5.8(P相/全相混在)",
		"最大震度</td><td>5弱",
		"最大長周期地震動階級</td><td>1",
		"予測の変化</td><td>震度ほとんど変化なし、長周期地震動階級1以上大きくなった",
		"付加文</td><td>強い揺れに警戒してください。",
		"<td>地域</td><td>区分</td><td>予測震度</td><td>長周期地震動階級</td><td>予想時刻</td>",
		"<td>愛媛県南予</td><td>警報</td><td>震度5弱</td><td>1未満</td><td>既に到達</td>",
		"<td>広島県南西部</td><td>警報</td><td>震度3～震度4</td><td>1未満</td><td>23:15:19</td>",
	} {
		if !strings.Contains(page, w) {
			t.Errorf("detail page missing %q", w)
		}
	}

	cancel := get(t, srv, "/eew-show?name=20240417231454.32_20240417231700.100_aa33")
	for _, w := range []string{
		"通知種類</td><td>取り消し",
		"報数</td><td>第32報",
		"発生日時</td><td>2024/04/17 23:14:47",
		"本文</td><td>先ほどの、緊急地震速報（地震動予報）を取り消します。",
	} {
		if !strings.Contains(cancel, w) {
			t.Errorf("cancellation page missing %q:\n%s", w, cancel)
		}
	}
	if strings.Contains(cancel, "震源深度") || strings.Contains(cancel, "output=embed") {
		t.Error("cancellation should have no hypocenter rows or map")
	}
}

// A legacy telegram and DMDATA reports on the same day share one listing,
// ordered by earthquake.
func TestDMDATA_MixedWithLegacySameDay(t *testing.T) {
	dir := t.TempDir()
	legacy, err := os.ReadFile(filepath.Join(fixtureDir, "20110312042346.1"))
	if err != nil {
		t.Fatal(err)
	}
	day := filepath.Join(dir, "2011", "03", "12")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(day, "20110312042346.1"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	writeDMDATA(t, dir, "real_20261112113529_01.xml", "20110312000000.1_20110312000010.000_bb01",
		func(s string) string {
			return strings.Replace(s, "<EventID>20261112113529</EventID>", "<EventID>20110312000000</EventID>", 1)
		})
	srv := serverFor(t, dir)
	defer srv.Close()

	list := get(t, srv, "/?year=2011&month=3&day=12")
	a := strings.Index(list, "(石垣島近海)")
	b := strings.Index(list, "(宮城県北部)")
	if a < 0 || b < 0 || a > b {
		t.Errorf("expected both, the earlier EventID first:\n%s", list)
	}
	if !strings.Contains(get(t, srv, "/?year=2011&month=3"), "12日") {
		t.Error("day link missing")
	}
}

func TestValidName(t *testing.T) {
	ok := []string{
		"20110312042346.81",
		"20261112222726.4_20261112222804.617_8fe58332b4ca",
		"20261112222804.617_8fe58332b4ca",
		"20261112222804.617_x0123",
	}
	for _, n := range ok {
		if !validName(n) {
			t.Errorf("%q rejected", n)
		}
	}
	bad := []string{"", "../../etc", "20261112222726.4_20261112222804.617_../x", "20261112222726.4_2026.1_ab", "x.json"}
	for _, n := range bad {
		if validName(n) {
			t.Errorf("%q accepted", n)
		}
	}
}

// 2026-11-13 熊本県熊本地方, report 1 as received live: a PLUM-only
// 仮定震源要素 report whose depth 10km / M1.0 are placeholders.
func TestDMDATA_AssumedHypocenterFromRealArchive(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	list := get(t, srv, "/?year=2026&month=11&day=13")
	want := "10:52:29第01報　　　　 (10:52:24発生)震央:N32.5/E130.6(熊本県熊本地方)深さ仮定 最大:M仮定 震度3程度以上"
	if !strings.Contains(list, want) {
		t.Errorf("listing missing %q:\n%s", want, list)
	}
}
