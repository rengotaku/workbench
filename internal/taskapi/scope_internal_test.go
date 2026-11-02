package taskapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The week scopes end on Sunday whatever day it is, so a task due next Monday
// stays out all week instead of coming in a day at a time.
func TestScopeDays_WeeksEndOnSunday(t *testing.T) {
	cases := []struct {
		today     string
		week, two int
	}{
		{"2026-10-19", 6, 13}, // Monday
		{"2026-10-21", 4, 11}, // Wednesday
		{"2026-10-24", 1, 8},  // Saturday
		{"2026-10-25", 0, 7},  // Sunday closes the week that began on Monday
		{"2026-10-31", 1, 8},  // Saturday across a month end
	}
	for _, c := range cases {
		today, err := time.ParseInLocation("2006-01-02", c.today, time.Local)
		assert.NoError(t, err)
		week, err := scopeDays("week", today)
		assert.NoError(t, err)
		assert.Equal(t, c.week, week, c.today)
		two, err := scopeDays("2weeks", today)
		assert.NoError(t, err)
		assert.Equal(t, c.two, two, c.today)
		def, err := scopeDays("", today)
		assert.NoError(t, err)
		assert.Equal(t, c.week, def, "the default is this week: "+c.today)
	}
}

func TestScopeDays_NumbersAndAllStillWork(t *testing.T) {
	today := time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)
	n, err := scopeDays("10", today)
	assert.NoError(t, err)
	assert.Equal(t, 10, n)
	all, err := scopeDays("all", today)
	assert.NoError(t, err)
	assert.Equal(t, 36500, all)
	_, err = scopeDays("month", today)
	assert.Error(t, err)
}

func TestFromLabel(t *testing.T) {
	assert.Equal(t, "11/5 から", fromLabel(time.Date(2026, 11, 5, 0, 0, 0, 0, time.Local)))
	assert.Equal(t, "12/20 から", fromLabel(time.Date(2026, 12, 20, 0, 0, 0, 0, time.Local)))
}
