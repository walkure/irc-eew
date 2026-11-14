package eewview

import (
	"fmt"
	"html/template"
	"net/http"
)

func text(s string) template.HTML { return template.HTML(escaped(s)) }

func accSuffix(label string) string {
	if label == "" {
		return ""
	}
	return "(" + label + ")"
}

type detailField struct {
	Label string
	Value template.HTML
}

type detailPageData struct {
	Title    string
	PathBase template.HTML
	BackHref string
	Summary  string
	Fields   []detailField
	Areas    []areaRow
	HasLgInt bool
	HasMap   bool
	Lat, Lng float64
}

func detailHandler(cfg Config, st *store, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if !validName(name) {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}

		// Load the whole day so a cancellation can show the origin time of
		// its earthquake's earlier report, exactly as the listing does.
		year, month, day := nameDate(name)
		var rec *record
		for _, c := range loadDay(st.dayFiles(year, month, day)) {
			if c.Name == name {
				rec = c
				break
			}
		}
		if rec == nil {
			http.NotFound(w, r)
			return
		}

		data := detailPageData{
			Title:    DetailSummaryText(rec),
			PathBase: template.HTML(escaped(cfg.PathBase)),
			BackHref: fmt.Sprintf("%s?year=%s&month=%s&day=%s", cfg.PathBase, name[0:4], name[4:6], name[6:8]),
			Summary:  DetailSummaryText(rec),
			Fields:   buildFields(rec),
			Areas:    rec.Areas,
		}
		for _, a := range rec.Areas {
			if a.LgInt != "" {
				data.HasLgInt = true
			}
		}
		if !rec.Cancelled && rec.PositionKnown {
			data.HasMap = true
			data.Lat, data.Lng = rec.Lat, rec.Lng
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "detail.html.tmpl", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	}
}

func serialLabel(r *record) string {
	label := fmt.Sprintf("第%d報", r.Serial)
	if r.Final {
		label += "(最終)"
	}
	if r.Corrected {
		label += "(訂正)"
	}
	return label
}

// orUnknown renders an empty value as 不明.
func orUnknown(s string) string {
	if s == "" {
		return "不明"
	}
	return s
}

// buildFields lists the detail table rows. Legacy telegrams and VXSE45
// share every label; rows only VXSE45 carries (検知日時, 長周期地震動,
// 予測の変化, 付加文, 本文) appear only when the report has them.
func buildFields(r *record) []detailField {
	if r.Unparsable {
		return []detailField{{"地震ID", text(r.EventID)}, {"報数", text(serialLabel(r))}}
	}
	fields := []detailField{
		{"地震ID", text(r.EventID)},
		{"通知種類", text(orUnknown(r.Kind))},
		{"通知内容", text(orUnknown(r.Content))},
		{"通知対象", text(orUnknown(r.Target))},
		{"報数", text(serialLabel(r))},
		{"発信官署", text(orUnknown(r.Office))},
		{"発生日時", text(orUnknown(r.OriginFull))},
	}
	if r.ArrivalFull != "" {
		fields = append(fields, detailField{"検知日時", text(r.ArrivalFull)})
	}
	fields = append(fields, detailField{"通知日時", text(orUnknown(r.ReportFull))})

	if !r.Cancelled {
		name := r.CenterName
		if name == "" {
			name = "不明"
		}
		var loc template.HTML
		if r.PositionKnown {
			mapURI := fmt.Sprintf("https://maps.google.com/maps?q=%.1f,%.1f", r.Lat, r.Lng)
			loc = link(mapURI, fmt.Sprintf("N%.1f E%.1f", r.Lat, r.Lng))
		} else {
			loc = text("不明")
		}
		suffix := "(" + name + ")" + accSuffix(r.CenterAcc)
		if r.Assumed {
			suffix += "(仮定震源要素)"
		}
		fields = append(fields, detailField{"震央位置", loc + text(suffix)})
		if r.LandSea != "" {
			fields = append(fields, detailField{"内陸/海域", text(r.LandSea)})
		}
		fields = append(fields,
			// The depth row shows DepthAcc, not CenterAcc: HTML/eew-show.pl:106
			// printed center_accurate here (a copy-paste bug).
			detailField{"震源深度", text(r.DepthText + accSuffix(r.DepthAcc))},
			detailField{"マグニチュード", text(r.MagText + accSuffix(r.MagAcc))},
			detailField{"最大震度", text(r.MaxIntText)},
		)
		if r.MaxLgIntText != "" {
			fields = append(fields, detailField{"最大長周期地震動階級", text(r.MaxLgIntText)})
		}
		if r.ChangeText != "" {
			fields = append(fields, detailField{"予測の変化", text(r.ChangeText)})
		}
		if r.Comment != "" {
			fields = append(fields, detailField{"付加文", text(r.Comment)})
		}
	}
	if r.Text != "" {
		fields = append(fields, detailField{"本文", text(r.Text)})
	}
	return fields
}
