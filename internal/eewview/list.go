package eewview

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// saturate mirrors HTML/index.pl's saturate(): an absent query param
// yields 0 (unclamped — deliberately not clamped to min, so an absent
// year/month/day falls through to the "list this level" branch); a
// present-but-non-numeric value numifies to 0 first (matching Perl's
// numeric-context coercion of a non-numeric string) and is then clamped
// like any other value.
func saturate(raw string, min, max int) int {
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		v = 0
	}
	if v > max {
		return max
	}
	if v < min {
		return min
	}
	return v
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// numericSubdirs lists dir's immediate subdirectories whose names start
// with a digit, sorted by name (os.ReadDir already returns entries sorted
// by filename, matching Perl's `sort readdir()`).
func numericSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if len(e.Name()) == 0 || e.Name()[0] < '0' || e.Name()[0] > '9' {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

type listPageData struct {
	PathBase template.HTML
	Rows     []template.HTML
}

func listHandler(cfg Config, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		year := saturate(q.Get("year"), 2000, 3000)
		month := saturate(q.Get("month"), 1, 12)
		day := saturate(q.Get("day"), 1, 31)

		var rows []template.HTML

		yearDir := filepath.Join(cfg.DataDir, fmt.Sprintf("%d", year))
		if !dirExists(yearDir) {
			for _, name := range numericSubdirs(cfg.DataDir) {
				n, err := strconv.Atoi(name)
				if err != nil {
					continue
				}
				href := fmt.Sprintf("%s?year=%d", cfg.PathBase, n)
				rows = append(rows, link(href, fmt.Sprintf("%d年", n)))
			}
			renderList(w, tmpl, cfg, rows)
			return
		}

		monthDir := filepath.Join(cfg.DataDir, fmt.Sprintf("%04d", year), fmt.Sprintf("%02d", month))
		if !dirExists(monthDir) {
			prefix := link(fmt.Sprintf("%s?year=%d", cfg.PathBase, year), fmt.Sprintf("%d年", year))
			for _, name := range numericSubdirs(yearDir) {
				n, err := strconv.Atoi(name)
				if err != nil {
					continue
				}
				href := fmt.Sprintf("%s?year=%d&month=%d", cfg.PathBase, year, n)
				rows = append(rows, prefix+link(href, fmt.Sprintf("%d月", n)))
			}
			renderList(w, tmpl, cfg, rows)
			return
		}

		dayDir := filepath.Join(cfg.DataDir, fmt.Sprintf("%04d", year), fmt.Sprintf("%02d", month), fmt.Sprintf("%02d", day))
		if !dirExists(dayDir) {
			prefix := link(fmt.Sprintf("%s?year=%d", cfg.PathBase, year), fmt.Sprintf("%d年", year)) +
				link(fmt.Sprintf("%s?year=%d&month=%d", cfg.PathBase, year, month), fmt.Sprintf("%d月", month))
			for _, name := range numericSubdirs(monthDir) {
				n, err := strconv.Atoi(name)
				if err != nil {
					continue
				}
				href := fmt.Sprintf("%s?year=%d&month=%d&day=%d", cfg.PathBase, year, month, n)
				rows = append(rows, prefix+link(href, fmt.Sprintf("%d日", n)))
			}
			renderList(w, tmpl, cfg, rows)
			return
		}

		prefix := link(fmt.Sprintf("%s?year=%d", cfg.PathBase, year), fmt.Sprintf("%d年", year)) +
			link(fmt.Sprintf("%s?year=%d&month=%d", cfg.PathBase, year, month), fmt.Sprintf("%d月", month)) +
			template.HTML(escaped(fmt.Sprintf("%d日", day)))
		recs, err := loadDay(dayDir)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		for _, rec := range recs {
			href := fmt.Sprintf("%s%s?name=%s", cfg.PathBase, cfg.ViewerPath, rec.Name)
			rows = append(rows, prefix+" "+link(href, "■")+" "+template.HTML(escaped(ListSummaryText(rec))))
		}
		renderList(w, tmpl, cfg, rows)
	}
}

func renderList(w http.ResponseWriter, tmpl *template.Template, cfg Config, rows []template.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := listPageData{PathBase: template.HTML(escaped(cfg.PathBase)), Rows: rows}
	if err := tmpl.ExecuteTemplate(w, "list.html.tmpl", data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
