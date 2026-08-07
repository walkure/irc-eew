package eewview

import (
	"fmt"
	"net/http"
	"strings"
)

// NewServer builds the viewer's http.Handler: the list view at "/" and the
// detail view at "/"+cfg.ViewerPath, replacing lighttpd+FastCGI's two
// separate processes/routes with one binary serving both directly.
func NewServer(cfg Config) (http.Handler, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("eewview: parsing templates: %w", err)
	}

	mux := http.NewServeMux()
	viewerRoute := "/" + strings.TrimPrefix(cfg.ViewerPath, "/")
	mux.HandleFunc(viewerRoute, detailHandler(cfg, tmpl))
	mux.HandleFunc("/", listHandler(cfg, tmpl))
	return mux, nil
}
