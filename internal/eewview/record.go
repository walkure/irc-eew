package eewview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/walkure/irc-eew/internal/decoder"
	"github.com/walkure/irc-eew/internal/eew"
)

// record is one archived report, legacy WNI code telegram or DMDATA
// VXSE45 alike, already reduced to what the pages display. Everything is
// rendered from records, so a browser user sees the same presentation for
// both archives; values that are unknown read 不明, and values only VXSE45
// carries are simply absent ("") for legacy telegrams.
type record struct {
	// Name is the detail page's ?name= value (and the file's base name).
	Name    string
	EventID string
	Serial  int
	// received orders reports sharing EventID and Serial (a cancellation
	// or correction repeats the Serial); zero for legacy telegrams.
	received time.Time

	Cancelled, Final, Corrected bool
	// Unparsable marks a DMDATA file that couldn't be read as VXSE45.
	Unparsable bool

	// Full ("2006/01/02 15:04:05") and clock-only ("15:04:05") renderings;
	// empty when unknown.
	ReportFull, ReportClock string
	OriginFull, OriginClock string
	// ArrivalFull is the detection time at the first station (VXSE45 only).
	ArrivalFull string

	Kind, Content, Target, Office string

	PositionKnown bool
	Lat, Lng      float64
	CenterName    string
	CenterAcc     string
	LandSea       string
	Assumed       bool
	DepthText     string
	DepthAcc      string
	MagText       string
	MagAcc        string
	MaxIntText    string
	MaxLgIntText  string
	ChangeText    string
	Comment, Text string

	Areas []areaRow
}

type areaRow struct {
	Code, Name, Kind, Shindo, LgInt, Time string
}

// ---- legacy WNI code telegrams ----

// legacyRecord adapts a decoded legacy telegram (file name "<eq_id>.<n>").
func legacyRecord(name string, t *decoder.Telegram) *record {
	r := &record{
		Name:        name,
		EventID:     t.EqID,
		Serial:      t.WarnNum,
		Cancelled:   t.IsCancellation(),
		Final:       isFinalForViewer(t),
		ReportFull:  fullDateTime(t.WarnTime),
		ReportClock: timeOnly(t.WarnTime),
		Kind:        t.MsgType,
		Content:     t.CodeType,
		Target:      t.WarnType,
		Office:      t.Section,
		CenterName:  t.CenterName,
		CenterAcc:   t.CenterAccurate,
		DepthAcc:    t.DepthAccurate,
		MagAcc:      t.MagnitudeAccurate,
		MaxIntText:  "不明",
		DepthText:   "不明",
		MagText:     "M不明",
	}
	if len(t.EqTime) == 12 {
		r.OriginFull, r.OriginClock = fullDateTime(t.EqTime), timeOnly(t.EqTime)
	}
	if !t.CenterUnknown {
		r.PositionKnown, r.Lat, r.Lng = true, t.CenterLat, t.CenterLng
	}
	if !t.DepthUnknown {
		r.DepthText = fmt.Sprintf("%dkm", t.CenterDepth)
	}
	if !t.MagnitudeUnknown {
		r.MagText = fmt.Sprintf("M%.1f", t.Magnitude)
	}
	if t.Shindo != "" {
		r.MaxIntText = t.Shindo
	}
	if t.AssumedHypocenter {
		r.Assumed = true
		r.DepthText, r.MagText = "仮定", "M仮定"
	}
	switch t.IsSea {
	case "陸":
		r.LandSea = "内陸"
	case "海":
		r.LandSea = "海域"
	}

	codes := make([]string, 0, len(t.EBI))
	for c := range t.EBI {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		e := t.EBI[c]
		row := areaRow{Code: c, Name: e.Name, Kind: "予報"}
		if e.IsWarndedCode == "1" {
			row.Kind = "警報"
		}
		// Shindo1 (e1e2) is the upper bound and Shindo2 (e3e4) the lower
		// one, or "//" for "e1e2程度以上" (コード電文解説資料). Shown lower to
		// upper, like VXSE45 ranges.
		switch {
		case e.Shindo2Code == "//":
			row.Shindo = "震度" + e.Shindo1 + "以上"
		case e.Shindo1 != e.Shindo2:
			row.Shindo = "震度" + e.Shindo2 + "～震度" + e.Shindo1
		default:
			row.Shindo = "震度" + e.Shindo1
		}
		switch {
		case e.ArriveCode == "1":
			row.Time = "既に到達"
		case e.Time == "" || strings.Contains(e.Time, "/"):
			row.Time = "不明"
		default:
			p := splitPairs(e.Time)
			row.Time = fmt.Sprintf("%s:%s:%s", at(p, 0), at(p, 1), at(p, 2))
		}
		r.Areas = append(r.Areas, row)
	}
	return r
}

