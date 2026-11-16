/*
===========================================================================

christmas_waves.go - the Christmas event's Snow Slaves and unique timetable

Port-only, not native (SRO_CHRISTMAS_EVENT, default off). The later
clients' winter event names its parts ("Snow Slave Hunting Event",
"White Knight summon Event", "Princess summon Event") and ships its
monsters, but no binary this port may cite schedules them; this
is the inference, as the event is remembered:

  - Snow Slaves (level 1, five hit points: the hunted crowd) fill every
    field while a snowfall falls (christmas_snow.go), a population kept
    full beside the open world's ordinary nests (one slave for every
    ChristmasSnowSlaveNestShare nests), and melt when it stops;
  - the uniques come now and then, on the Halloween waves' pattern: a
    White Knight every hour at half past, one per band field, and
    Princess Miyene twice a day, one per band field, with a longer
    warning: the event's boss.

The fields are the Halloween waves' three bands (halloweenFieldBands),
already proved against the shipped data, with the trio rotating. Every
hour here is UTC and keeps clear of 03:00-06:00 (the 04:00 daily reset
and the 05:00 backup), as the Halloween waves do. The action owner
spawns what this names (action/christmas_waves.go) and keeps the slaves
(action/christmas_snow_slaves.go).

===========================================================================
*/
package progression

import (
	"fmt"
	"sort"
	"time"
)

const (
	// ChristmasSnowSlaveCodename is the hunted crowd ("Snow Slave").
	ChristmasSnowSlaveCodename = "MOB_EV_WINTER_SNOWMAN"
	// christmasKnightPrefix and christmasPrincessPrefix name the level
	// band rows, MOB_EV_WINTER_WHITEKNIGHT_<level> and so on.
	christmasKnightPrefix   = "MOB_EV_WINTER_WHITEKNIGHT_"
	christmasPrincessPrefix = "MOB_EV_WINTER_PRINCESS_"

	// ChristmasKnightsPerField come to each field every knight hour.
	ChristmasKnightsPerField = 1
	// ChristmasWaveRadius bounds a unique's landing around the field's
	// anchor.
	ChristmasWaveRadius = 300

	// ChristmasSnowSlaveNestShare sets the standing Snow Slave population:
	// one slave for every this many ordinary open-world nests, so a field
	// with twenty nests holds a handful wherever a hunter stands (owner,
	// 2026-11-15: 4 was a little thin).
	ChristmasSnowSlaveNestShare = 3
	// ChristmasSnowSlaveTopUpEvery is how often the population is counted
	// and the killed slaves replaced: their respawn time.
	ChristmasSnowSlaveTopUpEvery = 30 * time.Second
	// ChristmasSnowSlaveTopUpBatch bounds one top-up, so the window's
	// opening fills the world over a few counts, not in one tick.
	ChristmasSnowSlaveTopUpBatch = 500
	// ChristmasSnowMeltSpread spreads a snowfall's melting over this
	// long, so thousands of slaves do not leave in one tick.
	ChristmasSnowMeltSpread = 30 * time.Second
	// ChristmasScatterRadius keeps a Snow Slave a few steps from its nest,
	// on the nest's own ground.
	ChristmasScatterRadius = 60

	// ChristmasKnightWarning and ChristmasPrincessWarning are how long
	// before a wave its notice goes out.
	ChristmasKnightWarning   = 5 * time.Minute
	ChristmasPrincessWarning = 10 * time.Minute
	// ChristmasKnightLifetime and ChristmasPrincessLifetime remove what a
	// wave left: wave monsters have no nest, so nothing else ever would.
	ChristmasKnightLifetime   = 20 * time.Minute
	ChristmasPrincessLifetime = 40 * time.Minute
	// ChristmasDespawnStep staggers the leaving, as the Halloween waves do.
	ChristmasDespawnStep = time.Second

	// christmasKnightMinute is the White Knights' minute in every open hour.
	christmasKnightMinute = 30
)

// christmasQuietHours are the UTC hours no wave lands in.
var christmasQuietHours = map[int]bool{3: true, 4: true, 5: true}

// christmasPrincessHours are the princess's UTC hours, one per
// half-day, the second inside the 21:00-23:00 peak.
var christmasPrincessHours = [...]int{13, 21}

// christmasWinterLevels are the levels the winter rows exist at, up to
// the server's cap (scripts/winter_event_import.py admits the same).
var christmasWinterLevels = [...]int{20, 24, 30, 40, 60, 80, 90, 100, 110}

/*
================
ChristmasWaveKind
================
*/
type ChristmasWaveKind int

const (
	ChristmasKnights ChristmasWaveKind = iota
	ChristmasPrincess
)

/*
================
ChristmasWave

One wave: what lands, when, and in which fields, lowest band first.
================
*/
type ChristmasWave struct {
	At     time.Time
	Kind   ChristmasWaveKind
	Fields [halloweenBands]HalloweenField
}

/*
================
ChristmasWaveMoment

One thing due on the timetable: a wave's warning, or the wave itself.
================
*/
type ChristmasWaveMoment struct {
	Wave    ChristmasWave
	Warning bool
}

