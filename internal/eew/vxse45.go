package eew

import (
	"encoding/xml"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

// FinalReportText is the fixed Body/NextAdvisory value of a final report
// (JMA 地震火山関連 解説資料 Ⅱ.21: "この情報をもって、緊急地震速報：最終報
// とします。"). Compared after Key normalization on both sides.
const FinalReportText = "この情報をもって、緊急地震速報：最終報とします。"

// xmlReport mirrors the parts of a JMA VXSE45 telegram this package uses.
// Elements are matched by local name; JMA's namespaces are fixed per
// element, so there is no ambiguity.
type xmlReport struct {
	XMLName xml.Name `xml:"Report"`
	Control struct {
		Title           string `xml:"Title"`
		Status          string `xml:"Status"`
		EditorialOffice string `xml:"EditorialOffice"`
	} `xml:"Control"`
	Head struct {
		Title          string `xml:"Title"`
		ReportDateTime string `xml:"ReportDateTime"`
		EventID        string `xml:"EventID"`
		InfoType       string `xml:"InfoType"`
		Serial         string `xml:"Serial"`
	} `xml:"Head"`
	Body struct {
		NextAdvisory *string `xml:"NextAdvisory"`
		Earthquake   *struct {
			OriginTime  *string `xml:"OriginTime"`
			Condition   *string `xml:"Condition"`
			ArrivalTime *string `xml:"ArrivalTime"`
			Hypocenter  struct {
				Area struct {
					Name       string `xml:"Name"`
					Coordinate struct {
						Value       string `xml:",chardata"`
						Description string `xml:"description,attr"`
					} `xml:"Coordinate"`
					LandOrSea string `xml:"LandOrSea"`
				} `xml:"Area"`
				Accuracy struct {
					Epicenter struct {
						Rank  string `xml:"rank,attr"`
						Rank2 string `xml:"rank2,attr"`
					} `xml:"Epicenter"`
					Depth struct {
						Rank string `xml:"rank,attr"`
					} `xml:"Depth"`
					MagnitudeCalculation struct {
						Rank string `xml:"rank,attr"`
					} `xml:"MagnitudeCalculation"`
					NumberOfMagnitudeCalculation string `xml:"NumberOfMagnitudeCalculation"`
				} `xml:"Accuracy"`
			} `xml:"Hypocenter"`
			Magnitude *struct {
				Value     string `xml:",chardata"`
				Condition string `xml:"condition,attr"`
			} `xml:"Magnitude"`
		} `xml:"Earthquake"`
		Intensity *struct {
			Forecast *struct {
				ForecastInt   *xmlRange `xml:"ForecastInt"`
				ForecastLgInt *xmlRange `xml:"ForecastLgInt"`
				Appendix      *struct {
					MaxIntChange       string `xml:"MaxIntChange"`
					MaxLgIntChange     string `xml:"MaxLgIntChange"`
					MaxIntChangeReason string `xml:"MaxIntChangeReason"`
				} `xml:"Appendix"`
				Pref []struct {
					Name string `xml:"Name"`
					Area []struct {
						Name     string `xml:"Name"`
						Code     string `xml:"Code"`
						Category struct {
							Kind struct {
								Name string `xml:"Name"`
								Code string `xml:"Code"`
							} `xml:"Kind"`
						} `xml:"Category"`
						ForecastInt   *xmlRange `xml:"ForecastInt"`
						ForecastLgInt *xmlRange `xml:"ForecastLgInt"`
						ArrivalTime   *string   `xml:"ArrivalTime"`
						Condition     *string   `xml:"Condition"`
					} `xml:"Area"`
				} `xml:"Pref"`
			} `xml:"Forecast"`
		} `xml:"Intensity"`
		Text     *string `xml:"Text"`
		Comments *struct {
			WarningComment []struct {
				Text string `xml:"Text"`
			} `xml:"WarningComment"`
		} `xml:"Comments"`
	} `xml:"Body"`
}

type xmlRange struct {
	From string `xml:"From"`
	To   string `xml:"To"`
}

// ParseVXSE45 parses a JMA 緊急地震速報（地震動予報） XML telegram. It
// only fails when the document can't be read as a JMA Report at all;
// anything present but not understood becomes "unknown" plus an entry in
// Report.Warnings.
func ParseVXSE45(body []byte) (*Report, error) {
	var x xmlReport
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, fmt.Errorf("eew: parsing VXSE45 XML: %w", err)
	}
	r := &Report{
		Title:           Display(x.Control.Title),
		EditorialOffice: Display(x.Control.EditorialOffice),
		Status:          Key(x.Control.Status),
		EventID:         Key(x.Head.EventID),
		Serial:          Key(x.Head.Serial),
	}
	if r.EventID == "" {
		r.warn("Head/EventID is empty")
	}

	switch it := Key(x.Head.InfoType); it {
	case "発表":
		r.InfoType = Issued
	case "訂正":
		r.InfoType = Corrected
	case "取消":
		r.InfoType = Cancelled
	default:
		r.InfoType = Issued
		r.warn("unknown Head/InfoType %q, treated as 発表", it)
	}

	if t, ok := r.parseTime("Head/ReportDateTime", &x.Head.ReportDateTime); ok {
		r.ReportTime = *t
	}

	if na := x.Body.NextAdvisory; na != nil {
		if Key(*na) == Key(FinalReportText) {
			r.Final = true
		} else {
			r.warn("Body/NextAdvisory present but not the final-report text: %q (not treated as final)", *na)
		}
	}

	if eq := x.Body.Earthquake; eq != nil {
		r.OriginTime, _ = r.parseTime("Earthquake/OriginTime", eq.OriginTime)
		r.ArrivalTime, _ = r.parseTime("Earthquake/ArrivalTime", eq.ArrivalTime)
		if eq.Condition != nil && Key(*eq.Condition) == "仮定震源要素" {
			r.AssumedHypocenter = true
		}
		r.HypocenterName = Display(eq.Hypocenter.Area.Name)
		r.LandOrSea = Key(eq.Hypocenter.Area.LandOrSea)
		r.parseCoordinate(eq.Hypocenter.Area.Coordinate.Value)
		acc := eq.Hypocenter.Accuracy
		r.Accuracy = Accuracy{
			Epicenter:         Key(acc.Epicenter.Rank),
			Epicenter2:        Key(acc.Epicenter.Rank2),
			Depth:             Key(acc.Depth.Rank),
			Magnitude:         Key(acc.MagnitudeCalculation.Rank),
			MagnitudeStations: Key(acc.NumberOfMagnitudeCalculation),
		}
		if m := eq.Magnitude; m != nil {
			r.parseMagnitude(m.Value, m.Condition)
		}
	}

	if in := x.Body.Intensity; in != nil && in.Forecast != nil {
		f := in.Forecast
		r.Intensity = r.intensity("ForecastInt", f.ForecastInt, intensityCodes)
		r.LgIntensity = r.intensity("ForecastLgInt", f.ForecastLgInt, lgIntensityCodes)
		if a := f.Appendix; a != nil {
			r.Change = Change{
				Present:  true,
				MaxInt:   Key(a.MaxIntChange),
				MaxLgInt: Key(a.MaxLgIntChange),
				Reason:   Key(a.MaxIntChangeReason),
			}
		}
		for _, p := range f.Pref {
			for _, a := range p.Area {
				af := AreaForecast{
					Pref:        Display(p.Name),
					Name:        Display(a.Name),
					Code:        Key(a.Code),
					KindName:    Display(a.Category.Kind.Name),
					KindCode:    Key(a.Category.Kind.Code),
					Intensity:   r.intensity("Area/ForecastInt", a.ForecastInt, intensityCodes),
					LgIntensity: r.intensity("Area/ForecastLgInt", a.ForecastLgInt, lgIntensityCodes),
				}
				af.ArrivalTime, _ = r.parseTime("Area/ArrivalTime", a.ArrivalTime)
				if a.Condition != nil && Key(*a.Condition) == "既に主要動到達と推測" {
					af.Arrived = true
				}
				r.Areas = append(r.Areas, af)
			}
		}
	}
	if x.Body.Text != nil {
		r.Text = Display(*x.Body.Text)
	}
	if c := x.Body.Comments; c != nil {
		for _, w := range c.WarningComment {
			if t := Display(w.Text); t != "" {
				if r.Comment != "" {
					r.Comment += " "
				}
				r.Comment += t
			}
		}
	}
	return r, nil
}