// ---- DMDATA VXSE45 ----

const (
	jmaTimeFull  = "2006/01/02 15:04:05"
	jmaTimeClock = "15:04:05"
)

// Accuracy labels for VXSE45's Hypocenter/Accuracy ranks, from JMA's
// 地震火山関連 解説資料 Ⅱ.21 1-4-2 (current method names; legacy telegrams
// keep their own era's labels from decoder.RK1..RK3).
var (
	epicenterRankLabel = map[string]string{
		"0": "不明",
		"1": "P波/S波レベル超え、IPF法(1点)、または仮定震源要素",
		"2": "IPF法(2点)",
		"3": "IPF法(3点/4点)",
		"4": "IPF法(5点以上)",
		"5": "防災科研システム(4点以下、または精度情報なし)",
		"6": "防災科研システム(5点以上)",
		"7": "EPOS(海域[観測網外])",
		"8": "EPOS(内陸[観測網内])",
	}
	magnitudeRankLabel = map[string]string{
		"0": "不明",
		"2": "防災科研システム",
		"3": "全点P相",
		"4": "P相/全相混在",
		"5": "全点全相",
		"6": "EPOS",
		"8": "P波/S波レベル超え、または仮定震源要素",
	}
	changeLabel = map[string]string{
		"0": "ほとんど変化なし",
		"1": "1.0以上大きくなった",
		"2": "1.0以上小さくなった",
	}
	lgChangeLabel = map[string]string{
		"0": "変化なし",
		"1": "1以上大きくなった",
		"2": "1以上小さくなった",
	}
	changeReasonLabel = map[string]string{
		"1": "主としてMが変化したため",
		"2": "主として震央位置が変化したため",
		"3": "M及び震央位置が変化したため",
		"4": "震源の深さが変化したため",
		"9": "PLUM法による予測により変化したため",
	}
)

func formatJMATime(t *time.Time) (full, clock string) {
	if t == nil {
		return "", ""
	}
	return t.Format(jmaTimeFull), t.Format(jmaTimeClock)
}

