package duedate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-09-30 is a Wednesday.
var wed = time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local)

func TestParse_Accepted(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"2026-10-07", "2026-10-07"},
		{"10/7", "2026-10-07"},
		{"10/07", "2026-10-07"},
		{"9/1", "2027-09-01"},  // already past this year → next year
		{"9/30", "2026-09-30"}, // today stays this year
		{"今日", "2026-09-30"},
		{"明日", "2026-10-01"},
		{"木", "2026-10-01"},
		{"水", "2026-10-07"}, // same weekday as today → next week
		{"水曜", "2026-10-07"},
		{"水曜日", "2026-10-07"},
		{"火", "2026-10-06"},
		{"来週月", "2026-10-05"},
		{"来週水", "2026-10-07"},
		{"来週日曜", "2026-10-11"},
		{"日", "2026-10-04"},
		{"日曜日", "2026-10-04"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := Parse(c.in, wed)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestParse_Rejected(t *testing.T) {
	for _, in := range []string{"あした", "2026-13-01", "2/30", "13/1", "来週", "来週x", "abc", "2026/10/07"} {
		t.Run(in, func(t *testing.T) {
			_, err := Parse(in, wed)
			assert.Error(t, err)
		})
	}
}
