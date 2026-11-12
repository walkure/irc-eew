package eew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parseFile(t *testing.T, name string) *Report {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseVXSE45(b)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("%s: unexpected warnings %v", name, r.Warnings)
	}
	return r
}

func TestParseVXSE45_RealFirstReport(t *testing.T) {
	r := parseFile(t, "real_20261112113529_01.xml")
	jst := time.FixedZone("", 9*3600)
	if r.Status != "通常" || r.EventID != "20261112113529" || r.Serial != "1" || r.InfoType != Issued || r.Final {
		t.Errorf("head: %+v", r)
	}
	if !r.ReportTime.Equal(time.Date(2026, 11, 12, 11, 35, 54, 0, jst)) {
		t.Errorf("ReportTime = %v", r.ReportTime)
	}
	if r.OriginTime == nil || !r.OriginTime.Equal(time.Date(2026, 11, 12, 11, 35, 21, 0, jst)) {
		t.Errorf("OriginTime = %v", r.OriginTime)
	}
	if r.HypocenterName != "石垣島近海" || *r.Lat != 23.7 || *r.Lon != 123.8 {
		t.Errorf("hypocenter: %q %v %v", r.HypocenterName, *r.Lat, *r.Lon)
	}
	if r.Depth != (Depth{Kind: DepthKm, Km: 20}) || r.Magnitude == nil || *r.Magnitude != 3.5 {
		t.Errorf("depth/M: %+v %v", r.Depth, r.Magnitude)
	}
	if r.Intensity != (Intensity{Present: true, From: "1", To: "1"}) {
		t.Errorf("intensity: %+v", r.Intensity)
	}
	if r.Event() != (Event{EventID: "20261112113529"}) {
		t.Errorf("event: %+v", r.Event())
	}
}

func TestParseVXSE45_RealFinalReports(t *testing.T) {
	for _, name := range []string{"real_20261112113529_04.xml", "real_20261112130734_06.xml"} {
		r := parseFile(t, name)
		if !r.Final || !r.Event().Final {
			t.Errorf("%s: not detected as final", name)
		}
	}
	for _, name := range []string{"real_20261112113529_03.xml", "real_20261112130734_05.xml"} {
		if r := parseFile(t, name); r.Final {
			t.Errorf("%s: wrongly detected as final", name)
		}
	}
}

// rank2="9" (精度が最終報相当) shows up before the series ends in JMA's
// own sample; it must not be mistaken for a final report.
func TestParseVXSE45_Rank2NineIsNotFinal(t *testing.T) {
	if r := parseFile(t, "jma_sample_77_01_29.xml"); r.Final {
		t.Error("rank2=9 report treated as final")
	}
}

func TestParseVXSE45_JMASampleCancellation(t *testing.T) {
	r := parseFile(t, "jma_sample_77_01_33.xml")
	if r.InfoType != Cancelled || !r.Event().Cancelled || r.Serial != "32" || r.EventID != "20240417231454" {
		t.Errorf("cancellation: %+v", r)
	}
	if r.OriginTime != nil || r.Lat != nil || r.Magnitude != nil || r.Intensity.Present || r.Depth.Kind != DepthUnknown {
		t.Errorf("cancellation should carry no earthquake elements: %+v", r)
	}
}

func TestParseVXSE45_JMASampleIntensity(t *testing.T) {
	if r := parseFile(t, "jma_sample_77_01_04.xml"); r.Intensity != (Intensity{Present: true, From: "5-", To: "5-"}) {
		t.Errorf("intensity: %+v", r.Intensity)
	}
}

// synth builds a minimal VXSE45 document around the given Head extras and
// Body content.
func synth(headExtra, body string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Report xmlns="http://xml.kishou.go.jp/jmaxml1/">
<Control><Title>緊急地震速報（地震動予報）</Title><Status>通常</Status></Control>
<Head xmlns="http://xml.kishou.go.jp/jmaxml1/informationBasis1/"><ReportDateTime>2026-11-12T11:35:54+09:00</ReportDateTime><EventID>20261112113529</EventID>` + headExtra + `</Head>
<Body xmlns="http://xml.kishou.go.jp/jmaxml1/body/seismology1/" xmlns:jmx_eb="http://xml.kishou.go.jp/jmaxml1/elementBasis1/">` + body + `</Body>
</Report>`)
}

func eq(coord, mag string) string {
	return `<Earthquake><OriginTime>2026-11-12T11:35:21+09:00</OriginTime><ArrivalTime>2026-11-12T11:35:29+09:00</ArrivalTime>` +
		`<Hypocenter><Area><Name>千島列島</Name>` + coord + `</Area></Hypocenter>` + mag + `</Earthquake>`
}

func mustParse(t *testing.T, b []byte) *Report {
	t.Helper()
	r, err := ParseVXSE45(b)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Stray line breaks / blanks inside values (as suspected in the
// 2026-10-16 docomo area-mail incident) must not change any decision.
func TestParseVXSE45_StrayWhitespaceIsNormalized(t *testing.T) {
	r := mustParse(t, synth(
		"<InfoType>\n取消\r\n</InfoType><Serial> 3\n</Serial>",
		"<NextAdvisory>この情報をもって、\n緊急地震速報：最終報とします。\r\n</NextAdvisory>"+
			`<Earthquake><Hypocenter><Area><Name>千島列島
</Name><jmx_eb:Coordinate>
+45.0+150.0-10000/
</jmx_eb:Coordinate></Area></Hypocenter><jmx_eb:Magnitude type="Mj" condition=" 不明
">NaN</jmx_eb:Magnitude></Earthquake>`+
			"<Intensity><Forecast><ForecastInt><From> 5-\n</From><To>\nover</To></ForecastInt></Forecast></Intensity>"))
	if len(r.Warnings) != 0 {
		t.Errorf("warnings: %v", r.Warnings)
	}
	if r.InfoType != Cancelled || r.Serial != "3" || !r.Final {
		t.Errorf("head/final: %+v", r)
	}
	if r.HypocenterName != "千島列島" {
		t.Errorf("name = %q", r.HypocenterName)
	}
	if r.Lat == nil || *r.Lat != 45.0 || r.Depth != (Depth{Kind: DepthKm, Km: 10}) {
		t.Errorf("coordinate: %v %+v", r.Lat, r.Depth)
	}
	if r.Magnitude != nil {
		t.Errorf("magnitude should be unknown: %v", *r.Magnitude)
	}
	if r.Intensity != (Intensity{Present: true, From: "5-", To: "over"}) {
		t.Errorf("intensity: %+v", r.Intensity)
	}
}

