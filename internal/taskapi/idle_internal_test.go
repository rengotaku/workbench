package taskapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/task"
)

func at(day string, hour int) time.Time {
	d, _ := time.ParseInLocation(task.DateLayout, day, time.Local)
	return d.Add(time.Duration(hour) * time.Hour)
}

func note(author task.Actor, day string) task.Annotation {
	return task.Annotation{Author: author, Kind: task.AnnotationMemo, Body: "x", CreatedAt: at(day, 15)}
}

type idleCase struct {
	name      string
	wantFrom  string
	task      task.Task
	wantDays  int
	wantStage int
}

func idle(name string, t task.Task, days, stage int, from string) idleCase {
	return idleCase{name: name, task: t, wantDays: days, wantStage: stage, wantFrom: from}
}

func TestIdleDays(t *testing.T) {
	today := at("2026-10-28", 0)
	human, ai := task.ActorHuman, task.ActorAI
	cases := []idleCase{
		idle("due yesterday, no note", task.Task{State: task.StateOpen, Due: "2026-10-27", CreatedAt: at("2026-10-01", 9)}, 1, 1, "due"),
		idle("due 10/20, human note 10/25", task.Task{State: task.StateOpen, Due: "2026-10-20",
			Annotations: []task.Annotation{note(human, "2026-10-25")}}, 3, 3, "annotation"),
		idle("due ahead", task.Task{State: task.StateOpen, Due: "2026-10-30"}, 0, 0, ""),
		idle("due today", task.Task{State: task.StateOpen, Due: "2026-10-28"}, 0, 0, ""),
		idle("due ahead, old human note", task.Task{State: task.StateOpen, Due: "2026-10-30",
			Annotations: []task.Annotation{note(human, "2026-10-01")}}, 0, 0, ""),
		idle("no due, created yesterday", task.Task{State: task.StateOpen, CreatedAt: at("2026-10-27", 9)}, 1, 1, "created"),
		idle("no due, created 10/23", task.Task{State: task.StateOpen, CreatedAt: at("2026-10-23", 23)}, 5, 5, "created"),
		idle("no due, created today", task.Task{State: task.StateOpen, CreatedAt: at("2026-10-28", 1)}, 0, 0, ""),
		idle("no due, human note after creation", task.Task{State: task.StateOpen, CreatedAt: at("2026-10-01", 9),
			Annotations: []task.Annotation{note(human, "2026-10-26")}}, 2, 2, "annotation"),
		idle("AI note after due does not reset", task.Task{State: task.StateOpen, Due: "2026-10-20",
			Annotations: []task.Annotation{note(ai, "2026-10-27")}}, 8, 8, "due"),
		idle("AI note does not reset creation", task.Task{State: task.StateOpen, CreatedAt: at("2026-10-20", 9),
			Annotations: []task.Annotation{note(ai, "2026-10-27")}}, 8, 8, "created"),
		idle("human note today", task.Task{State: task.StateOpen, Due: "2026-10-20",
			Annotations: []task.Annotation{note(human, "2026-10-28")}}, 0, 0, ""),
		idle("newest human note counts", task.Task{State: task.StateOpen, CreatedAt: at("2026-10-01", 9),
			Annotations: []task.Annotation{note(human, "2026-10-10"), note(human, "2026-10-24"), note(ai, "2026-10-27")}}, 4, 3, "annotation"),
		idle("stage 2", task.Task{State: task.StateOpen, Due: "2026-10-26"}, 2, 2, "due"),
		idle("stage 5 at 7 days", task.Task{State: task.StateOpen, Due: "2026-10-21"}, 7, 5, "due"),
		idle("stage 8 at 8 days", task.Task{State: task.StateOpen, Due: "2026-10-20"}, 8, 8, "due"),
		idle("long run has the fields", task.Task{State: task.StateLong, CreatedAt: at("2026-10-20", 9)}, 8, 8, "created"),
		idle("waiting shows nothing", task.Task{State: task.StateWaiting, Due: "2026-10-01"}, 0, 0, ""),
		idle("someday shows nothing", task.Task{State: task.StateSomeday, Due: "2026-10-01"}, 0, 0, ""),
		idle("done shows nothing", task.Task{State: task.StateDone, Due: "2026-10-01"}, 0, 0, ""),
	}
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	a := New(store, time.Local)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v View
			a.describeIdle(&v, c.task, today)
			assert.Equal(t, c.wantDays, v.IdleDays)
			assert.Equal(t, c.wantStage, v.IdleStage)
			assert.Equal(t, c.wantFrom, v.IdleFrom)
		})
	}
}

// A task from before creation times were kept counts from its first event, and
// shows nothing when there is none.
func TestIdleDays_LegacyTaskWithoutCreationTime(t *testing.T) {
	store, err := task.Open(t.TempDir(), time.Local)
	require.NoError(t, err)
	a := New(store, time.Local)
	today := dayOf(time.Now(), time.Local)

	added, err := store.Add("古い", "", "", task.ActorHuman)
	require.NoError(t, err)
	legacy := added
	legacy.CreatedAt = time.Time{}
	var v View
	a.describeIdle(&v, legacy, today.AddDate(0, 0, 4))
	assert.Equal(t, 4, v.IdleDays, "counts from the created event")
	assert.Equal(t, "created", v.IdleFrom)

	var none View
	a.describeIdle(&none, task.Task{ID: "T999", State: task.StateOpen}, today)
	assert.Zero(t, none.IdleDays, "no creation time and no events")
}
