package eewmsg

import (
	"fmt"
	"time"

	"github.com/walkure/irc-eew/internal/eew"
)

// Title is the Slack username/title for every EEW notification. It is the
// one the legacy WNI code telegrams always produced (WarnType "NCN" =
// 高度利用者向け), kept unchanged for VXSE45 so recipients' filters keep
// working.
const Title = "高度利用者向け緊急地震速報"

// timeLayout matches the legacy "20yy/mm/dd hh:mm:ss" rendering.
const timeLayout = "2006/01/02 15:04:05"

// FormatReport renders a VXSE45 report in the same shape Format renders a
// legacy code telegram:
//
//	{発表時刻} 第{N}報[(最終)][(訂正)] ({発生時刻}発生)震央:<地図|N{lat}/E{lon}>({震央名})深さ{D} 最大:M{M} 震度{I}
//	{発表時刻} 第{N}報 ({発生時刻}発生) 取り消されました
//
// Differences from the legacy rendering, all decided by the user:
//   - unknown values read 不明 instead of the legacy 0 / 0.0 (深さ不明,
//     M不明, and 震央:不明 without a map link for unknown coordinates);
//     ごく浅い and 700km以上 are spelled out.
//   - the forecast intensity shows the From/To range: 震度5弱程度以上
//     (To=over), 震度4から5強程度 (From≠To), 震度0 is kept distinct from
//     震度不明.
//   - the "(…発生)" part is omitted when no origin time is known.
//   - a correction (InfoType 訂正) is marked "(訂正)" after the serial.
//   - for 仮定震源要素 (PLUM-only) the placeholder depth/M read 深さ仮定 and
//     M仮定; the position (the triggering station's) is shown as usual.
//
// originFallback is used when r has no OriginTime (a cancellation): the
// caller passes the origin time of the latest earlier report for the same
// EventID, or nil.
//
// The returned text never contains a CR or LF (see eew.StripLineBreaks).
func FormatReport(r *eew.Report, originFallback *time.Time) (text, title string) {
	reported := "不明"
	if !r.ReportTime.IsZero() {
		reported = r.ReportTime.Format(timeLayout)
	}

	times := " 第" + r.Serial + "報"
	if r.Final {
		times += "(最終)"
	}
	if r.InfoType == eew.Corrected {
		times += "(訂正)"
	}

	origin := r.OriginTime
	if origin == nil {
		origin = originFallback
	}
	occurred := ""
	if origin != nil {
		occurred = " (" + origin.Format(timeLayout) + "発生)"
	}

	if r.InfoType == eew.Cancelled {
		text = reported + times + occurred + " 取り消されました"
	} else {
		sep := ""
		if occurred == "" {
			sep = " "
		}
		text = reported + times + occurred + sep + center(r) + magnitude(r)
	}
	return eew.StripLineBreaks(text), Title
}

func center(r *eew.Report) string {
	name := r.HypocenterName
	if name == "" {
		name = "不明"
	}
	pos := "不明"
	if r.Lat != nil && r.Lon != nil {
		mapURI := fmt.Sprintf("http://maps.google.com/maps?q=%.1f,%.1f", *r.Lat, *r.Lon)
		pos = fmt.Sprintf("<%s|N%.1f/E%.1f>", mapURI, *r.Lat, *r.Lon)
	}
	d := depth(r.Depth)
	if r.AssumedHypocenter {
		d = "仮定"
	}
	return "震央:" + pos + "(" + name + ")深さ" + d
}

func depth(d eew.Depth) string {
	switch d.Kind {
	case eew.DepthKm:
		return fmt.Sprintf("%dkm", d.Km)
	case eew.DepthShallow:
		return "ごく浅い"
	case eew.Depth700Plus:
		return "700km以上"
	default:
		return "不明"
	}
}

func magnitude(r *eew.Report) string {
	m := "M不明"
	if r.AssumedHypocenter {
		// 仮定震源要素 (PLUM only): depth 10km and M1.0 are fixed
		// placeholders, not estimates; the position is the triggering
		// station's and is kept (user decision).
		m = "M仮定"
	} else if r.Magnitude != nil {
		m = fmt.Sprintf("M%.1f", *r.Magnitude)
	}
	return " 最大:" + m + " 震度" + intensity(r.Intensity)
}

var intensityLabel = map[string]string{
	"5-": "5弱", "5+": "5強", "6-": "6弱", "6+": "6強",
}

func label(code string) string {
	if l, ok := intensityLabel[code]; ok {
		return l
	}
	return code
}

func intensity(in eew.Intensity) string {
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