func TestParseVXSE45_NextAdvisoryWithOtherTextIsNotFinal(t *testing.T) {
	r := mustParse(t, synth("<InfoType>発表</InfoType><Serial>1</Serial>", "<NextAdvisory>次の情報は未定です。</NextAdvisory>"))
	if r.Final || len(r.Warnings) != 1 {
		t.Errorf("final=%v warnings=%v", r.Final, r.Warnings)
	}
}

func TestParseVXSE45_CoordinateForms(t *testing.T) {
	cases := []struct {
		name      string
		coord     string
		wantLat   bool
		wantDepth Depth
	}{
		{"normal", `<jmx_eb:Coordinate>+39.0+140.9-10000/</jmx_eb:Coordinate>`, true, Depth{Kind: DepthKm, Km: 10}},
		{"shallow", `<jmx_eb:Coordinate>+39.0+140.9+0/</jmx_eb:Coordinate>`, true, Depth{Kind: DepthShallow}},
		{"700plus", `<jmx_eb:Coordinate>+39.0+140.9-700000/</jmx_eb:Coordinate>`, true, Depth{Kind: Depth700Plus}},
		{"depth unknown", `<jmx_eb:Coordinate>+39.0+140.9/</jmx_eb:Coordinate>`, true, Depth{Kind: DepthUnknown}},
		{"all unknown", `<jmx_eb:Coordinate description="震源要素不明" />`, false, Depth{Kind: DepthUnknown}},
	}
	for _, c := range cases {
		r := mustParse(t, synth("<InfoType>発表</InfoType><Serial>1</Serial>", eq(c.coord, "")))
		if (r.Lat != nil) != c.wantLat || r.Depth != c.wantDepth || len(r.Warnings) != 0 {
			t.Errorf("%s: lat=%v depth=%+v warnings=%v", c.name, r.Lat, r.Depth, r.Warnings)
		}
	}
}

func TestParseVXSE45_UnknownValuesWarnButDoNotFail(t *testing.T) {
	r := mustParse(t, synth("<InfoType>謎</InfoType><Serial>1</Serial>",
		eq(`<jmx_eb:Coordinate>北緯</jmx_eb:Coordinate>`, `<jmx_eb:Magnitude type="Mj">abc</jmx_eb:Magnitude>`)+
			"<Intensity><Forecast><ForecastInt><From>５弱</From><To>5-</To></ForecastInt></Forecast></Intensity>"))
	if r.InfoType != Issued || r.Lat != nil || r.Magnitude != nil || r.Intensity.From != "不明" {
		t.Errorf("report: %+v", r)
	}
	// InfoType, Coordinate, Magnitude and the full-width 5弱 (no NFKC).
	if len(r.Warnings) != 4 {
		t.Errorf("warnings = %d %v", len(r.Warnings), r.Warnings)
	}
}

func TestParseVXSE45_NoIntensityAndAssumedHypocenter(t *testing.T) {
	r := mustParse(t, synth("<InfoType>発表</InfoType><Serial>1</Serial>",
		`<Earthquake><OriginTime>2016-04-14T21:26:36+09:00</OriginTime><Condition>仮定震源要素</Condition><ArrivalTime>2016-04-14T21:26:38+09:00</ArrivalTime>`+
			`<Hypocenter><Area><Name>熊本県熊本地方</Name><jmx_eb:Coordinate>+32.8+130.7-10000/</jmx_eb:Coordinate></Area></Hypocenter>`+
			`<jmx_eb:Magnitude type="Mj">1.0</jmx_eb:Magnitude></Earthquake>`))
	if r.Intensity.Present || !r.AssumedHypocenter || r.Magnitude == nil || *r.Magnitude != 1.0 {
		t.Errorf("report: %+v", r)
	}
}

