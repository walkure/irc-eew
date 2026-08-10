// Package config loads the daemon's YAML configuration, matching the shape
// documented in config.yaml-dist. The `irc:` section is a list of servers
// (see IRCServerConfig) — unlike irc-eew.pl, which only ever supported one.
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
	// IRC lists zero or more IRC servers to notify. Unlike irc-eew.pl (which
	// only ever supported one `irc:` server block), this is a YAML sequence
	// so the daemon can join multiple networks. An old Perl-era single-object
	// `irc:` block (from before the Go port supported IRC at all) will fail
	// to parse against this shape and needs rewriting into the list form.
	IRC []IRCServerConfig `yaml:"irc"`
}

// IRCServerConfig describes one IRC server to connect to and notify.
type IRCServerConfig struct {
	Server struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Password string `yaml:"password"`
		// Charset names the encoding used for message bodies and channel
		// names sent to this server ("iso-2022-jp" or "utf-8"/omitted).
		// Unlike IRCSock.pm (which fell back to ISO-2022-JP, matching the
		// author's own IRC network at the time), an unset Charset here
		// defaults to UTF-8 — the more common choice today.
		Charset string `yaml:"charset"`
		// MessageType selects the IRC command used to notify a channel:
		// "notice" (default, matching irc-eew.pl's IRCSock::notice, which is
		// what eew_callback actually called) or "privmsg".
		MessageType string `yaml:"message-type"`
	} `yaml:"server"`
	Nick string `yaml:"nick"`
	Name string `yaml:"name"`
	Desc string `yaml:"desc"`
	// AllNotice and LimitedNotice list the channels to join and notify.
	// Perl's config.yaml-dist used a comma-separated string
	// (`"#all,#whole,#news:*.jp"`); this is a YAML list instead, matching
	// how Slack hooks are configured.
	AllNotice     []string `yaml:"all-notice"`
	LimitedNotice []string `yaml:"limited-notice"`
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
