/*
===========================================================================

schedule.go - the EventBot data file: one event per line, daily

The operator's file (SRO_EVENT_BOT_FILE) drives the scheduler. One
row says when an event runs each day, which kind plugs into it, how
many rounds of how long, and exactly what a win pays:

	# an evening raffle, three two-minute rounds, ten silk scrolls
	19:30 raffle rounds=3 round=2m reward=ITEM_ETC_SILK_SCROLLx10 title=Evening Raffle
	# hide & seek: a hider codename and its spots (Name|Region|X|Z)
	20:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 hider=MOB_EVENT_HIDER spot=West_Gate|2499|100|200 title=Hide & Seek
	# random unique: unique codenames and spots, one of each per round
	21:00 randomunique rounds=1 round=30m reward=ITEM_ETC_SILK_SCROLLx10 unique=MOB_RM_TAHOMET spot=Hotan|2500|100|200 title=Random Unique
	# lottery: entering spends one Lottery Ticket (dropped by every
	# unique's death); the winner is drawn at round close
	21:00 lottery rounds=1 round=2m reward=ITEM_ETC_SILK_SCROLLx10 title=Sunday Lottery

Fields after the kind are key=value; title= takes the rest of the
line. The parse is strict like the house's other operator files: one
bad row refuses the whole file, so a typo never runs half a schedule,
and a reward codename the item catalogue does not know refuses it
too. Times are server-local; the schedule feed renders them as the
shard's times.

Port-only, not native.

===========================================================================
*/
package eventbot

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The bounds a schedule row must stay inside: a row outside them is a
// typo, not a plan, and refuses the file.
const (
	maxRounds = 20
	minRound  = 10 * time.Second
	// The longest window an event may hold: Random Unique's
	// reference hunt runs thirty minutes (#3537).
	maxRound      = 30 * time.Minute
	maxRewardN    = 1000
	scheduleClock = "15:04"
)

/*
================
Event

One schedule row: a daily event.
================
*/
type Event struct {
	// At is the daily start, "15:04" server-local.
	At string
	// Kind names the plug (kinds.go registry).
	Kind string
	// Title is the display name; empty takes the kind's title.
	Title  string
	Rounds int
	// RoundLength is one round's entry window.
	RoundLength time.Duration
	// Reward pays a win: exactly this item, this many, never more.
	RewardCodename string
	RewardCount    int
	// Hider is the monster/NPC codename a world kind hides (the
	// file's hider=); empty for kinds that act nowhere.
	Hider string
	// Uniques are the monster codenames a kill kind may spawn (the
	// file's unique= rows, one picked per round); empty for kinds
	// that spawn nothing.
	Uniques []string
	// Spots are the hiding places a world kind rotates through (the
	// file's spot= rows); empty for kinds that act nowhere.
	Spots []Spot
}

/*
================
Spot

One hiding place from the data file: spot=Name|Region|X|Z. The name
is display text (underscores read as spaces) and the only part a
hint ever names - the coordinates place the hider and are never
announced.
================
*/
type Spot struct {
	Name     string
	RegionID int
	X, Z     float64
}

/*
================
DisplayName

The spot's name as players read it.
================
*/
func (s Spot) DisplayName() string {
	return strings.ReplaceAll(s.Name, "_", " ")
}

/*
================
Span

The occurrence's whole run: rounds back to back.
================
*/
func (e Event) Span() time.Duration {
	return time.Duration(e.Rounds) * e.RoundLength
}

/*
================
DisplayTitle
================
*/
func (e Event) DisplayTitle() string {
	if e.Title != "" {
		return e.Title
	}
	if kind, ok := kindByName(e.Kind); ok {
		return kind.Title()
	}
	return e.Kind
}

/*
================
Schedule

The parsed file, in file order.
================
*/
type Schedule struct {
	Events []Event
}

/*
================
LoadScheduleFile

Reads and parses the schedule file. A missing file is an error the
caller logs; the host then runs an empty schedule.
================
*/
func LoadScheduleFile(path string, knownItem func(codename string) bool) (Schedule, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Schedule{}, err
	}
	return ParseSchedule(string(raw), knownItem)
}

/*
================
ParseSchedule

Parses the data file's text. knownItem (nil accepts all) validates
reward codenames against the item catalogue.
================
*/
func ParseSchedule(text string, knownItem func(codename string) bool) (Schedule, error) {
	var schedule Schedule
	for lineNo, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		event, err := parseScheduleLine(line)
		if err != nil {
			return Schedule{}, fmt.Errorf("eventbot schedule line %d: %w", lineNo+1, err)
		}
		if knownItem != nil && !knownItem(event.RewardCodename) {
			return Schedule{}, fmt.Errorf("eventbot schedule line %d: reward item %q is not in the item catalogue", lineNo+1, event.RewardCodename)
		}
		schedule.Events = append(schedule.Events, event)
	}
	return schedule, nil
}

