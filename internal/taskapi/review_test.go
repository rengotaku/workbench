package taskapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

// Each reason alone and together (ADR todo-0022).
func TestLongView_ReviewReasons(t *testing.T) {
	const today = "2026-10-28"
	long := func(reviewOn string, idle, kidsOpen int) (View, map[string]*kidCount) {
		v := View{Task: task.Task{ID: "T001", State: task.StateLong, ReviewOn: reviewOn}, IdleDays: idle}
		return v, map[string]*kidCount{"T001": {open: kidsOpen}}
	}
	cases := []struct {
		name     string
		reviewOn string
		want     []string
		idle     int
		kidsOpen int
	}{
		{"nothing", "2026-11-02", nil, 0, 2},
		{"no next action only", "2026-11-02", []string{"no_next"}, 3, 0},
		{"review date reached", "2026-10-28", []string{"review_due"}, 3, 1},
		{"review date passed", "2026-10-20", []string{"review_due"}, 0, 1},
		{"review date missing counts as due", "", []string{"review_due"}, 0, 1},
		{"stalled only", "2026-11-02", []string{"stalled"}, 8, 1},
		{"7 idle days is not stalled", "2026-11-02", nil, 7, 1},
		{"all three", "", []string{"no_next", "review_due", "stalled"}, 9, 0},
		{"no next and stalled", "2026-11-02", []string{"no_next", "stalled"}, 8, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, kids := long(c.reviewOn, c.idle, c.kidsOpen)
			v.KidsOpen = c.kidsOpen
			lv := longView(v, kids, today)
			assert.Equal(t, c.want, lv.ReviewReasons)
			assert.Equal(t, len(c.want) > 0, lv.NeedsReview)
		})
	}
}

// For a long-run task a review counts as a person touching it.
func TestIdleDays_ReviewResetsALongRunTask(t *testing.T) {
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	a := New(store, time.Local)
	today := dayOf(time.Now(), time.Local)
	reviewed := today.AddDate(0, 0, -3).Add(9 * time.Hour)

	lt := task.Task{State: task.StateLong, CreatedAt: today.AddDate(0, 0, -20), ReviewedAt: &reviewed}
	days, from := a.idleDays(lt, today)
	assert.Equal(t, 3, days)
	assert.Equal(t, "review", from)

	// The same fields on a task that is not long-run change nothing.
	lt.State = task.StateOpen
	days, from = a.idleDays(lt, today)
	assert.Equal(t, 20, days)
	assert.Equal(t, "created", from)
}
