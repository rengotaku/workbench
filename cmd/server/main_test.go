package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"todo-order", "note-stock"} {
		got, err := defaultDataDir(name)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".local", "share", name); got != want {
			t.Errorf("defaultDataDir(%q) = %q, want %q", name, got, want)
		}
	}
}
