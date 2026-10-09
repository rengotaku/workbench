package noteapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/note"
)

func newServer(t *testing.T) (http.Handler, *note.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := note.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	New(store).Mount(r)
	return r, store
}

func call(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("body %q is not the expected JSON: %v", rec.Body.String(), err)
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, want, rec.Body.String())
	}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	wantStatus(t, rec, status)
	if e := decodeBody[errorBody](t, rec); e.Error == "" {
		t.Errorf("error body should carry a message: %s", rec.Body.String())
	}
}

func TestAddAndGetNote(t *testing.T) {
	h, _ := newServer(t)
	rec := call(t, h, http.MethodPost, "/api/notes", `{"text":" https://x.slack.com/a/1 メモ ","topic":"青分離"}`)
	wantStatus(t, rec, http.StatusCreated)
	n := decodeBody[note.Note](t, rec)
	if n.ID != 1 || n.Text != "https://x.slack.com/a/1 メモ" || n.URL != "https://x.slack.com/a/1" || n.Topic != "青分離" {
		t.Errorf("created note = %+v", n)
	}

	rec = call(t, h, http.MethodGet, "/api/notes/1", "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeBody[note.Note](t, rec); got.ID != 1 {
		t.Errorf("got %+v", got)
	}

	rec = call(t, h, http.MethodGet, "/api/topics", "")
	if got := decodeBody[TopicsResponse](t, rec); len(got.Topics) != 1 || got.Topics[0] != "青分離" {
		t.Errorf("topics = %+v (a note's topic must be registered)", got)
	}
}

func TestAddNoteRejects(t *testing.T) {
	h, store := newServer(t)
	for _, body := range []string{`{"text":""}`, `{"text":"   "}`, `{}`, `not json`} {
		wantError(t, call(t, h, http.MethodPost, "/api/notes", body), http.StatusBadRequest)
	}
	if len(store.List(note.Filter{})) != 0 {
		t.Error("rejected requests must store nothing")
	}
}

func TestListNotesFilters(t *testing.T) {
	h, store := newServer(t)
	a, _ := store.Add("a", "X")
	_, _ = store.Add("b", "")
	c, _ := store.Add("c", "X")
	if _, err := store.Annotate(a.ID, note.AnnotationInput{Kind: note.KindMemo, Body: "x"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query string
		want  []int
	}{
		{"", []int{3, 2, 1}},
		{"?topic=X", []int{3, 1}},
		{"?pending=1", []int{3, 2}},
		{"?no_topic=1", []int{2}},
		{"?topic=X&pending=1", []int{c.ID}},
		{"?topic=nope", []int{}},
	}
	for _, tt := range tests {
		rec := call(t, h, http.MethodGet, "/api/notes"+tt.query, "")
		wantStatus(t, rec, http.StatusOK)
		r := decodeBody[NotesResponse](t, rec)
		got := []int{}
		for _, n := range r.Notes {
			got = append(got, n.ID)
		}
		if len(got) != len(tt.want) {
			t.Errorf("%q: got %v, want %v", tt.query, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%q: got %v, want %v", tt.query, got, tt.want)
			}
		}
		if r.Pending != 2 {
			t.Errorf("%q: pending = %d, want 2 (whole store)", tt.query, r.Pending)
		}
	}
}

func TestNoteIDErrors(t *testing.T) {
	h, _ := newServer(t)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/notes/99", "", http.StatusNotFound},
		{http.MethodDelete, "/api/notes/99", "", http.StatusNotFound},
		{http.MethodPost, "/api/notes/99/annotations", `{"kind":"memo","body":"x"}`, http.StatusNotFound},
		{http.MethodPut, "/api/notes/99/topic", `{"topic":"x"}`, http.StatusNotFound},
		{http.MethodGet, "/api/notes/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/notes/0", "", http.StatusBadRequest},
		{http.MethodDelete, "/api/notes/abc", "", http.StatusBadRequest},
		{http.MethodPost, "/api/notes/abc/annotations", `{"kind":"memo","body":"x"}`, http.StatusBadRequest},
		{http.MethodPut, "/api/notes/abc/topic", `{"topic":"x"}`, http.StatusBadRequest},
	} {
		wantError(t, call(t, h, tc.method, tc.path, tc.body), tc.status)
	}
}

func TestDeleteNote(t *testing.T) {
	h, store := newServer(t)
	_, _ = store.Add("a", "")
	wantStatus(t, call(t, h, http.MethodDelete, "/api/notes/1", ""), http.StatusNoContent)
	wantError(t, call(t, h, http.MethodGet, "/api/notes/1", ""), http.StatusNotFound)
}