func TestParseVXSE45_Malformed(t *testing.T) {
	if _, err := ParseVXSE45([]byte("<Report><unclosed>")); err == nil {
		t.Error("expected an error for malformed XML")
	}
}

func TestNormalize(t *testing.T) {
	if got := Key(" 5-\r\n　"); got != "5-" {
		t.Errorf("Key = %q", got)
	}
	if got := Display("\n１４日１９時４１分１５秒頃\r\n千葉県銚子市付近　最大震度５弱\n "); got != "１４日１９時４１分１５秒頃 千葉県銚子市付近　最大震度５弱" {
		t.Errorf("Display = %q", got)
	}
	if got := StripLineBreaks("a\r\nb\nc"); got != "abc" {
		t.Errorf("StripLineBreaks = %q", got)
	}
	if !strings.Contains(FinalReportText, "最終報") {
		t.Error("FinalReportText changed unexpectedly")
	}
}

func TestOriginCache_PerEventIDAndBounded(t *testing.T) {
	c := NewOriginCache(2)
	at := func(sec int) *time.Time { v := time.Date(2026, 1, 1, 0, 0, sec, 0, time.UTC); return &v }

	c.Observe(&Report{EventID: "A", OriginTime: at(1)})
	c.Observe(&Report{EventID: "B", OriginTime: at(2)})
	c.Observe(&Report{EventID: "A", OriginTime: at(3)}) // later report updates A
	if got := c.Observe(&Report{EventID: "A"}); got == nil || !got.Equal(*at(3)) {
		t.Errorf("A fallback = %v, want the latest origin", got)
	}
	if got := c.Observe(&Report{EventID: "B"}); got == nil || !got.Equal(*at(2)) {
		t.Errorf("B fallback = %v (interleaved quakes must not mix)", got)
	}
	c.Observe(&Report{EventID: "C", OriginTime: at(4)}) // evicts the least recent (A)
	if got := c.Observe(&Report{EventID: "A"}); got != nil {
		t.Errorf("A should have been forgotten, got %v", got)
	}
	if got := c.Observe(&Report{EventID: "D"}); got != nil {
		t.Errorf("unknown EventID fallback = %v, want nil", got)
	}
}

func TestRecentSet(t *testing.T) {
	s := NewRecentSet(2)
	if !s.Add("a") || !s.Add("b") || s.Add("a") {
		t.Fatal("basic add/dedupe failed")
	}
	// "a" was refreshed, so adding "c" evicts "b".
	if !s.Add("c") || s.Add("a") || !s.Add("b") {
		t.Error("LRU eviction order wrong")
	}
}

// The richer elements the viewer shows, from JMA's 豊後水道 sample report 4
// (a warning with per-area forecasts and long-period ground motion).
func TestParseVXSE45_ViewerFields(t *testing.T) {
	r := parseFile(t, "jma_sample_77_01_04.xml")
	if r.Title != "緊急地震速報（地震動予報）" || r.EditorialOffice != "気象庁本庁" || r.LandOrSea != "海域" {
		t.Errorf("control/hypocenter: %q %q %q", r.Title, r.EditorialOffice, r.LandOrSea)
	}
	if r.Accuracy != (Accuracy{Epicenter: "4", Epicenter2: "4", Depth: "4", Magnitude: "4", MagnitudeStations: "5"}) {
		t.Errorf("accuracy: %+v", r.Accuracy)
	}
	if r.LgIntensity != (Intensity{Present: true, From: "1", To: "1"}) {
		t.Errorf("lg intensity: %+v", r.LgIntensity)
	}
	if r.Change != (Change{Present: true, MaxInt: "0", MaxLgInt: "1", Reason: "0"}) {
		t.Errorf("change: %+v", r.Change)
	}
	if r.Comment != "強い揺れに警戒してください。" {
		t.Errorf("comment: %q", r.Comment)
	}
	if len(r.Areas) != 9 {
		t.Fatalf("areas: %d", len(r.Areas))
	}
	first := r.Areas[0]
	if first.Pref != "愛媛" || first.Name != "愛媛県南予" || first.KindName != "緊急地震速報（警報）" ||
		first.Intensity != (Intensity{Present: true, From: "5-", To: "5-"}) || !first.Arrived || first.ArrivalTime != nil {
		t.Errorf("area[0]: %+v", first)
	}
	last := r.Areas[8]
	if last.Name != "広島県南西部" || last.Intensity.From != "3" || last.Intensity.To != "4" || last.Arrived ||
		last.ArrivalTime == nil || last.ArrivalTime.Format("15:04:05") != "23:15:19" {
		t.Errorf("area[8]: %+v", last)
	}
}

func TestParseVXSE45_CancellationText(t *testing.T) {
	if r := parseFile(t, "jma_sample_77_01_33.xml"); r.Text != "先ほどの、緊急地震速報（地震動予報）を取り消します。" {
		t.Errorf("text: %q", r.Text)
	}
}
