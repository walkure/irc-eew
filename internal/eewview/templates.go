package eewview

import (
	"embed"
	"html/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

func parseTemplates() (*template.Template, error) {
	return template.ParseFS(templateFS, "templates/*.tmpl")
}
