package cmdutil

import (
	"os"
	"path/filepath"
	"testing"

	"pigcloud/internal/config"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pigcloud-cmdutil-test-*")
	if err == nil {
		config.SetConfigFile(filepath.Join(dir, "config.json"))
		config.Load()
	}
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

func TestResolvePath(t *testing.T) {

	tests := []struct {
		input string
		want  string
	}{
		{"", "/"},
		{"/", "/"},
		{"/docs", "/docs"},
		{"/docs/../photos", "/photos"},
	}

	for _, tt := range tests {
		got := ResolvePath(tt.input)
		if got != tt.want {
			t.Errorf("ResolvePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripMSYSConversion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/", "/"},
		{"/documents", "/documents"},
		{"relative/path", "relative/path"},
	}
	for _, tt := range tests {
		got := stripMSYSConversion(tt.input)
		if got != tt.want {
			t.Errorf("stripMSYSConversion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
