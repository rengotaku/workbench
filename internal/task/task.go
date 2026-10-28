// Package task holds the task model and its append-only event log.
//
// A task carries only what the list needs: a name (the主), an optional detail,
// an optional URL it was pasted from, its place in the order, and its state.
// Any ticket key inside Name is plain text — nothing here treats it specially.
package task

import (
	"fmt"
	"time"
)

// State is where a task sits: in the queue, handed to someone else, or done.
type State string

const (
	StateOpen    State = "open"
	StateWaiting State = "waiting"
	StateSomeday State = "someday"
	// StateLong is a job that runs for months. It sits in its own band above
	// the focus band, out of the queue (ADR todo-0019).
	StateLong State = "long"
	StateDone State = "done"
)

// Actor says who made a change, so the event log can tell a person's
// reordering apart from one an assistant made through the API.
type Actor string

const (
	ActorHuman Actor = "human"
	ActorAI    Actor = "ai"
)

// Kind labels an event. The set is closed: adding a new kind means deciding
// whether it needs a reason (see NeedsReason).
type Kind string

const (
	KindCreated   Kind = "created"
	KindReordered Kind = "reordered"
	KindDueSet    Kind = "due_set"
	KindWaiting   Kind = "waiting"
	KindResumed   Kind = "resumed"
	KindShelved   Kind = "shelved"
	KindDone      Kind = "done"
	KindReopened  Kind = "reopened"
	KindDetail    Kind = "detail_edited"
	KindRemoved   Kind = "removed"
	KindRenamed   Kind = "renamed"
	KindLinked    Kind = "linked"
	// KindLinks is a change to the task's related URL list; from/to hold the
	// old and new lists as JSON.
	KindLinks     Kind = "links_set"
	KindFocused   Kind = "focused"
	KindUnfocused Kind = "unfocused"
	// KindLonged / KindUnlonged move a task into and out of the long-run band.
	KindLonged   Kind = "longed"
	KindUnlonged Kind = "unlonged"
	// KindParentSet ties a task to a long-run task; from/to hold the old and
	// new parent IDs.
	KindParentSet Kind = "parent_set"
	// KindReviewSet moves a long-run task's next review date; from/to hold the
	// old and new dates, and the snapshot the task as it was (ADR todo-0022).
	KindReviewSet Kind = "review_set"
	// KindAnnotated is a supplement appended to a task. It is not undoable:
	// see ADR todo-0010.
	KindAnnotated Kind = "annotated"
)

// Annotation kinds: the three a note has, plus progress and blocked, the two
// a person writes as a status line (ADR todo-0017).
const (
	AnnotationQuote    = "quote"
	AnnotationAnalysis = "analysis"
	AnnotationMemo     = "memo"
	AnnotationProgress = "progress"
	AnnotationBlocked  = "blocked"
)

// Annotation is one appended supplement to a task. None is ever edited or
// removed.
type Annotation struct {
	CreatedAt time.Time `json:"created_at"`
	Kind      string    `json:"kind"`
	Body      string    `json:"body"`
	Source    string    `json:"source,omitempty"`
	Author    Actor     `json:"author"`
	ID        int       `json:"id"`
}