// intensity converts an optional From/To pair against the given code set.
func (r *Report) intensity(what string, rg *xmlRange, codes map[string]bool) Intensity {
	if rg == nil {
		return Intensity{}
	}
	return Intensity{
		Present: true,
		From:    r.rangeCode(what+"/From", rg.From, codes, false),
		To:      r.rangeCode(what+"/To", rg.To, codes, true),
	}
}

func (r *Report) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// parseTime parses an optional xs:dateTime element. ok is false when the
// element is absent or unparsable (the latter also warns).
func (r *Report) parseTime(what string, s *string) (t *time.Time, ok bool) {
	if s == nil {
		return nil, false
	}
	v, err := time.Parse(time.RFC3339, Key(*s))
	if err != nil {
		r.warn("%s %q unparsable: %v", what, *s, err)
		return nil, false
	}
	return &v, true
}

// coordinateRE matches the ISO 6709 forms JMA uses for jmx_eb:Coordinate:
// "+23.7+123.8-20000/" (depth in metres, negative = below sea level),
// "+39.0+140.9+0/" (ごく浅い), "+39.0+140.9/" (depth unknown). An empty
// value means 震源要素不明.
var coordinateRE = regexp.MustCompile(`^([+-]\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)([+-]\d+)?/$`)

func (r *Report) parseCoordinate(raw string) {
	v := Key(raw)
	if v == "" {
		return // 震源要素不明: everything stays unknown
	}
	m := coordinateRE.FindStringSubmatch(v)
	if m == nil {
		r.warn("jmx_eb:Coordinate %q not understood", raw)
		return
	}
	lat, err1 := strconv.ParseFloat(m[1], 64)
	lon, err2 := strconv.ParseFloat(m[2], 64)
	if err1 == nil && err2 == nil {
		r.Lat, r.Lon = &lat, &lon
	}
	if m[3] == "" {
		return // 深さ不明
	}
	metres, err := strconv.Atoi(m[3])
	switch {
	case err != nil:
		r.warn("jmx_eb:Coordinate depth %q not understood", m[3])
	case metres == 0:
		r.Depth = Depth{Kind: DepthShallow}
	case metres <= -700000:
		r.Depth = Depth{Kind: Depth700Plus}
	case metres < 0:
		r.Depth = Depth{Kind: DepthKm, Km: -metres / 1000}
	default:
		r.warn("jmx_eb:Coordinate depth %q above sea level, treated as unknown", m[3])
	}
}

func (r *Report) parseMagnitude(raw, condition string) {
	if Key(condition) == "不明" {
		return
	}
	v, err := strconv.ParseFloat(Key(raw), 64)
	if err != nil {
		r.warn("jmx_eb:Magnitude %q not understood", raw)
		return
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	r.Magnitude = &v
}

var intensityCodes = map[string]bool{
	"0": true, "1": true, "2": true, "3": true, "4": true,
	"5-": true, "5+": true, "6-": true, "6+": true, "7": true, "不明": true,
}

var lgIntensityCodes = map[string]bool{
	"0": true, "1": true, "2": true, "3": true, "4": true, "不明": true,
}

func (r *Report) rangeCode(what, raw string, codes map[string]bool, allowOver bool) string {
	v := Key(raw)
	if codes[v] || (allowOver && v == "over") {
		return v
	}
	r.warn("%s %q not understood, treated as 不明", what, raw)
	return "不明"
}