/*
================
parseScheduleLine
================
*/
func parseScheduleLine(line string) (Event, error) {
	event := Event{Rounds: 1}
	rest := line
	// title= takes the rest of the line, spaces included.
	if at := strings.Index(rest, "title="); at >= 0 {
		event.Title = strings.TrimSpace(rest[at+len("title="):])
		rest = strings.TrimSpace(rest[:at])
	}
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return Event{}, fmt.Errorf("want HH:MM, a kind and key=value fields, got %q", line)
	}
	if _, err := time.Parse(scheduleClock, fields[0]); err != nil {
		return Event{}, fmt.Errorf("start %q is not HH:MM", fields[0])
	}
	event.At = fields[0]
	event.Kind = fields[1]
	if _, ok := kindByName(event.Kind); !ok {
		return Event{}, fmt.Errorf("unknown event kind %q", event.Kind)
	}
	seen := map[string]bool{}
	for _, field := range fields[2:] {
		key, value, found := strings.Cut(field, "=")
		// spot= and unique= repeat (a world kind's places and
		// monsters); every other field is single.
		if !found || (key != "spot" && key != "unique" && seen[key]) {
			return Event{}, fmt.Errorf("bad field %q", field)
		}
		seen[key] = true
		switch key {
		case "unique":
			if value == "" {
				return Event{}, fmt.Errorf("unique %q is empty", field)
			}
			event.Uniques = append(event.Uniques, value)
		case "hider":
			if value == "" {
				return Event{}, fmt.Errorf("hider %q is empty", field)
			}
			event.Hider = value
		case "spot":
			spot, err := parseSpot(value)
			if err != nil {
				return Event{}, err
			}
			event.Spots = append(event.Spots, spot)
		case "rounds":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > maxRounds {
				return Event{}, fmt.Errorf("rounds %q is not 1..%d", value, maxRounds)
			}
			event.Rounds = n
		case "round":
			d, err := time.ParseDuration(value)
			if err != nil || d < minRound || d > maxRound {
				return Event{}, fmt.Errorf("round %q is not %s..%s", value, minRound, maxRound)
			}
			event.RoundLength = d
		case "reward":
			// The count follows the LAST x: a codename may hold one.
			at := strings.LastIndexByte(value, 'x')
			if at < 1 {
				return Event{}, fmt.Errorf("reward %q is not ITEM_CODENAMEx1..%d", value, maxRewardN)
			}
			n, err := strconv.Atoi(value[at+1:])
			if err != nil || n < 1 || n > maxRewardN {
				return Event{}, fmt.Errorf("reward %q is not ITEM_CODENAMEx1..%d", value, maxRewardN)
			}
			event.RewardCodename, event.RewardCount = value[:at], n
		default:
			return Event{}, fmt.Errorf("unknown field %q", key)
		}
	}
	if event.RoundLength == 0 {
		return Event{}, fmt.Errorf("no round= length")
	}
	if event.RewardCodename == "" {
		return Event{}, fmt.Errorf("no reward= item")
	}
	// A world kind refuses a row it cannot run (no hider, no spots).
	if kind, ok := kindByName(event.Kind); ok {
		if wk, ok := kind.(WorldKind); ok {
			if err := wk.ValidateEvent(event); err != nil {
				return Event{}, err
			}
		}
	}
	return event, nil
}

/*
================
parseSpot

One spot= value: Name|Region|X|Z.
================
*/
func parseSpot(value string) (Spot, error) {
	parts := strings.Split(value, "|")
	if len(parts) != 4 || parts[0] == "" {
		return Spot{}, fmt.Errorf("spot %q is not Name|Region|X|Z", value)
	}
	region, err := strconv.Atoi(parts[1])
	if err != nil || region < 1 || region > 65535 {
		return Spot{}, fmt.Errorf("spot %q has no region 1..65535", value)
	}
	x, errX := strconv.ParseFloat(parts[2], 64)
	z, errZ := strconv.ParseFloat(parts[3], 64)
	if errX != nil || errZ != nil {
		return Spot{}, fmt.Errorf("spot %q has bad coordinates", value)
	}
	return Spot{Name: parts[0], RegionID: region, X: x, Z: z}, nil
}

/*
================
startOn

The occurrence's start on the given day (server-local).
================
*/
func (e Event) startOn(day time.Time) time.Time {
	clock, _ := time.Parse(scheduleClock, e.At)
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, day.Location())
}

/*
================
Occurrence

One event's live-or-next run: its start and end on the day it falls.
A run in progress says Running.
================
*/
type Occurrence struct {
	Event   Event
	Start   time.Time
	End     time.Time
	Running bool
}

/*
================
Occurrences

Each event's live-or-next occurrence at now: the run in progress, or
the next start (today's if it is still ahead, else tomorrow's). One
row per event, in file order - the schedule feed's rows.
================
*/
func (s Schedule) Occurrences(now time.Time) []Occurrence {
	var out []Occurrence
	for _, event := range s.Events {
		start := event.startOn(now)
		end := start.Add(event.Span())
		switch {
		case !now.Before(start) && now.Before(end):
			out = append(out, Occurrence{Event: event, Start: start, End: end, Running: true})
		case now.Before(start):
			out = append(out, Occurrence{Event: event, Start: start, End: end})
		default:
			start = start.AddDate(0, 0, 1)
			out = append(out, Occurrence{Event: event, Start: start, End: start.Add(event.Span())})
		}
	}
	return out
}
