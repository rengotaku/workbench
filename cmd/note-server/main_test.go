package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := defaultDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "share", "note-stock"); got != want {
		t.Errorf("defaultDataDir() = %q, want %q", got, want)
	}
}
