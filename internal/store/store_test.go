package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengotaku/workbench/internal/store"
)

func TestWriteFileAtomicReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	for _, body := range []string{"one", "two"} {
		if err := store.WriteFileAtomic(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != body {
			t.Fatalf("got %q want %q", got, body)
		}
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestWriteFileAtomicFailsInMissingDir(t *testing.T) {
	if err := store.WriteFileAtomic(filepath.Join(t.TempDir(), "no", "x"), []byte("a")); err == nil {
		t.Fatal("expected error")
	}
}

func TestAppendLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.jsonl")
	for _, l := range []string{`{"a":1}`, `{"b":2}`} {
		if err := store.AppendLine(p, []byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(p)
	if string(got) != "{\"a\":1}\n{\"b\":2}\n" {
		t.Errorf("got %q", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestMonthlyLogPath(t *testing.T) {
	at := time.Date(2026, 3, 9, 1, 0, 0, 0, time.UTC)
	if got, want := store.MonthlyLogPath("/d", at), "/d/events-2026-03.jsonl"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestLockIsExclusiveUntilClosed(t *testing.T) {
	dir := t.TempDir()
	l, err := store.AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AcquireLock(dir); !errors.Is(err, store.ErrLocked) {
		t.Fatalf("second lock err = %v, want ErrLocked", err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	l2, err := store.AcquireLock(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = l2.Close()
}
