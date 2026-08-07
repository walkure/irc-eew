package eewview

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/walkure/irc-eew/internal/decoder"
)

// detailNameRE validates the `name` query param before it's used to build
// a filesystem path. HTML/eew-show.pl's equivalent (/\d{14}\.\d{1,4}/) is
// an unanchored *contains* match; anchored here (^...$) as a strict
// superset of what any legitimate caller (this viewer's own generated
// links) ever produces.
var detailNameRE = regexp.MustCompile(`^\d{14}\.\d{1,4}$`)

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

type ebiRow struct {
	Name, Shindo, Time string
}

type detailPageData struct {
	Title    string
	PathBase template.HTML
	BackHref string
	Summary  string
	Fields   []detailField
	EBI      []ebiRow
	HasMap   bool
	Lat, Lng float64
	RawDump  string
}

func detailHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if !detailNameRE.MatchString(name) {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}

		path, err := telegramPath(cfg.DataDir, name)
		if err != nil {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		tel := decoder.Decode(raw)
		data := detailPageData{
			Title:    DetailSummaryText(tel),
			PathBase: template.HTML(escaped(cfg.PathBase)),
			BackHref: fmt.Sprintf("%s?year=%s&month=%s&day=%s", cfg.PathBase, name[0:4], name[4:6], name[6:8]),
			Summary:  DetailSummaryText(tel),
			Fields:   buildFields(tel),
			EBI:      buildEBIRows(tel),
			RawDump:  markControlChars(decodeShiftJIS(raw)),
		}
		if !tel.IsCancellation() && tel.CenterCode != "" {
			data.HasMap = true
			data.Lat, data.Lng = tel.CenterLat, tel.CenterLng
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "detail.html.tmpl", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	}
}

// telegramPath builds the archival path for a validated "<eq_id>.<warn_num>"
// name, matching get_fn_from_eqid's <dir>/YYYY/MM/DD/<name> layout exactly
// (same convention as internal/eewlog.FinalPath, but built directly from
// the already-validated filename string rather than round-tripping through
// eqID+int, so a leading-zero warn_num — none exist in the real archive
// today, but nothing rules it out — can never be reformatted into a
// different filename than what was requested).
func telegramPath(dataDir, name string) (string, error) {
	if len(name) < 8 {
		return "", fmt.Errorf("eewview: name %q too short", name)
	}
	full := filepath.Join(dataDir, name[0:4], name[4:6], name[6:8], name)

	// Defense in depth: detailNameRE already makes traversal impossible
	// (only digits and one dot are ever accepted), but confirm the
	// resolved path still lives under dataDir regardless.
	base, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(resolved, base) {
		return "", fmt.Errorf("eewview: resolved path escapes data dir")
	}
	return full, nil
}

func buildFields(tel *decoder.Telegram) []detailField {
	fields := []detailField{
		{"地震ID", text(tel.EqID)},
		{"通知種類", text(tel.MsgType)},
		{"通知内容", text(tel.CodeType)},
		{"通知対象", text(tel.WarnType)},
		{"報数", text(warnNumLabel(tel))},
		{"発信官署", text(tel.Section)},
		{"検知日時", text(fullDateTime(tel.EqTime))},
		{"通知日時", text(fullDateTime(tel.WarnTime))},
	}

	if tel.IsCancellation() || tel.CenterCode == "" {
		return fields
	}

	mapURI := fmt.Sprintf("https://maps.google.com/maps?q=%.1f,%.1f", tel.CenterLat, tel.CenterLng)
	centerLabel := fmt.Sprintf("N%.1f E%.1f", tel.CenterLat, tel.CenterLng)
	locValue := link(mapURI, centerLabel) + template.HTML(escaped(fmt.Sprintf("(%s)%s", tel.CenterName, accSuffix(tel.CenterAccurate))))

	fields = append(fields,
		detailField{"震央位置", locValue},
		// Fixed vs. HTML/eew-show.pl:106, which prints center_accurate here
		// instead of depth_accurate (a copy-paste bug) — see plan doc.
		detailField{"震源深度", text(fmt.Sprintf("%dkm%s", tel.CenterDepth, accSuffix(tel.DepthAccurate)))},
		detailField{"マグニチュード", text(fmt.Sprintf("M%.1f%s", tel.Magnitude, accSuffix(tel.MagnitudeAccurate)))},
		detailField{"最大震度", text(tel.Shindo)},
	)
	return fields
}

func warnNumLabel(t *decoder.Telegram) string {
	label := fmt.Sprintf("第%d報", t.WarnNum)
	if isFinalForViewer(t) {
		label += "(最終)"
	}
	return label
}

func buildEBIRows(tel *decoder.Telegram) []ebiRow {
	if len(tel.EBI) == 0 {
		return nil
	}
	areas := make([]string, 0, len(tel.EBI))
	for area := range tel.EBI {
		areas = append(areas, area)
	}
	sort.Strings(areas)

	rows := make([]ebiRow, 0, len(areas))
	for _, area := range areas {
		e := tel.EBI[area]

		shindo := "震度" + e.Shindo1
		switch {
		case e.Shindo2Code == "//":
			shindo += "以上"
		case e.Shindo1 != e.Shindo2:
			shindo += "～震度" + e.Shindo2
		}

		arrival := e.Time
		if arrival == "//////" {
			arrival = "既に到達"
		} else if p := splitPairs(arrival); len(p) >= 3 {
			arrival = fmt.Sprintf("%s:%s:%s", p[0], p[1], p[2])
		}

		rows = append(rows, ebiRow{Name: e.Name, Shindo: shindo, Time: arrival})
	}
	return rows
}
