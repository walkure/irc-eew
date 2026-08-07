// Package eewview serves a read-only web viewer for raw EEW telegrams
// archived by the notifier daemon (see internal/eewlog), porting
// HTML/index.pl (list view) and HTML/eew-show.pl (detail view) as a plain
// net/http server instead of CGI/FastCGI behind lighttpd — nothing about
// this component actually requires that split.
package eewview

import "os"

// Config configures the viewer server. Fields are normally sourced from
// environment variables (LoadConfigFromEnv), matching the documented
// deployment contract in HTML/README.md, so existing `docker run -e ...`
// invocations keep working unchanged against the Go image.
type Config struct {
	// DataDir is the root directory of archived telegrams, in the
	// <DataDir>/YYYY/MM/DD/<eq_id>.<warn_num> layout internal/eewlog writes
	// (EEW_DATA_DIR).
	DataDir string
	// PathBase prefixes every generated link href (EEW_PATH_BASE).
	PathBase string
	// ViewerPath is both the route the detail view is served on and the
	// link target the list view generates (EEW_VIEWER) — a single source
	// of truth, unlike the Perl/lighttpd deployment where EEW_VIEWER had
	// to independently match lighttpd.conf's fastcgi route by hand.
	ViewerPath string
	// ListenAddr is the address the HTTP server binds (EEW_LISTEN_ADDR) —
	// new in the Go port; the Perl original had no equivalent because
	// lighttpd's port was fixed in lighttpd.conf, not env-configurable.
	ListenAddr string
}

// LoadConfigFromEnv builds a Config from environment variables, applying
// the same defaults as the Perl originals.
func LoadConfigFromEnv() Config {
	return Config{
		DataDir:    envOr("EEW_DATA_DIR", "./eewlog/"),
		PathBase:   envOr("EEW_PATH_BASE", "./"),
		ViewerPath: envOr("EEW_VIEWER", "eew-show"),
		ListenAddr: envOr("EEW_LISTEN_ADDR", ":8080"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
