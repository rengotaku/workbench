// Package duedate turns the short due-date forms a person types at the
// interactive prompt (水, 来週水, 10/7, 明日 …) into the YYYY-MM-DD the server
// stores. The server itself only ever sees the normalized form.
package duedate

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const layout = "2006-01-02"

// Hint is shown next to the prompt so the accepted forms need not be
// remembered.
const Hint = "例: 水 / 来週水 / 10/7 / 明日 / 2026-10-07、空で無し"

var weekdays = map[string]time.Weekday{
	"日": time.Sunday, "月": time.Monday, "火": time.Tuesday, "水": time.Wednesday,
	"木": time.Thursday, "金": time.Friday, "土": time.Saturday,
}

// Parse normalizes in relative to today. An empty input means no due date
// and returns "". A bare weekday is the next such day after today, so typing
// today's weekday lands a week out.
func Parse(in string, today time.Time) (string, error) {
	s := strings.TrimSpace(in)
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	switch s {
	case "":
		return "", nil
	case "今日":
		return day.Format(layout), nil
	case "明日":
		return day.AddDate(0, 0, 1).Format(layout), nil
	}
	if t, err := time.ParseInLocation(layout, s, today.Location()); err == nil {
		return t.Format(layout), nil
	}
	if strings.HasPrefix(s, "来週") {
		wd, ok := weekday(strings.TrimPrefix(s, "来週"))
		if !ok {
			return "", fmt.Errorf("読めない期限: %q", in)
		}
		// Weeks start on Monday: find next week's Monday, then offset.
		sinceMon := (int(day.Weekday()) + 6) % 7
		nextMon := day.AddDate(0, 0, 7-sinceMon)
		return nextMon.AddDate(0, 0, (int(wd)+6)%7).Format(layout), nil
	}
	if wd, ok := weekday(s); ok {
		ahead := (int(wd) - int(day.Weekday()) + 7) % 7
		if ahead == 0 {
			ahead = 7
		}
		return day.AddDate(0, 0, ahead).Format(layout), nil
	}
	if d, ok := monthDay(s, day); ok {
		return d.Format(layout), nil
	}
	return "", fmt.Errorf("読めない期限: %q", in)
}

func weekday(s string) (time.Weekday, bool) {
	if wd, ok := weekdays[s]; ok { // bare "日" must not lose its only rune below
		return wd, true
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "日"), "曜")
	wd, ok := weekdays[s]
	return wd, ok
}

// monthDay reads M/D in the current year, rolling to next year when that
// date has already passed.
func monthDay(s string, day time.Time) (time.Time, bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return time.Time{}, false
	}
	m, err1 := strconv.Atoi(parts[0])
	d, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	year := day.Year()
	t := time.Date(year, time.Month(m), d, 0, 0, 0, 0, day.Location())
	if t.Day() != d { // 2/30 and the like normalize into the next month
		return time.Time{}, false
	}
	if t.Before(day) {
		t = time.Date(year+1, time.Month(m), d, 0, 0, 0, 0, day.Location())
	}
	return t, true
}
