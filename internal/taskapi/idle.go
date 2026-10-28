package taskapi

import (
	"time"

	"github.com/rengotaku/workbench/internal/task"
)

// idleStages are the steps the page colours "idle days" by, highest first
// (ADR todo-0021). The first step has no colour of its own.
var idleStages = []int{8, 5, 3, 2, 1}

// describeIdle fills in IdleDays, IdleStage and IdleFrom for a task.
func (a *API) describeIdle(v *View, t task.Task, today time.Time) {
	days, from := a.idleDays(t, today)
	if days == 0 {
		return
	}
	v.IdleDays, v.IdleFrom = days, from
	for _, st := range idleStages {
		if days >= st {
			v.IdleStage = st
			return
		}
	}
}

// idleDays counts the calendar days since a person last dealt with an open or
// long-run task, and says which date it counted from. Only a person's
// annotation counts as touching it. A task with a due date counts from the
// later of the due date and that annotation; one without, from the later of
// its creation and that annotation. It returns 0 when there is nothing to
// show: other states, a start that is today or ahead, or no start known.
func (a *API) idleDays(t task.Task, today time.Time) (int, string) {
	if t.State != task.StateOpen && t.State != task.StateLong {
		return 0, ""
	}

	var human time.Time
	touch := "annotation"
	for _, an := range t.Annotations {
		if an.Author == task.ActorHuman && an.CreatedAt.After(human) {
			human = an.CreatedAt
		}
	}
	// For a long-run task a review is a person looking at it (ADR todo-0022).
	if t.State == task.StateLong && t.ReviewedAt != nil && t.ReviewedAt.After(human) {
		human, touch = *t.ReviewedAt, "review"
	}

	var start time.Time
	from := ""
	if t.Due != "" {
		d, err := task.ParseDate(t.Due, a.loc)
		if err != nil {
			return 0, ""
		}
		start, from = d, "due"
	} else {
		created := t.CreatedAt
		if created.IsZero() {
			created = a.firstEvent(t.ID)
		}
		if created.IsZero() {
			// No creation time and no history: the start is unknown, so
			// nothing is shown rather than a made-up number.
			if human.IsZero() {
				return 0, ""
			}
		} else {
			start, from = dayOf(created, a.loc), "created"
		}
	}
	if !human.IsZero() {
		if h := dayOf(human, a.loc); start.IsZero() || h.After(start) {
			start, from = h, touch
		}
	}

	days := int(today.Sub(start).Hours()/24 + 0.5)
	if days <= 0 {
		return 0, ""
	}
	return days, from
}

// firstEvent is when the earliest event for a task was written, for a task
// that predates creation times. The zero time means there is none.
func (a *API) firstEvent(id string) time.Time {
	events, err := a.store.Events(id)
	if err != nil || len(events) == 0 {
		return time.Time{}
	}
	first := events[0].At
	for _, e := range events[1:] {
		if e.At.Before(first) {
			first = e.At
		}
	}
	return first
}

// dayOf is the calendar day an instant falls on, at midnight in loc.
func dayOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
