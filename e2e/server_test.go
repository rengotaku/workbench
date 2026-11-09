//go:build e2e

// Package e2e drives the real server in headless Chrome. It locks what the
// screens do for a person (DOM, text, URL), not how the JS is written, so it
// keeps passing when a screen is rewritten.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// serverBin is the server built once for the whole run.
var serverBin string

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(file))
	// bin/ is inside the worktree (gitignored): a binary run from a temp
	// directory is what EDR kills on a work Mac.
	serverBin = filepath.Join(root, "bin", "workbench-e2e")
	build := exec.Command("go", "build", "-o", serverBin, "./cmd/server")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building the server: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// app is a running server with its own data directories.
type app struct {
	base string // http://127.0.0.1:<port>
}

// startApp runs the server on a free port with empty data directories and
// stops it when the test ends.
func startApp(t *testing.T) *app {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("reading the port: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("releasing the port: %v", err)
	}

	var logs bytes.Buffer
	cmd := exec.Command(serverBin)
	cmd.Env = append(os.Environ(),
		"PORT="+port,
		"APP_ENV=",
		"TODO_DATA_DIR="+t.TempDir(),
		"NOTE_DATA_DIR="+t.TempDir(),
	)
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the server: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		if t.Failed() {
			t.Logf("server log:\n%s", logs.String())
		}
	})

	a := &app{base: "http://127.0.0.1:" + port}
	deadline := time.Now().Add(15 * time.Second)
	for {
		res, err := http.Get(a.base + "/api/revision")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return a
			}
		}
		select {
		case <-exited:
			t.Fatalf("the server exited early:\n%s", logs.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server did not come up on %s", a.base)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// post sends a JSON write to the API and returns the decoded answer.
func (a *app) post(t *testing.T, path string, body any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(a.base+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		t.Fatalf("POST %s = %d: %s", path, res.StatusCode, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("POST %s: bad JSON %q: %v", path, data, err)
	}
	return out
}

// addTask seeds a task and returns its id.
func (a *app) addTask(t *testing.T, name string) string {
	t.Helper()
	out := a.post(t, "/api/tasks", map[string]any{"name": name})
	task, ok := out["task"].(map[string]any)
	if !ok {
		t.Fatalf("no task in the answer: %v", out)
	}
	return fmt.Sprint(task["id"])
}

// addNote seeds a memo under topic.
func (a *app) addNote(t *testing.T, text, topic string) {
	t.Helper()
	a.post(t, "/api/notes", map[string]any{"text": text, "topic": topic})
}