// Task is the current state of one entry. Callers never mutate a Task in
// place; Store hands out copies and applies changes through its own methods.
type Task struct {
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Detail      string     `json:"detail,omitempty"`
	// URL is the link the task came from, kept exactly as it was pasted. The
	// app does not parse it: it has no idea which tracker, wiki or document a
	// link points at, and nothing here reconstructs a URL from the name.
	URL string `json:"url,omitempty"`
	// LinkText is the part of Name that was the link where it was copied from.
	// Keeping it means the link covers the same words it covered at the source
	// — a tracker that links only the key leaves the title outside it — without
	// this package knowing what a key looks like.
	LinkText string `json:"link_text,omitempty"`
	State    State  `json:"state"`
	Due      string `json:"due,omitempty"`
	// Until is a someday task's "don't look before this" date. It answers a
	// different question than Due: Due is a promise, Until is a reminder to
	// come back. The two never share a colour or a count.
	Until    string `json:"until,omitempty"`
	HandedTo string `json:"handed_to,omitempty"`
	HandedOn string `json:"handed_on,omitempty"`
	WaitNote string `json:"wait_note,omitempty"`
	// Parent is the long-run task this one was cut out of; empty when none.
	// One level only: a long-run task is never a child (ADR todo-0019).
	Parent string `json:"parent,omitempty"`
	// ReviewOn is the day a long-run task is next looked at (ADR todo-0022);
	// empty counts as due. ReviewedAt is when somebody last reviewed it.
	ReviewOn   string     `json:"review_on,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	// Annotations are appended supplements, oldest first. Undo leaves them.
	Annotations []Annotation `json:"annotations,omitempty"`
	// Links are the related URLs the name refers to as [1], [2] …. The old
	// single URL above is left as it is on tasks that have it (ADR todo-0008).
	Links []Link `json:"links,omitempty"`
	Order int    `json:"order"`
	// Focused lifts a task into the band at the top of the page. It is a
	// working set, not a state: the task keeps its place in the queue.
	Focused bool `json:"focused,omitempty"`
	// SourceNoteID is the memo this task was made from; 0 when it was not made
	// from one. It is what lets a retry find the task it already made.
	SourceNoteID int `json:"source_note_id,omitempty"`
}

// Event is one entry in the append-only log for a task. Reason is empty until
// somebody explains the change; Store never invents one.
type Event struct {
	At        time.Time    `json:"at"`
	ReasonAt  *time.Time   `json:"reason_at,omitempty"`
	Snapshot  *Task        `json:"snapshot,omitempty"`
	FromValue string       `json:"from_value,omitempty"`
	Actor     Actor        `json:"actor"`
	Kind      Kind         `json:"kind"`
	TaskID    string       `json:"task_id"`
	ToValue   string       `json:"to_value,omitempty"`
	Reason    string       `json:"reason,omitempty"`
	ID        string       `json:"id"`
	Jumped    []JumpedTask `json:"jumped,omitempty"`
	// Moved lists every task whose queue position changed because of this
	// event, with the position it had before. Events written before it was
	// added lack it; undo then moves only the event's own task.
	Moved     []MovedTask `json:"moved,omitempty"`
	FromOrder int         `json:"from_order,omitempty"`
	ToOrder   int         `json:"to_order,omitempty"`
}

// MovedTask is a task's queue position before an event moved it.
type MovedTask struct {
	ID    string `json:"id"`
	Order int    `json:"order"`
}

// JumpedTask records a task that this reorder overtook, with the due date that
// made overtaking it worth explaining.
type JumpedTask struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Due  string `json:"due"`
}

// AskThresholdDays is the stall length past which a waiting task is worth
// asking about (condition 4).
const AskThresholdDays = 14

// NeedsReason reports whether an event is one of the changes agreed as worth
// explaining: the three reorder shapes, and dropping a task. Everything else —
// swapping neighbours, setting a due date, finishing — is left alone, so
// reordering stays cheap.
//
// queueLen is the number of open tasks at the time of the event.
func (e Event) NeedsReason(queueLen int) bool {
	if e.Reason != "" {
		return false
	}
	switch e.Kind {
	case KindReordered:
		if e.ToOrder == 0 && e.FromOrder != 0 {
			return true // moved to the top
		}
		if queueLen > 0 && e.ToOrder >= queueLen-1 && e.FromOrder < queueLen-1 {
			return true // dropped to the bottom
		}
		return len(e.Jumped) > 0 // overtook something with a due date in scope
	case KindRemoved:
		// Dropping a task is the one change that loses it. The reason is not
		// asked for at the click — see ADR 0004 — but it is worth asking
		// afterwards, the same way a reorder to the top is.
		return true
	case KindWaiting:
		return false // asked about only once it has stalled; see StalledFor
	default:
		return false
	}
}

// AskReason explains, in one line, why an event is in the question queue. It
// exists so the assistant can quote the trigger instead of guessing at it.
func (e Event) AskReason(queueLen int) string {
	switch {
	case e.Kind == KindReordered && e.ToOrder == 0 && e.FromOrder != 0:
		return "1位に上げた"
	case e.Kind == KindReordered && queueLen > 0 && e.ToOrder >= queueLen-1 && e.FromOrder < queueLen-1:
		return "最下位へ落とした"
	case e.Kind == KindReordered && len(e.Jumped) > 0:
		return fmt.Sprintf("期限が近い %d 件を飛び越えた", len(e.Jumped))
	case e.Kind == KindRemoved:
		return "やめた（削除した）"
	default:
		return ""
	}
}

// StalledFor returns how many days a waiting task has been with someone else,
// counted from the first handoff. It reports false when the task is not
// waiting or has no recorded handoff date.
func (t Task) StalledFor(today time.Time) (int, bool) {
	if t.State != StateWaiting || t.HandedOn == "" {
		return 0, false
	}
	on, err := time.ParseInLocation(DateLayout, t.HandedOn, today.Location())
	if err != nil {
		return 0, false
	}
	days := int(today.Sub(on).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, true
}

// DateLayout is the only date format stored or accepted. Timestamps keep full
// precision; dates a person types stay plain.
const DateLayout = "2006-01-02"

// ParseDate validates a YYYY-MM-DD string, returning an error rather than a
// zero value so callers cannot mistake a bad date for "no date".
func ParseDate(s string, loc *time.Location) (time.Time, error) {
	d, err := time.ParseInLocation(DateLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("date must be YYYY-MM-DD: %q", s)
	}
	return d, nil
}
