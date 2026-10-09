package noteclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/noteapi"
)

// newClient starts the real API over a temp store and returns a client for it.
func newClient(t *testing.T) *Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := note.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	noteapi.New(store).Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewBaseURL(t *testing.T) {
	t.Setenv("NOTE_SERVER", "")
	c, err := New("")
	if err != nil || c.BaseURL() != DefaultBaseURL {
		t.Errorf("default = %v, %v", c, err)
	}
	t.Setenv("NOTE_SERVER", "http://127.0.0.1:9999/")
	c, err = New("")
	if err != nil || c.BaseURL() != "http://127.0.0.1:9999" {
		t.Errorf("env = %v, %v", c, err)
	}
	c, err = New("http://127.0.0.1:1")
	if err != nil || c.BaseURL() != "http://127.0.0.1:1" {
		t.Errorf("explicit should beat env: %v, %v", c, err)
	}
	for _, bad := range []string{"127.0.0.1:8085", "ftp://x", "http://", "://"} {
		if _, err := New(bad); err == nil {
			t.Errorf("New(%q) should fail", bad)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	c := newClient(t)

	n, raw, err := c.Add("https://x.slack.com/a/1 メモ", "青分離")
	if err != nil || n.ID != 1 || n.URL == "" || !strings.Contains(string(raw), `"id":1`) {
		t.Fatalf("Add = %+v, %s, %v", n, raw, err)
	}
	if _, _, addErr := c.Add("二つ目", ""); addErr != nil {
		t.Fatal(addErr)
	}

	r, raw, err := c.List(note.Filter{Pending: true, Topic: "青分離"})
	if err != nil || len(r.Notes) != 1 || r.Pending != 2 || !strings.Contains(string(raw), `"notes"`) {
		t.Fatalf("List = %+v, %v", r, err)
	}
	if r, _, err = c.List(note.Filter{NoTopic: true}); err != nil || len(r.Notes) != 1 || r.Notes[0].ID != 2 {
		t.Fatalf("List no-topic = %+v, %v", r, err)
	}

	if _, annErr := c.Annotate(1, note.AnnotationInput{Kind: "quote", Body: "引用", Source: "slack"}); annErr != nil {
		t.Fatal(annErr)
	}
	got, raw, err := c.Get(1)
	if err != nil || len(got.Annotations) != 1 || got.Annotations[0].Author != "ai" || len(raw) == 0 {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	moved, err := c.SetTopic(2, "C&C")
	if err != nil || moved.Topic != "C&C" {
		t.Fatalf("SetTopic = %+v, %v", moved, err)
	}
	topics, err := c.Topics()
	if err != nil || len(topics) != 2 {
		t.Fatalf("Topics = %v, %v", topics, err)
	}

	if err := c.Delete(2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Get(2); err == nil || !strings.Contains(err.Error(), "見つかりません") {
		t.Errorf("deleted memo err = %v", err)
	}
}

func TestServerErrorMessageIsPassedOn(t *testing.T) {
	c := newClient(t)
	if _, _, err := c.Add("   ", ""); err == nil || !strings.Contains(err.Error(), "メモが空です") {
		t.Errorf("err = %v", err)
	}
	if _, err := c.Annotate(1, note.AnnotationInput{Kind: "memo", Body: "x"}); err == nil {
		t.Error("annotating a missing memo should fail")
	}
	if _, err := c.SetTopic(1, "x"); err == nil {
		t.Error("topic on a missing memo should fail")
	}
	if err := c.Delete(1); err == nil {
		t.Error("deleting a missing memo should fail")
	}
}

func TestUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there any more
	c, err := New(url)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Add("x", "")
	if err == nil || !strings.Contains(err.Error(), "サーバに接続できません") || !strings.Contains(err.Error(), url) {
		t.Errorf("err = %v", err)
	}
	if _, _, err := c.List(note.Filter{}); err == nil {
		t.Error("List should fail too")
	}
	if _, _, err := c.Get(1); err == nil {
		t.Error("Get should fail too")
	}
	if _, err := c.Annotate(1, note.AnnotationInput{Kind: "memo", Body: "x"}); err == nil {
		t.Error("Annotate should fail too")
	}
	if _, err := c.SetTopic(1, "x"); err == nil {
		t.Error("SetTopic should fail too")
	}
	if _, err := c.Topics(); err == nil {
		t.Error("Topics should fail too")
	}
	if err := c.Delete(1); err == nil {
		t.Error("Delete should fail too")
	}
}

func TestUnexpectedResponses(t *testing.T) {
	status := http.StatusInternalServerError
	body := "boom"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, _ := New(srv.URL)

	if _, _, err := c.Get(1); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("non-JSON error body: err = %v", err)
	}

	status, body = http.StatusOK, "not json"
	if _, _, err := c.Get(1); err == nil || !strings.Contains(err.Error(), "解釈できません") {
		t.Errorf("garbled success body: err = %v", err)
	}
}
