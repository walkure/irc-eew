package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/walkure/irc-eew/internal/config"
	"gopkg.in/yaml.v3"
)

// TestLoad_DistExample loads the repo's own config.yaml-dist (the
// documented example config, safe to read — unlike the real gitignored
// config.yaml which holds live secrets) and checks all three Slack hook
// forms it demonstrates parse correctly.
func TestLoad_DistExample(t *testing.T) {
	path := filepath.Join("..", "..", "config.yaml-dist")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("config.yaml-dist not found at %s: %v", path, err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.LogDir != "./eewlog" {
		t.Errorf("LogDir: got %q", cfg.LogDir)
	}

	if cfg.Slack == nil {
		t.Fatal("Slack config is nil")
	}
	if len(cfg.Slack.All) != 3 {
		t.Fatalf("Slack.All: got %d entries, want 3", len(cfg.Slack.All))
	}
	// `- a: https://www.example.com/api/webhook` (labeled form)
	if cfg.Slack.All[0].Name != "a" || cfg.Slack.All[0].URL != "https://www.example.com/api/webhook" {
		t.Errorf("Slack.All[0]: got %+v", cfg.Slack.All[0])
	}
	// `- https://www.example.org/api/webhook` (bare scalar form)
	if cfg.Slack.All[1].Name != "" || cfg.Slack.All[1].URL != "https://www.example.org/api/webhook" {
		t.Errorf("Slack.All[1]: got %+v", cfg.Slack.All[1])
	}
	if cfg.Slack.All[1].DisplayName() != "www.example.org" {
		t.Errorf("Slack.All[1].DisplayName(): got %q, want host fallback", cfg.Slack.All[1].DisplayName())
	}
	// `- hoge: https://www.example.org/api/webhook` (labeled form again)
	if cfg.Slack.All[2].Name != "hoge" {
		t.Errorf("Slack.All[2]: got %+v", cfg.Slack.All[2])
	}

	if len(cfg.Slack.Limited) != 2 {
		t.Fatalf("Slack.Limited: got %d entries, want 2", len(cfg.Slack.Limited))
	}

	if cfg.WNIEEW.User != "username" || cfg.WNIEEW.Passwd != "bar" {
		t.Errorf("WNIEEW: got %+v", cfg.WNIEEW)
	}
	if cfg.WNIEEW.PasswdMD5 != "" {
		t.Errorf("WNIEEW.PasswdMD5: expected empty (commented out), got %q", cfg.WNIEEW.PasswdMD5)
	}
	// The Logs: block is entirely commented out in the dist file.
	if cfg.WNIEEW.Logs.Timeout || cfg.WNIEEW.Logs.KeepAlive {
		t.Errorf("Logs flags: expected both false, got %+v", cfg.WNIEEW.Logs)
	}
}

func TestLoad_IRCSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlText := `
logdir: ./eewlog
irc:
 - server:
    host: irc.example.jp
    port: 6667
    charset: iso-2022-jp
   nick: EEWNotice
   name: EEWBot
   desc: Emergency Earthquake Warning
   all-notice:
    - "#all"
    - "#whole"
   limited-notice:
    - "#limited"
 - server:
    host: irc2.example.jp
    port: 6697
   nick: EEWNotice2
   all-notice:
    - "#eew"
slack:
 all:
  - https://example.com/webhook
WNIEEW:
 user: u
 passwd: p
`
	if err := os.WriteFile(path, []byte(yamlText), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WNIEEW.User != "u" {
		t.Errorf("WNIEEW.User: got %q", cfg.WNIEEW.User)
	}

	if len(cfg.IRC) != 2 {
		t.Fatalf("IRC: got %d servers, want 2", len(cfg.IRC))
	}

	first := cfg.IRC[0]
	if first.Server.Host != "irc.example.jp" || first.Server.Port != 6667 {
		t.Errorf("IRC[0].Server: got %+v", first.Server)
	}
	if first.Server.Charset != "iso-2022-jp" {
		t.Errorf("IRC[0].Server.Charset: got %q", first.Server.Charset)
	}
	if first.Nick != "EEWNotice" || first.Name != "EEWBot" || first.Desc != "Emergency Earthquake Warning" {
		t.Errorf("IRC[0] identity: got %+v", first)
	}
	if len(first.AllNotice) != 2 || first.AllNotice[0] != "#all" || first.AllNotice[1] != "#whole" {
		t.Errorf("IRC[0].AllNotice: got %v", first.AllNotice)
	}
	if len(first.LimitedNotice) != 1 || first.LimitedNotice[0] != "#limited" {
		t.Errorf("IRC[0].LimitedNotice: got %v", first.LimitedNotice)
	}

	second := cfg.IRC[1]
	if second.Server.Charset != "" {
		t.Errorf("IRC[1].Server.Charset: expected empty (defaults to UTF-8 downstream), got %q", second.Server.Charset)
	}
}

// TestLoad_RejectsOldStyleIRCSection documents a deliberate compatibility
// break: irc-eew.pl (and the Go port before it supported IRC at all) used a
// single `irc:` mapping. Since IRC notification is a new Go-port feature,
// its config shape is a YAML sequence (multiple servers) from the start, so
// any old single-object `irc:` block left over in a real config.yaml needs
// rewriting into the list form rather than continuing to be silently inert.
func TestLoad_RejectsOldStyleIRCSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlText := `
irc:
 all-notice: "#all"
 server:
  host: irc.example.jp
  port: 6667
WNIEEW:
 user: u
 passwd: p
`
	if err := os.WriteFile(path, []byte(yamlText), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("expected an error for an old-style (single-object) irc: block")
	}
}

func TestFlag_AcceptsIntAndBoolForms(t *testing.T) {
	var f struct {
		AsInt  config.Flag `yaml:"as_int"`
		AsBool config.Flag `yaml:"as_bool"`
		AsZero config.Flag `yaml:"as_zero"`
	}
	yamlText := "as_int: 1\nas_bool: true\nas_zero: 0\n"
	if err := yaml.Unmarshal([]byte(yamlText), &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !bool(f.AsInt) {
		t.Error("as_int: 1 should be truthy")
	}
	if !bool(f.AsBool) {
		t.Error("as_bool: true should be truthy")
	}
	if bool(f.AsZero) {
		t.Error("as_zero: 0 should be falsy")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}