func TestAnnotate(t *testing.T) {
	h, store := newServer(t)
	_, _ = store.Add("a", "")

	rec := call(t, h, http.MethodPost, "/api/notes/1/annotations", `{"kind":"quote","body":"引用\n二行","source":"slack"}`)
	wantStatus(t, rec, http.StatusCreated)
	a := decodeBody[note.Annotation](t, rec)
	if a.Kind != "quote" || a.Body != "引用\n二行" || a.Source != "slack" || a.Author != "ai" {
		t.Errorf("annotation = %+v", a)
	}
	rec = call(t, h, http.MethodPost, "/api/notes/1/annotations", `{"kind":"memo","body":"人","author":"human"}`)
	wantStatus(t, rec, http.StatusCreated)

	got := decodeBody[note.Note](t, call(t, h, http.MethodGet, "/api/notes/1", ""))
	if len(got.Annotations) != 2 || got.Annotations[1].Author != "human" {
		t.Errorf("annotations = %+v", got.Annotations)
	}

	for _, body := range []string{
		`{"kind":"essay","body":"x"}`,
		`{"kind":"","body":"x"}`,
		`{"kind":"memo","body":""}`,
		`{"kind":"memo","body":"x","author":"bot"}`,
		`oops`,
	} {
		wantError(t, call(t, h, http.MethodPost, "/api/notes/1/annotations", body), http.StatusBadRequest)
	}
}

func TestSetTopic(t *testing.T) {
	h, store := newServer(t)
	_, _ = store.Add("a", "")

	rec := call(t, h, http.MethodPut, "/api/notes/1/topic", `{"topic":"青分離"}`)
	wantStatus(t, rec, http.StatusOK)
	if n := decodeBody[note.Note](t, rec); n.Topic != "青分離" {
		t.Errorf("topic = %q", n.Topic)
	}
	rec = call(t, h, http.MethodPut, "/api/notes/1/topic", `{"topic":""}`)
	wantStatus(t, rec, http.StatusOK)
	if n := decodeBody[note.Note](t, rec); n.Topic != "" {
		t.Errorf("topic should be cleared, got %q", n.Topic)
	}
	wantError(t, call(t, h, http.MethodPut, "/api/notes/1/topic", `nope`), http.StatusBadRequest)

	// A missing or null topic must not clear it: only an explicit "" does.
	call(t, h, http.MethodPut, "/api/notes/1/topic", `{"topic":"残る"}`)
	for _, body := range []string{`{}`, `{"topic":null}`, `{"name":"x"}`} {
		wantError(t, call(t, h, http.MethodPut, "/api/notes/1/topic", body), http.StatusBadRequest)
	}
	if n, _ := store.Get(1); n.Topic != "残る" {
		t.Errorf("a rejected request changed the topic to %q", n.Topic)
	}
}

func TestTopics(t *testing.T) {
	h, _ := newServer(t)
	rec := call(t, h, http.MethodGet, "/api/topics", "")
	wantStatus(t, rec, http.StatusOK)
	if got := decodeBody[TopicsResponse](t, rec); got.Topics == nil || len(got.Topics) != 0 {
		t.Errorf("empty topics should be [], got %s", rec.Body.String())
	}

	rec = call(t, h, http.MethodPost, "/api/topics", `{"name":" C&C "}`)
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeBody[TopicsResponse](t, rec); len(got.Topics) != 1 || got.Topics[0] != "C&C" {
		t.Errorf("topics = %+v", got)
	}
	for _, body := range []string{`{"name":"C&C"}`, `{"name":""}`, `{"name":"  "}`, `nope`} {
		wantError(t, call(t, h, http.MethodPost, "/api/topics", body), http.StatusBadRequest)
	}
}

func TestStoreFailureIs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := note.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	New(store).Mount(r)
	// A directory where the state file goes makes every write fail.
	if err := os.Mkdir(dir+"/notes.json", 0o700); err != nil {
		t.Fatal(err)
	}
	wantError(t, call(t, r, http.MethodPost, "/api/notes", `{"text":"a"}`), http.StatusInternalServerError)
}

func TestAddNoteWithLinks(t *testing.T) {
	h, store := newServer(t)
	rec := call(t, h, http.MethodPost, "/api/notes", `{"text":"[1] を見る","links":[{"url":"https://x.slack.com/a/1","label":"スレッド"}]}`)
	wantStatus(t, rec, http.StatusCreated)
	n := decodeBody[note.Note](t, rec)
	if len(n.Links) != 1 || n.Links[0] != (note.Link{N: 1, URL: "https://x.slack.com/a/1", Label: "スレッド"}) {
		t.Errorf("links = %+v", n.Links)
	}

	for _, body := range []string{
		`{"text":"a","links":[{"url":"javascript:alert(1)"}]}`,
		`{"text":"[2]","links":[{"url":"https://x.example/"}]}`,
		`{"text":"a","links":"https://x.example/"}`,
	} {
		wantError(t, call(t, h, http.MethodPost, "/api/notes", body), http.StatusBadRequest)
	}
	if got := len(store.List(note.Filter{})); got != 1 {
		t.Errorf("rejected requests must store nothing; have %d memos", got)
	}
}
