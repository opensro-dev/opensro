/*
===========================================================================

schedule.go - the shard's schedule.cfg, and when each schedule is running

SR_ShardManager reads schedule.cfg at boot and tells the game servers when
each named schedule starts and ends; the GameServer's schedule jobs
(CGameSJ_SiegeRequestPeriod 679320/6794C0, CGameSJ_SiegePeriod
679720/6798B0, CGame_SiegeTaxPeriod 679B00/679CA0) act on those edges.
This server is both processes, so it evaluates the same file itself.

A schedule is a block:

	Schedule Name
	{
		DurationBegin	2006-12-13, 00:00:00
		DurationEnd	2030-12-31, 00:00:00
		Weekly		1, {3, }	// every week, on Wednesday (0 = Sunday)
		Daily		1,1		// or: every day
		Once		20:00:00, 21:30:00
	}

It runs, inside its duration, on the matching days, from the Once start up
to (not including) the Once end, in the shard's local time.

===========================================================================
*/
package siege

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"
)

//go:embed data/schedule.cfg
var shippedSchedule string

const (
	scheduleDateLayout = "2006-01-02, 15:04:05"
)

/*
================
Schedule

One parsed block. Days is a Weekly block's weekday set; a Daily block has
none and runs every EveryDays days.
================
*/
type Schedule struct {
	Name       string
	Begin, End time.Time
	Weekly     bool
	EveryWeeks int
	Days       map[time.Weekday]bool
	EveryDays  int
	// StartSecond and EndSecond are the Once window, seconds after midnight.
	StartSecond, EndSecond int
}

