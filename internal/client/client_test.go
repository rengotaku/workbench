package client_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/todo-order/internal/client"
)

// recorder captures what the CLI actually sent, so the tests can assert on the
// request shape rather than trusting the method names.
type recorder struct {
	body   map[string]any
	method string
	path   string
	query  string
}

func serve(t *testing.T, status int, response string) (*client.Client, *recorder) {
	t.Helper()
	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		if raw, err := io.ReadAll(r.Body); err == nil && len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)

	c, err := client.New(srv.URL)
	require.NoError(t, err)
	return c, rec
}

func TestNew_RejectsSomethingThatIsNotAURL(t *testing.T) {
	_, err := client.New("not a url")
	assert.Error(t, err)
}

func TestNew_FallsBackToTheDefaultAddress(t *testing.T) {
	t.Setenv("TODO_ORDER_URL", "")
	c, err := client.New("")
	require.NoError(t, err)
	assert.Equal(t, client.DefaultBaseURL, c.BaseURL())
}

func TestNew_PrefersTheEnvironmentOverTheDefault(t *testing.T) {
	t.Setenv("TODO_ORDER_URL", "http://127.0.0.1:9999/")
	c, err := client.New("")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:9999", c.BaseURL(), "a trailing slash is trimmed")
}

func TestList_PassesTheScopeThrough(t *testing.T) {
	c, rec := serve(t, http.StatusOK,
		`{"today":"2026-09-10","scope_days":7,"queue":[{"id":"T001","name":"A","due_label":"あと2日"}],"counts":{"over":1,"near":2,"rest":3}}`)

	board, _, err := c.List("14")
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, rec.method)
	assert.Equal(t, "/api/tasks", rec.path)
	assert.Equal(t, "scope=14", rec.query)
	require.Len(t, board.Queue, 1)
	assert.Equal(t, "T001", board.Queue[0].ID)
	assert.Equal(t, "あと2日", board.Queue[0].DueLabel)
	assert.Equal(t, 1, board.Counts.Over)
	assert.Equal(t, 3, board.Counts.Rest)
}

func TestAdd_SendsTheNameDetailAndDue(t *testing.T) {
	c, rec := serve(t, http.StatusOK, `{"task":{"id":"T007","name":"やる"}}`)

	got, _, err := c.Add("やる", "詳しい話", "2026-09-12")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, rec.method)
	assert.Equal(t, "/api/tasks", rec.path)
	assert.Equal(t, "やる", rec.body["name"])
	assert.Equal(t, "詳しい話", rec.body["detail"])
	assert.Equal(t, "2026-09-12", rec.body["due"])
	assert.Equal(t, "T007", got.ID)
}

// The event log tells a person's change from an assistant's, so the CLI has to
// declare which it is.
func TestActor_IsSentAsAIWhenTheEnvironmentSaysSo(t *testing.T) {
	t.Setenv("TODO_ORDER_ACTOR", "ai")
	c, rec := serve(t, http.StatusOK, `{"task":{"id":"T001"}}`)

	_, _, err := c.Add("AI が入れた", "", "")
	require.NoError(t, err)
	assert.Equal(t, "ai", rec.body["actor"])
}

func TestActor_DefaultsToHuman(t *testing.T) {
	t.Setenv("TODO_ORDER_ACTOR", "")
	c, rec := serve(t, http.StatusOK, `{"task":{"id":"T001"}}`)

	_, _, err := c.Add("自分で入れた", "", "")
	require.NoError(t, err)
	assert.Equal(t, "human", rec.body["actor"])
}

