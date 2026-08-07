package eewview

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestDetailHandler_FieldsAndAccuracyLabelFix(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/eew-show?name=20110312042346.1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)

	// RK33433: center=3 depth=3 magnitude=4 — center/depth share a label
	// text ("グリッドサーチ法(3点/4点)") but magnitude's ("P相/全相混在") is
	// distinct, so this fixture would catch a regression of the Perl
	// eew-show.pl bug that printed center_accurate on the depth row.
	if !strings.Contains(body, "震源深度</td><td>10km(グリッドサーチ法(3点/4点))") {
		t.Errorf("expected depth row to show its own DepthAccurate label, body: %s", body)
	}
	if !strings.Contains(body, "マグニチュード</td><td>M6.8(P相/全相混在)") {
		t.Errorf("expected magnitude row to show MagnitudeAccurate, body: %s", body)
	}

	// Date-bug fix: eq_time's date must not borrow warn_time's day.
	if !strings.Contains(body, "検知日時</td><td>2011/03/12 04:23:48") {
		t.Errorf("expected correct eq-occurrence date, body: %s", body)
	}
	if !strings.Contains(body, "通知日時</td><td>2011/03/12 04:24:50") {
		t.Errorf("expected correct warn-report date, body: %s", body)
	}

	if !strings.Contains(body, "地震ID</td><td>20110312042346") {
		t.Errorf("expected eq_id field, body: %s", body)
	}
	if !strings.Contains(body, "maps.google.com") {
		t.Errorf("expected a Google Maps link, body: %s", body)
	}
}

func TestDetailHandler_EBITable(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/eew-show?name=20110312042346.1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)

	if !strings.Contains(body, "<td>地域</td><td>予測震度</td><td>予想時刻</td>") {
		t.Errorf("expected the EBI table header, body: %s", body)
	}
	if !strings.Contains(body, "宮城県中部") {
		t.Errorf("expected an EBI area name (宮城県中部, area code 222), body: %s", body)
	}
}

func TestDetailHandler_RawDumpSJISAndControlChars(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/eew-show?name=20110312042346.1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)

	if !strings.Contains(body, "ﾅｳｷﾔｽﾄ") {
		t.Errorf("expected the SJIS-decoded preamble to render correctly (not mojibake), body: %s", body)
	}
	if !strings.Contains(body, "[SOH]") || !strings.Contains(body, "[STX]") {
		t.Errorf("expected control-character markers in the raw dump, body: %s", body)
	}
	if strings.ContainsRune(body, '\x01') || strings.ContainsRune(body, '\x02') {
		t.Errorf("raw control bytes should have been replaced, not passed through, body: %s", body)
	}
}

func TestDetailHandler_Cancellation_NoHypocenterFields(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/eew-show?name=20110312134730.3")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)

	if strings.Contains(body, "震源深度") {
		t.Errorf("cancellation telegrams have no hypocenter, should not show 震源深度, body: %s", body)
	}
	if !strings.Contains(body, "取り消し") {
		t.Errorf("expected cancellation text in the title/summary, body: %s", body)
	}
}

func TestDetailHandler_InvalidName(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	cases := []string{
		"",
		"not-a-name",
		"../../../etc/passwd",
		"20110312042346.1; DROP TABLE",
	}
	for _, name := range cases {
		resp, err := http.Get(srv.URL + "/eew-show?name=" + url.QueryEscape(name))
		if err != nil {
			t.Fatalf("GET(%q): %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("name=%q: got status %d, want %d", name, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

func TestDetailHandler_UnknownFile(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/eew-show?name=99999999999999.1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got status %d, want 404", resp.StatusCode)
	}
}

func TestTelegramPath_MatchesArchiveLayout(t *testing.T) {
	got, err := telegramPath("/data", "20110312042346.81")
	if err != nil {
		t.Fatalf("telegramPath: %v", err)
	}
	want := "\\data\\2011\\03\\12\\20110312042346.81"
	if !strings.HasSuffix(got, "2011\\03\\12\\20110312042346.81") && !strings.HasSuffix(got, "2011/03/12/20110312042346.81") {
		t.Errorf("telegramPath: got %q, want suffix matching %q", got, want)
	}
}
