package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAppURLFollowsTheAppsPort(t *testing.T) {
	t.Setenv("APP_PORT", "")
	t.Setenv("APP_PORT_FILE", "")
	if got := defaultAppURL(); got != "http://127.0.0.1:8264" {
		t.Fatalf("default: %s", got)
	}
	t.Setenv("APP_PORT", "abc")
	if got := defaultAppURL(); got != "http://127.0.0.1:8264" {
		t.Fatalf("a port that is not a number must not end up in the address: %s", got)
	}
	file := filepath.Join(t.TempDir(), "port")
	if err := os.WriteFile(file, []byte("9123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_PORT", "")
	t.Setenv("APP_PORT_FILE", file)
	if got := defaultAppURL(); got != "http://127.0.0.1:9123" {
		t.Fatalf("APP_PORT_FILE: %s", got)
	}
}
