// Package config loads the daemon's YAML configuration, matching the shape
// documented in config.yaml-dist. The `irc:` section has no corresponding
// field here — yaml.v3 ignores unknown keys by default, so an existing
// production config.yaml (which still has an irc: section) loads unchanged,
// with IRC support simply never referenced.
package config

import (
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the root of the YAML configuration.
type Config struct {
	// LogDir, if set, enables raw-telegram archival (see internal/eewlog).
	// Omitted/empty means "don't save EEW data", matching irc-eew.pl.
	LogDir string       `yaml:"logdir"`
	Slack  *SlackConfig `yaml:"slack"`
	WNIEEW WNIConfig    `yaml:"WNIEEW"`
}

// SlackConfig lists the "all" (every telegram) and "limited" (first/final/
// cancellation only) Slack webhook tiers.
type SlackConfig struct {
	All     []Hook `yaml:"all"`
	Limited []Hook `yaml:"limited"`
}

// Hook is one Slack incoming-webhook entry. YAML accepts either a labeled
// single-key mapping (`- name: https://...`) or a bare URL string
// (`- https://...`), matching the two branches SlackWebhookSock::configure
// handles in the Perl original.
type Hook struct {
	Name string
	URL  string
}

// DisplayName returns Name if set, otherwise the URL's host — matching
// SlackWebhookSock.pm's `*$self->{name} = $name || $path->host();` fallback.
func (h Hook) DisplayName() string {
	if h.Name != "" {
		return h.Name
	}
	if u, err := url.Parse(h.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return h.URL
}

// UnmarshalYAML implements custom decoding for the two accepted hook forms.
func (h *Hook) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		h.Name = ""
		h.URL = s
		return nil
	case yaml.MappingNode:
		var m map[string]string
		if err := value.Decode(&m); err != nil {
			return err
		}
		for k, v := range m {
			h.Name = k
			h.URL = v
			return nil
		}
		return fmt.Errorf("config: hook mapping at line %d is empty", value.Line)
	default:
		return fmt.Errorf("config: unexpected YAML node kind for a slack hook entry at line %d", value.Line)
	}
}

// WNIConfig mirrors the WNIEEW: section (WNI FastCaster login credentials).
type WNIConfig struct {
	User string `yaml:"user"`
	// Passwd is used verbatim (then MD5-hashed) unless PasswdMD5 is set.
	Passwd    string `yaml:"passwd"`
	PasswdMD5 string `yaml:"passwd-md5"`
	Logs      struct {
		Timeout   Flag `yaml:"Timeout"`
		KeepAlive Flag `yaml:"KeepAlive"`
	} `yaml:"Logs"`
	// ServerOverride, if set ("host:port"), skips the real WNI server-list
	// HTTP fetch and always connects there instead. Has no equivalent in
	// the Perl original; exists for pointing the daemon at a test/fake WNI
	// server (e.g. internal/wnisim) during verification or shadow-mode
	// rollout, never needed against the real production service.
	ServerOverride string `yaml:"server-override"`
}

// Flag is a lenient boolean that accepts YAML `true`/`false` as well as the
// `1`/`0` integer form config.yaml-dist actually uses for Logs.Timeout /
// Logs.KeepAlive (Perl's config just checks truthiness of the raw value).
type Flag bool

func (f *Flag) UnmarshalYAML(value *yaml.Node) error {
	var asBool bool
	if err := value.Decode(&asBool); err == nil {
		*f = Flag(asBool)
		return nil
	}
	var asInt int
	if err := value.Decode(&asInt); err == nil {
		*f = Flag(asInt != 0)
		return nil
	}
	return fmt.Errorf("config: cannot parse %q as a boolean flag at line %d", value.Value, value.Line)
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return &cfg, nil
}