/*
================
ParseSchedules

Every block in a schedule.cfg text, keyed by name. Comments (//) and blank
lines are ignored; dates and clocks are read in loc.
================
*/
func ParseSchedules(text string, loc *time.Location) (map[string]Schedule, error) {
	out := map[string]Schedule{}
	var current *Schedule
	for index, raw := range strings.Split(text, "\n") {
		line := raw
		if cut := strings.Index(line, "//"); cut >= 0 {
			line = line[:cut]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fail := func(format string, args ...any) error {
			return fmt.Errorf("schedule.cfg line %d: %s", index+1, fmt.Sprintf(format, args...))
		}
		switch {
		case strings.HasPrefix(line, "Schedule "):
			if current != nil {
				return nil, fail("schedule %s is not closed", current.Name)
			}
			current = &Schedule{Name: strings.TrimSpace(strings.TrimPrefix(line, "Schedule "))}
		case line == "{":
			if current == nil {
				return nil, fail("block without a schedule name")
			}
		case line == "}":
			if current == nil {
				return nil, fail("unmatched }")
			}
			if err := validSchedule(*current); err != nil {
				return nil, fail("%v", err)
			}
			if _, dup := out[current.Name]; dup {
				return nil, fail("schedule %s defined twice", current.Name)
			}
			out[current.Name] = *current
			current = nil
		default:
			if current == nil {
				return nil, fail("directive outside a schedule")
			}
			if err := parseDirective(current, line, loc); err != nil {
				return nil, fail("%v", err)
			}
		}
	}
	if current != nil {
		return nil, fmt.Errorf("schedule.cfg: schedule %s is not closed", current.Name)
	}
	return out, nil
}

/*
================
parseDirective
================
*/
func parseDirective(s *Schedule, line string, loc *time.Location) error {
	key, value, _ := strings.Cut(line, "\t")
	if strings.Contains(key, " ") {
		key, value, _ = strings.Cut(line, " ")
	}
	value = strings.TrimSpace(value)
	switch key {
	case "DurationBegin", "DurationEnd":
		at, err := time.ParseInLocation(scheduleDateLayout, strings.Join(strings.Fields(value), " "), loc)
		if err != nil {
			return fmt.Errorf("%s %q: %w", key, value, err)
		}
		if key == "DurationBegin" {
			s.Begin = at
		} else {
			s.End = at
		}
	case "Daily":
		every, _, _ := strings.Cut(value, ",")
		n, err := strconv.Atoi(strings.TrimSpace(every))
		if err != nil || n < 1 {
			return fmt.Errorf("daily %q", value)
		}
		s.EveryDays = n
	case "Weekly":
		every, days, found := strings.Cut(value, ",")
		n, err := strconv.Atoi(strings.TrimSpace(every))
		if err != nil || n < 1 || !found {
			return fmt.Errorf("weekly %q", value)
		}
		s.Weekly, s.EveryWeeks, s.Days = true, n, map[time.Weekday]bool{}
		days = strings.Trim(strings.TrimSpace(days), "{}")
		for _, part := range strings.Split(days, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			day, err := strconv.Atoi(part)
			if err != nil || day < 0 || day > 6 {
				return fmt.Errorf("weekly day %q", part)
			}
			s.Days[time.Weekday(day)] = true
		}
	case "Once":
		start, end, found := strings.Cut(value, ",")
		if !found {
			return fmt.Errorf("once %q", value)
		}
		var err error
		if s.StartSecond, err = clockSeconds(start); err != nil {
			return err
		}
		if s.EndSecond, err = clockSeconds(end); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown directive %q", key)
	}
	return nil
}

/*
================
clockSeconds
================
*/
func clockSeconds(text string) (int, error) {
	at, err := time.Parse("15:04:05", strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("clock %q: %w", text, err)
	}
	return at.Hour()*3600 + at.Minute()*60 + at.Second(), nil
}

/*
================
validSchedule
================
*/
func validSchedule(s Schedule) error {
	if s.Begin.IsZero() || s.End.IsZero() || !s.End.After(s.Begin) {
		return fmt.Errorf("schedule %s has no valid duration", s.Name)
	}
	if s.Weekly == (s.EveryDays != 0) {
		return fmt.Errorf("schedule %s needs exactly one of Weekly or Daily", s.Name)
	}
	if s.Weekly && len(s.Days) == 0 {
		return fmt.Errorf("schedule %s runs on no weekday", s.Name)
	}
	if s.EndSecond <= s.StartSecond {
		return fmt.Errorf("schedule %s has an empty Once window", s.Name)
	}
	return nil
}

/*
================
Schedule.Active

Whether the schedule is running at t (read in the duration's location).
================
*/
func (s Schedule) Active(t time.Time) bool {
	t = t.In(s.Begin.Location())
	if t.Before(s.Begin) || !t.Before(s.End) {
		return false
	}
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	second := int(t.Sub(midnight) / time.Second)
	if second < s.StartSecond || second >= s.EndSecond {
		return false
	}
	first := time.Date(s.Begin.Year(), s.Begin.Month(), s.Begin.Day(), 0, 0, 0, 0, t.Location())
	days := int(midnight.Sub(first).Hours()+12) / 24
	if s.Weekly {
		if !s.Days[t.Weekday()] {
			return false
		}
		// Week numbers count from the week holding DurationBegin's Sunday.
		weekStart := first.AddDate(0, 0, -int(first.Weekday()))
		weeks := int(midnight.Sub(weekStart).Hours()+12) / (24 * 7)
		return weeks%s.EveryWeeks == 0
	}
	return days%s.EveryDays == 0
}

// windowSearchDays bounds the next-window search: a weekly schedule repeats
// within a week, every shipped one within a year.
const windowSearchDays = 400

/*
================
Schedule.Window

The window running at t, or else the next one to start: the start and end
instants of that day's Once span. ok is false when none remains inside the
duration.
================
*/
func (s Schedule) Window(t time.Time) (start, end time.Time, ok bool) {
	t = t.In(s.Begin.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	for i := 0; i < windowSearchDays; i++ {
		candidate := day.AddDate(0, 0, i)
		start = candidate.Add(time.Duration(s.StartSecond) * time.Second)
		end = candidate.Add(time.Duration(s.EndSecond) * time.Second)
		if !end.After(t) || !s.Active(start) {
			continue
		}
		return start, end, true
	}
	return time.Time{}, time.Time{}, false
}
