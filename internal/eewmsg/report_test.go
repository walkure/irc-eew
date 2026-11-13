package eewmsg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walkure/irc-eew/internal/eew"
)

func parseFixture(t *testing.T, name string) *eew.Report {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "eew", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	r, err := eew.ParseVXSE45(b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFormatReport_RealReports(t *testing.T) {
	cases := map[string]string{
		"real_20261112113529_01.xml": "2026/11/12 11:35:54 第1報 (2026/11/12 11:35:21発生)" +
			"震央:<http://maps.google.com/maps?q=23.7,123.8|N23.7/E123.8>(石垣島近海)深さ20km 最大:M3.5 震度1",
		"real_20261112113529_04.xml": "2026/11/12 11:36:32 第4報(最終) (2026/11/12 11:35:21発生)" +
			"震央:<http://maps.google.com/maps?q=23.7,123.8|N23.7/E123.8>(石垣島近海)深さ20km 最大:M3.9 震度2",
		"real_20261112130734_01.xml": "2026/11/12 13:07:41 第1報 (2026/11/12 13:07:30発生)" +
			"震央:<http://maps.google.com/maps?q=40.3,143.8|N40.3/E143.8>(三陸沖)深さ10km 最大:M4.2 震度1",
	}
	for name, want := range cases {
		text, title := FormatReport(parseFixture(t, name), nil)
		if text != want {
			t.Errorf("%s:\n got  %q\n want %q", name, text, want)
		}
		if title != "高度利用者向け緊急地震速報" {
			t.Errorf("%s: title %q", name, title)
		}
	}
}

func TestFormatReport_CancellationUsesFallbackOriginAndXMLSerial(t *testing.T) {
	r := parseFixture(t, "jma_sample_77_01_33.xml")
	origin := time.Date(2024, 4, 17, 23, 14, 50, 0, time.FixedZone("", 9*3600))

	text, _ := FormatReport(r, &origin)
	if want := "2024/04/17 23:17:00 第32報 (2024/04/17 23:14:50発生) 取り消されました"; text != want {
		t.Errorf("with fallback:\n got  %q\n want %q", text, want)
	}
	text, _ = FormatReport(r, nil)
	if want := "2024/04/17 23:17:00 第32報 取り消されました"; text != want {
		t.Errorf("without fallback:\n got  %q\n want %q", text, want)
	}
}

func TestFormatReport_UnknownsAndRanges(t *testing.T) {
	jst := time.FixedZone("", 9*3600)
	rt := time.Date(2026, 1, 2, 3, 4, 5, 0, jst)
	f := func(v float64) *float64 { return &v }
	base := func() *eew.Report {
		return &eew.Report{EventID: "x", Serial: "2", ReportTime: rt, HypocenterName: "千島列島",
			Lat: f(45.0), Lon: f(150.0), Depth: eew.Depth{Kind: eew.DepthKm, Km: 10}, Magnitude: f(6.5),
			Intensity: eew.Intensity{Present: true, From: "4", To: "4"}}
	}
	cases := []struct {
		name   string
		mutate func(r *eew.Report)
		want   string // substring after the serial
	}{
		{"no origin", func(r *eew.Report) {}, "第2報 震央:<http://maps.google.com/maps?q=45.0,150.0|N45.0/E150.0>(千島列島)深さ10km 最大:M6.5 震度4"},
		{"unknown position", func(r *eew.Report) { r.Lat, r.Lon = nil, nil }, "震央:不明(千島列島)深さ10km"},
		{"unknown name", func(r *eew.Report) { r.HypocenterName = "" }, "N45.0/E150.0>(不明)"},
		{"depth unknown", func(r *eew.Report) { r.Depth = eew.Depth{} }, ")深さ不明 最大"},
		{"shallow", func(r *eew.Report) { r.Depth = eew.Depth{Kind: eew.DepthShallow} }, ")深さごく浅い 最大"},
		{"700+", func(r *eew.Report) { r.Depth = eew.Depth{Kind: eew.Depth700Plus} }, ")深さ700km以上 最大"},
		{"M unknown", func(r *eew.Report) { r.Magnitude = nil }, "最大:M不明 震度4"},
		{"over", func(r *eew.Report) { r.Intensity = eew.Intensity{Present: true, From: "5-", To: "over"} }, "震度5弱程度以上"},
		{"range", func(r *eew.Report) { r.Intensity = eew.Intensity{Present: true, From: "4", To: "5+"} }, "震度4から5強程度"},
		{"six strong", func(r *eew.Report) { r.Intensity = eew.Intensity{Present: true, From: "6+", To: "6+"} }, "震度6強"},
		{"zero", func(r *eew.Report) { r.Intensity = eew.Intensity{Present: true, From: "0", To: "0"} }, "震度0"},
		{"intensity unknown", func(r *eew.Report) { r.Intensity = eew.Intensity{Present: true, From: "不明", To: "不明"} }, "震度不明"},
		{"intensity absent", func(r *eew.Report) { r.Intensity = eew.Intensity{} }, "震度不明"},
		{"final", func(r *eew.Report) { r.Final = true }, "第2報(最終) 震央:"},
		{"corrected", func(r *eew.Report) { r.InfoType = eew.Corrected }, "第2報(訂正) 震央:"},
		{"assumed", func(r *eew.Report) { r.AssumedHypocenter = true }, "震央:<http://maps.google.com/maps?q=45.0,150.0|N45.0/E150.0>(千島列島)深さ仮定 最大:M仮定 震度4"},
		{"corrected final", func(r *eew.Report) { r.Final = true; r.InfoType = eew.Corrected }, "第2報(最終)(訂正) 震央:"},
	}
	for _, c := range cases {
		r := base()
		c.mutate(r)
		text, _ := FormatReport(r, nil)
		if !strings.HasPrefix(text, "2026/01/02 03:04:05 ") || !strings.Contains(text, c.want) {
			t.Errorf("%s: %q does not contain %q", c.name, text, c.want)
		}
	}
}

func TestFormatReport_NeverContainsLineBreaks(t *testing.T) {
	f := 1.0
	r := &eew.Report{Serial: "1\n", HypocenterName: "千島\r\n列島", Lat: &f, Lon: &f}
	text, _ := FormatReport(r, nil)
	if strings.ContainsAny(text, "\r\n") {
		t.Errorf("line break leaked: %q", text)
	}
}
