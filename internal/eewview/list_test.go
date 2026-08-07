package eewview

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureEewlogRoot = "testdata/eewlog"

func TestSaturate(t *testing.T) {
	cases := []struct {
		raw      string
		min, max int
		want     int
	}{
		{"", 2000, 3000, 0},        // absent param -> 0, NOT clamped to min
		{"2011", 2000, 3000, 2011}, // in range
		{"1999", 2000, 3000, 2000}, // below min -> clamped
		{"9999", 2000, 3000, 3000}, // above max -> clamped
		{"abc", 2000, 3000, 2000},  // non-numeric -> numifies to 0, then clamped to min
		{"7", 1, 12, 7},            // month in range
	}
	for _, c := range cases {
		got := saturate(c.raw, c.min, c.max)
		if got != c.want {
			t.Errorf("saturate(%q,%d,%d) = %d, want %d", c.raw, c.min, c.max, got, c.want)
		}
	}
}

func TestSplitEEWName(t *testing.T) {
	eqID, warnNum, ok := splitEEWName("20110312042346.81")
	if !ok || eqID != "20110312042346" || warnNum != 81 {
		t.Errorf("got eqID=%q warnNum=%d ok=%v", eqID, warnNum, ok)
	}
	if _, _, ok := splitEEWName("not-a-telegram"); ok {
		t.Error("expected ok=false for a non-matching name")
	}
}

func TestLessEEWFile_NumericWithinSameEqID(t *testing.T) {
	// "20110312042346.10" sorts before "20110312042346.9" under a plain
	// string compare (lexical '1' < '9'), but warn_num 9 < 10 numerically
	// — this is exactly the case index.pl's custom sort exists to handle.
	if !lessEEWFile("20110312042346.9", "20110312042346.10") {
		t.Error("expected .9 to sort before .10 within the same eq_id (numeric compare)")
	}
	if lessEEWFile("20110312042346.10", "20110312042346.9") {
		t.Error("expected .10 to NOT sort before .9 within the same eq_id")
	}
}

func TestLessEEWFile_DifferentEqIDFallsBackToStringCompare(t *testing.T) {
	if !lessEEWFile("20110312042346.1", "20110312134730.3") {
		t.Error("expected different eq_ids to fall back to plain string comparison")
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := Config{
		DataDir:    fixtureEewlogRoot,
		PathBase:   "./",
		ViewerPath: "eew-show",
	}
	handler, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return httptest.NewServer(handler)
}

func TestListHandler_YearDrilldown(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	body := readBody(t, resp)
	if !strings.Contains(body, "year=2011") {
		t.Errorf("expected a link to year 2011, got body: %s", body)
	}
}

func TestListHandler_DayListingSortAndSummary(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/?year=2011&month=3&day=12")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)

	idx9 := strings.Index(body, "20110312042346.9")
	idx10 := strings.Index(body, "20110312042346.10")
	if idx9 == -1 || idx10 == -1 {
		t.Fatalf("expected both .9 and .10 links in body: %s", body)
	}
	if idx9 > idx10 {
		t.Errorf(".9 should be listed before .10 (numeric sort), got .9 at %d, .10 at %d", idx9, idx10)
	}

	if !strings.Contains(body, "第81報(最終)") {
		t.Errorf("expected the final-report summary to appear, got: %s", body)
	}
	if !strings.Contains(body, "取り消し") {
		t.Errorf("expected the cancellation summary to appear, got: %s", body)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return string(b)
}
