package main

import (
	"log/slog"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"DEBUG", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"trace", 0, true},
		{"verbose", 0, true},
	}

	for _, c := range cases {
		got, err := parseLogLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseLogLevel(%q): expected an error, got level %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLogLevel(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseLogLevel(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}