func TestWaitResumeOrderDueDetail_HitTheRightEndpoints(t *testing.T) {
	cases := []struct {
		call       func(c *client.Client) error
		wantBody   map[string]any
		name       string
		wantPath   string
		wantMethod string
	}{
		{
			name:       "wait",
			call:       func(c *client.Client) error { _, _, err := c.Wait("T001", "相手", "回答待ち"); return err },
			wantPath:   "/api/tasks/T001/wait",
			wantMethod: http.MethodPost,
			wantBody:   map[string]any{"to": "相手", "note": "回答待ち"},
		},
		{
			name:       "resume",
			call:       func(c *client.Client) error { _, _, err := c.Resume("T001", "返ってきた"); return err },
			wantPath:   "/api/tasks/T001/resume",
			wantMethod: http.MethodPost,
			wantBody:   map[string]any{"note": "返ってきた"},
		},
		{
			name:       "order",
			call:       func(c *client.Client) error { _, _, err := c.Order("T001", 0, 7, 0); return err },
			wantPath:   "/api/tasks/T001/order",
			wantMethod: http.MethodPost,
			wantBody:   map[string]any{"to": float64(0), "scope_days": float64(7)},
		},
		{
			name:       "due",
			call:       func(c *client.Client) error { _, _, err := c.SetDue("T001", "2026-09-12"); return err },
			wantPath:   "/api/tasks/T001",
			wantMethod: http.MethodPatch,
			wantBody:   map[string]any{"due": "2026-09-12"},
		},
		{
			name:       "detail",
			call:       func(c *client.Client) error { _, _, err := c.SetDetail("T001", "詳しい話"); return err },
			wantPath:   "/api/tasks/T001",
			wantMethod: http.MethodPatch,
			wantBody:   map[string]any{"detail": "詳しい話"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := serve(t, http.StatusOK, `{"task":{"id":"T001"}}`)
			require.NoError(t, tc.call(c))

			assert.Equal(t, tc.wantMethod, rec.method)
			assert.Equal(t, tc.wantPath, rec.path)
			for k, want := range tc.wantBody {
				assert.Equal(t, want, rec.body[k], k)
			}
		})
	}
}

func TestDone_MarksTheActorInTheQuery(t *testing.T) {
	t.Setenv("TODO_ORDER_ACTOR", "")
	c, rec := serve(t, http.StatusOK, `{"task":{"id":"T001","state":"done"}}`)

	got, _, err := c.Done("T001")
	require.NoError(t, err)
	assert.Equal(t, "/api/tasks/T001/done", rec.path)
	assert.Equal(t, "actor=human", rec.query)
	assert.EqualValues(t, "done", got.State)
}

func TestContext_ReturnsTheTaskWithItsEvents(t *testing.T) {
	c, _ := serve(t, http.StatusOK,
		`{"task":{"id":"T001","name":"A"},"events":[{"id":"E0001","kind":"created"},{"id":"E0002","kind":"reordered","from_order":2,"to_order":0}]}`)

	tk, events, _, err := c.Context("T001")
	require.NoError(t, err)
	assert.Equal(t, "T001", tk.ID)
	require.Len(t, events, 2)
	assert.Equal(t, "E0002", events[1].ID)
	assert.Equal(t, 2, events[1].FromOrder)
}

func TestAsk_ReturnsThePendingChanges(t *testing.T) {
	c, _ := serve(t, http.StatusOK,
		`{"pending":[{"event":{"id":"E0002"},"task":{"id":"T001"},"trigger":"1位に上げた"}]}`)

	pending, _, err := c.Ask()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "1位に上げた", pending[0].Trigger)
	assert.Equal(t, "E0002", pending[0].Event.ID)
}

func TestWhy_PostsTheReason(t *testing.T) {
	c, rec := serve(t, http.StatusOK, `{"event":{"id":"E0002","reason":"切替日が動いた"}}`)

	e, _, err := c.Why("E0002", "切替日が動いた")
	require.NoError(t, err)
	assert.Equal(t, "/api/events/E0002/why", rec.path)
	assert.Equal(t, "切替日が動いた", rec.body["reason"])
	assert.Equal(t, "切替日が動いた", e.Reason)
}

// A refusal from the server has to reach the caller as an error: a mistyped ID
// must not look like a completed command.
func TestServerError_IsReportedWithTheServersMessage(t *testing.T) {
	c, _ := serve(t, http.StatusNotFound, `{"error":"task T999: not found"}`)

	_, _, err := c.Done("T999")
	assert.ErrorContains(t, err, "task T999: not found")
}

func TestServerError_WithoutAMessageStillFails(t *testing.T) {
	c, _ := serve(t, http.StatusInternalServerError, `plain text failure`)

	_, _, err := c.Done("T001")
	assert.ErrorContains(t, err, "500")
}

func TestBadJSON_IsReportedRatherThanIgnored(t *testing.T) {
	c, _ := serve(t, http.StatusOK, `{not json`)

	_, _, err := c.List("")
	assert.ErrorContains(t, err, "parsing response")
}

// With no server there is no local fallback: writing to the files directly
// would split the source of truth in two, so the command has to stop.
func TestUnreachableServer_StopsWithAnActionableMessage(t *testing.T) {
	c, err := client.New("http://127.0.0.1:1")
	require.NoError(t, err)

	_, _, err = c.List("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot reach the todo-order server")
	assert.Contains(t, err.Error(), "make run")
}
