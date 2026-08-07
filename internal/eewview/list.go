package eewview

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/walkure/irc-eew/internal/decoder"
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

var eewFileNameRE = regexp.MustCompile(`^(\d+)\.(\d+)$`)

// splitEEWName parses an archived telegram filename ("<eq_id>.<warn_num>")
// into its parts.
func splitEEWName(name string) (eqID string, warnNum int, ok bool) {
	m := eewFileNameRE.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// lessEEWFile replicates HTML/index.pl's custom sort: group by eq_id and
// sort numerically by warn_num within a group, else fall back to a plain
// string compare.
func lessEEWFile(a, b string) bool {
	aID, aNum, aOK := splitEEWName(a)
	bID, bNum, bOK := splitEEWName(b)
	if aOK && bOK && aID == bID {
		return aNum < bNum
	}
	return a < b
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

		prefix := fmt.Sprintf("%d年%d月%d日", year, month, day)
		entries, err := os.ReadDir(dayDir)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if _, _, ok := splitEEWName(e.Name()); ok {
				names = append(names, e.Name())
			}
		}
		sort.Slice(names, func(i, j int) bool { return lessEEWFile(names[i], names[j]) })

		for _, name := range names {
			body, err := os.ReadFile(filepath.Join(dayDir, name))
			if err != nil {
				continue
			}
			tel := decoder.Decode(body)
			summary := ListSummaryText(tel)
			href := fmt.Sprintf("%s%s?name=%s", cfg.PathBase, cfg.ViewerPath, name)
			rows = append(rows, template.HTML(escaped(prefix))+" "+link(href, "■")+" "+template.HTML(escaped(summary)))
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
