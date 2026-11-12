package eew

import "time"

// InfoType is Head/InfoType.
type InfoType int

const (
	// Issued is 発表 (a normal report).
	Issued InfoType = iota
	// Corrected is 訂正 (corrects the latest report of the series).
	Corrected
	// Cancelled is 取消 (withdraws the whole series). A cancellation carries
	// no earthquake elements and keeps the previous report's Serial.
	Cancelled
)

func (t InfoType) String() string {
	switch t {
	case Issued:
		return "発表"
	case Corrected:
		return "訂正"
	case Cancelled:
		return "取消"
	default:
		return "?"
	}
}

// DepthKind distinguishes how a hypocenter depth is expressed.
type DepthKind int

const (
	// DepthUnknown: not given (深さ不明, 震源要素不明, cancellation...).
	DepthUnknown DepthKind = iota
	// DepthKm: an ordinary depth in Depth.Km.
	DepthKm
	// DepthShallow: ごく浅い (ISO 6709 depth +0). JMA says EEW doesn't use
	// it (it reports 10km instead), but it is in the shared element spec.
	DepthShallow
	// Depth700Plus: 700km以上.
	Depth700Plus
)

// Depth is a hypocenter depth.
type Depth struct {
	Kind DepthKind
	Km   int
}

// Intensity is the forecast maximum seismic intensity
// (Body/Intensity/Forecast/ForecastInt).
type Intensity struct {
	// Present is false when the element is absent (deeper than 150km,
	// single-station forecast, cancellation...).
	Present bool
	// From and To are the normalized raw codes: "0".."4", "5-", "5+",
	// "6-", "6+", "7", "不明", and for To also "over". A value outside
	// that set is replaced by "不明" (with a Report warning).
	From, To string
}

// Accuracy holds Hypocenter/Accuracy's rank codes, normalized ("" when
// absent). See JMA 地震火山関連 解説資料 Ⅱ.21 1-4-2 for the code tables.
type Accuracy struct {
	Epicenter, Epicenter2, Depth, Magnitude, MagnitudeStations string
}

// Change is Intensity/Forecast/Appendix: how the maximum forecasts moved
// since the previous report. Codes are normalized; Present is false when
// the element is absent.
type Change struct {
	Present                  bool
	MaxInt, MaxLgInt, Reason string
}

// AreaForecast is one Intensity/Forecast/Pref/Area entry.
type AreaForecast struct {
	Pref, Name, Code string
	// KindName is Category/Kind/Name (緊急地震速報（警報）/（予報）) and
	// KindCode its code (2nd digit 9 = PLUM-based forecast).
	KindName, KindCode string
	Intensity          Intensity
	LgIntensity        Intensity
	// ArrivalTime is the expected arrival of the main shaking (or, for a
	// PLUM forecast, when that intensity was first forecast); nil if absent.
	ArrivalTime *time.Time
	// Arrived is set for Condition 既に主要動到達と推測.
	Arrived bool
}

// Report is one parsed EEW report.
type Report struct {
	// Title is Control/Title (e.g. 緊急地震速報（地震動予報）).
	Title string
	// EditorialOffice is Control/EditorialOffice (e.g. 大阪管区気象台).
	EditorialOffice string
	// Status is Control/Status: 通常, 訓練 or 試験.
	Status   string
	EventID  string
	Serial   string
	InfoType InfoType
	// ReportTime is Head/ReportDateTime; zero if missing or unparsable.
	ReportTime time.Time
	// OriginTime is Earthquake/OriginTime; nil when absent (cancellation,
	// or a 最大予測震度のみ report).
	OriginTime *time.Time
	// ArrivalTime is Earthquake/ArrivalTime (detection at a station).
	ArrivalTime *time.Time
	// Final is set when Body/NextAdvisory carries the fixed final-report
	// text.
	Final bool
	// AssumedHypocenter is set for 仮定震源要素 (PLUM-only; depth 10km and
	// M1.0 are placeholders).
	AssumedHypocenter bool
	// HypocenterName is Hypocenter/Area/Name, display-normalized; empty
	// when unknown.
	HypocenterName string
	// Lat and Lon are the epicenter in degrees; nil when unknown.
	Lat, Lon *float64
	// LandOrSea is 内陸 or 海域 (empty when absent).
	LandOrSea string
	Accuracy  Accuracy
	Depth     Depth
	Magnitude *float64
	Intensity Intensity
	// LgIntensity is the forecast maximum long-period ground motion class
	// (ForecastLgInt): codes "0".."4", "不明", and "over" for To.
	LgIntensity Intensity
	Change      Change
	Areas       []AreaForecast
	// Comment is Comments/WarningComment/Text (display-normalized).
	Comment string
	// Text is Body/Text: the cancellation reason, or the free text of a
	// 最大予測震度のみ report (display-normalized).
	Text string
	// Warnings lists values that were present but not understood, for
	// logging. They never stop a report from being used.
	Warnings []string
}

// Event returns the dispatch-relevant view of r.
func (r *Report) Event() Event {
	return Event{EventID: r.EventID, Final: r.Final, Cancelled: r.InfoType == Cancelled}
}