/*
================
ChristmasWave.Warning

How long before the wave its notice goes out.
================
*/
func (w ChristmasWave) Warning() time.Duration {
	if w.Kind == ChristmasPrincess {
		return ChristmasPrincessWarning
	}
	return ChristmasKnightWarning
}

/*
================
ChristmasWave.Lifetime
================
*/
func (w ChristmasWave) Lifetime() time.Duration {
	if w.Kind == ChristmasPrincess {
		return ChristmasPrincessLifetime
	}
	return ChristmasKnightLifetime
}

/*
================
christmasDayWaves

A day's waves in time order, by whole UTC days since the window's first
day. The trio moves on every wave, so the crowd does not meet in one
field twice running.
================
*/
func christmasDayWaves(day time.Time, dayIndex int) []ChristmasWave {
	var waves []ChristmasWave
	add := func(at time.Time, kind ChristmasWaveKind) {
		trio := (dayIndex*24 + len(waves)) % halloweenTrios
		wave := ChristmasWave{At: at, Kind: kind}
		for band := range halloweenFieldBands {
			wave.Fields[band] = halloweenFieldBands[band][trio]
		}
		waves = append(waves, wave)
	}
	princess := 0
	for hour := range 24 {
		if christmasQuietHours[hour] {
			continue
		}
		if princess < len(christmasPrincessHours) && christmasPrincessHours[princess] == hour {
			add(day.Add(time.Duration(hour)*time.Hour), ChristmasPrincess)
			princess++
		}
		add(day.Add(time.Duration(hour)*time.Hour+christmasKnightMinute*time.Minute), ChristmasKnights)
	}
	return waves
}

/*
================
ChristmasEvent.WaveMomentsBetween

What falls in (after, until], oldest first. A wave outside the window, or
a warning before the window opens, does not exist. The action owner
keeps the last instant it asked about, so nothing fires twice and a
restart never replays a wave that already landed.
================
*/
func (e ChristmasEvent) WaveMomentsBetween(after, until time.Time) []ChristmasWaveMoment {
	if !e.enabled || !e.WindowValid() || !until.After(after) {
		return nil
	}
	first := e.startsAt.Truncate(hoursPerDay)
	var moments []ChristmasWaveMoment
	due := func(moment time.Time) bool { return moment.After(after) && !moment.After(until) && e.ActiveAt(moment) }
	// The day before after's still holds warnings that fall after it.
	for day := after.UTC().Truncate(hoursPerDay).Add(-hoursPerDay); !day.After(until); day = day.Add(hoursPerDay) {
		dayIndex := int(day.Sub(first) / hoursPerDay)
		if dayIndex < 0 {
			continue
		}
		for _, wave := range christmasDayWaves(day, dayIndex) {
			if due(wave.At.Add(-wave.Warning())) && e.ActiveAt(wave.At) {
				moments = append(moments, ChristmasWaveMoment{Wave: wave, Warning: true})
			}
			if due(wave.At) {
				moments = append(moments, ChristmasWaveMoment{Wave: wave})
			}
		}
	}
	sort.SliceStable(moments, func(i, j int) bool { return moments[i].instant().Before(moments[j].instant()) })
	return moments
}

/*
================
ChristmasEvent.NextWave

The first wave of the kind landing after now, for the entry notice.
A day covers the longest gap between waves of either kind.
================
*/
func (e ChristmasEvent) NextWave(now time.Time, kind ChristmasWaveKind) (ChristmasWave, bool) {
	for _, moment := range e.WaveMomentsBetween(now, now.Add(hoursPerDay)) {
		if !moment.Warning && moment.Wave.Kind == kind {
			return moment.Wave, true
		}
	}
	return ChristmasWave{}, false
}

/*
================
ChristmasWaveMoment.instant
================
*/
func (m ChristmasWaveMoment) instant() time.Time {
	if m.Warning {
		return m.Wave.At.Add(-m.Wave.Warning())
	}
	return m.Wave.At
}

/*
================
ChristmasWinterLevel

The winter row level a field of fieldLevel gets: the highest row at or
below it, or the lowest row for a field below every row (a level 15
field still meets the level 20 knight, the gentlest there is).
================
*/
func ChristmasWinterLevel(fieldLevel int) int {
	level := christmasWinterLevels[0]
	for _, candidate := range christmasWinterLevels {
		if candidate <= fieldLevel {
			level = candidate
		}
	}
	return level
}

/*
================
ChristmasKnightCodename
================
*/
func ChristmasKnightCodename(fieldLevel int) string {
	return fmt.Sprintf("%s%d", christmasKnightPrefix, ChristmasWinterLevel(fieldLevel))
}

/*
================
ChristmasPrincessCodename
================
*/
func ChristmasPrincessCodename(fieldLevel int) string {
	return fmt.Sprintf("%s%d", christmasPrincessPrefix, ChristmasWinterLevel(fieldLevel))
}

/*
================
ChristmasWinterCodenames

Every winter row the timetable can name, so shipped data can prove each
exists.
================
*/
func ChristmasWinterCodenames() []string {
	codes := []string{ChristmasSnowSlaveCodename}
	for _, level := range christmasWinterLevels {
		codes = append(codes, fmt.Sprintf("%s%d", christmasKnightPrefix, level), fmt.Sprintf("%s%d", christmasPrincessPrefix, level))
	}
	return codes
}