// dmdataRecord adapts a parsed VXSE45 report.
func dmdataRecord(name string, received time.Time, rep *eew.Report) *record {
	r := &record{
		Name:      name,
		EventID:   rep.EventID,
		received:  received,
		Cancelled: rep.InfoType == eew.Cancelled,
		Final:     rep.Final,
		Corrected: rep.InfoType == eew.Corrected,
		Content:   rep.Title,
		// Same audience label the legacy telegrams carried (and the
		// notifier's title keeps).
		Target:     "高度利用者向け",
		Office:     rep.EditorialOffice,
		CenterName: rep.HypocenterName,
		Assumed:    rep.AssumedHypocenter,
		Comment:    rep.Comment,
		Text:       rep.Text,
	}
	fmt.Sscanf(rep.Serial, "%d", &r.Serial)
	if !rep.ReportTime.IsZero() {
		rt := rep.ReportTime
		r.ReportFull, r.ReportClock = formatJMATime(&rt)
	}
	r.OriginFull, r.OriginClock = formatJMATime(rep.OriginTime)
	r.ArrivalFull, _ = formatJMATime(rep.ArrivalTime)

	// 通知種類, in the legacy telegrams' vocabulary (decoder.MsgType).
	training := rep.Status == "訓練"
	switch {
	case r.Cancelled && training:
		r.Kind = "訓練取り消し"
	case r.Cancelled:
		r.Kind = "取り消し"
	case training:
		r.Kind = "訓練"
	case rep.Status == "試験":
		r.Kind = "試験"
	case r.Corrected:
		r.Kind = "訂正"
	default:
		r.Kind = "通常"
	}

	if rep.Lat != nil && rep.Lon != nil {
		r.PositionKnown, r.Lat, r.Lng = true, *rep.Lat, *rep.Lon
	}
	r.CenterAcc = epicenterRankLabel[rep.Accuracy.Epicenter]
	if rep.Accuracy.Epicenter2 == "9" {
		r.CenterAcc = joinNonEmpty("、", r.CenterAcc, "震源・Mは最終報相当")
	}
	r.DepthAcc = epicenterRankLabel[rep.Accuracy.Depth]
	r.MagAcc = magnitudeRankLabel[rep.Accuracy.Magnitude]
	r.LandSea = rep.LandOrSea

	switch rep.Depth.Kind {
	case eew.DepthKm:
		r.DepthText = fmt.Sprintf("%dkm", rep.Depth.Km)
	case eew.DepthShallow:
		r.DepthText = "ごく浅い"
	case eew.Depth700Plus:
		r.DepthText = "700km以上"
	default:
		r.DepthText = "不明"
	}
	r.MagText = "M不明"
	if rep.Magnitude != nil {
		r.MagText = fmt.Sprintf("M%.1f", *rep.Magnitude)
	}
	if rep.AssumedHypocenter {
		// Placeholders (depth 10km, M1.0), shown as 仮定 like the notifier.
		r.DepthText, r.MagText = "仮定", "M仮定"
	}
	r.MaxIntText = intensityText(rep.Intensity, shindoLabel)
	if rep.LgIntensity.Present {
		r.MaxLgIntText = intensityText(rep.LgIntensity, lgLabel)
	}
	if c := rep.Change; c.Present {
		parts := []string{"震度" + labelOr(changeLabel, c.MaxInt)}
		if c.MaxLgInt != "" {
			parts = append(parts, "長周期地震動階級"+labelOr(lgChangeLabel, c.MaxLgInt))
		}
		r.ChangeText = strings.Join(parts, "、")
		if reason, ok := changeReasonLabel[c.Reason]; ok {
			r.ChangeText += "（" + reason + "）"
		}
	}

	areas := append([]eew.AreaForecast(nil), rep.Areas...)
	sort.SliceStable(areas, func(i, j int) bool { return areas[i].Code < areas[j].Code })
	for _, a := range areas {
		row := areaRow{Code: a.Code, Name: a.Name, Kind: "予報", Shindo: areaShindo(a.Intensity)}
		if strings.Contains(a.KindName, "警報") {
			row.Kind = "警報"
		}
		if a.LgIntensity.Present {
			row.LgInt = intensityText(a.LgIntensity, lgLabel)
		}
		switch {
		case a.Arrived:
			row.Time = "既に到達"
		case a.ArrivalTime != nil:
			row.Time = a.ArrivalTime.Format(jmaTimeClock)
		default:
			row.Time = "不明"
		}
		r.Areas = append(r.Areas, row)
	}
	return r
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func labelOr(m map[string]string, code string) string {
	if l, ok := m[code]; ok {
		return l
	}
	return "不明"
}

func shindoLabel(code string) string {
	switch code {
	case "5-":
		return "5弱"
	case "5+":
		return "5強"
	case "6-":
		return "6弱"
	case "6+":
		return "6強"
	}
	return code
}

func lgLabel(code string) string {
	if code == "0" {
		return "1未満"
	}
	return code
}

// intensityText renders a maximum forecast the way the notifier does:
// 6弱 / 5弱程度以上 / 4から5強程度 / 不明.
func intensityText(in eew.Intensity, label func(string) string) string {
	if !in.Present || in.From == "不明" {
		return "不明"
	}
	switch in.To {
	case in.From, "不明":
		return label(in.From)
	case "over":
		return label(in.From) + "程度以上"
	default:
		return label(in.From) + "から" + label(in.To) + "程度"
	}
}

// areaShindo renders a per-area forecast like the legacy area table.
func areaShindo(in eew.Intensity) string {
	if !in.Present || in.From == "不明" {
		return "不明"
	}
	switch in.To {
	case in.From, "不明":
		return "震度" + shindoLabel(in.From)
	case "over":
		return "震度" + shindoLabel(in.From) + "以上"
	default:
		return "震度" + shindoLabel(in.From) + "～震度" + shindoLabel(in.To)
	}
}
