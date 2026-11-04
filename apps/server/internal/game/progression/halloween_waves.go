/*
===========================================================================

halloween_waves.go - the Halloween Day event's Gourd wave timetable

Port-only, not native (#2124, the #2131 tally). A pure timetable: which
wave falls when, and in which fields. One peak wave a day plus one wave
that rotates around the clock, each in three fields, one per level band
(low, high and end game), with the trio rotating. The end-game band
(#2546) exists because the shard's cap is 90: without it no wave landed
where a capped character hunts, and the level gap left such a character
nothing from the lower fields but the boxes. The action owner spawns what
this names (action/halloween_waves.go).

Every hour here is in UTC and stays clear of the 04:00 daily box reset by
an hour, and of the 05:00 backup.

===========================================================================
*/
package progression

import (
	"sort"
	"time"
)

const (
	// HalloweenWaveWarning is how long before a wave its notice goes out.
	HalloweenWaveWarning = 10 * time.Minute
	// HalloweenWaveLifetime removes what a wave left: a wave's monsters
	// have no nest, so nothing else ever would.
	HalloweenWaveLifetime = 30 * time.Minute
	// HalloweenWaveDespawnStep staggers that leaving: a field's monster
	// i, in spawn order, leaves i steps after the lifetime ends, so a
	// field drains over about a minute instead of expiring in one tick.
	// A synchronized 140-monster expiry cost an 11-tick burst (worst
	// tick 6.2 s) in the #2124 crowded-field run.
	HalloweenWaveDespawnStep = time.Second
	// HalloweenWaveGourdsPerField must stay high: the Gourd has 11 HP, so a
	// thin wave pays only the best area damage in the field. The live
	// crowded-field run (#2144) proves or moves this before launch.
	HalloweenWaveGourdsPerField = 40
	// HalloweenWaveRingPerField is the denser ring of the field's own
	// mobs. The Gourd is the marker; the ring is the fight.
	HalloweenWaveRingPerField = 30
	// HalloweenWaveGourdRadius bounds the Gourds around the field's anchor.
	HalloweenWaveGourdRadius = 300
	// HalloweenWaveRingRadius and HalloweenWaveRingDepth place the ring
	// just outside the Gourds.
	HalloweenWaveRingRadius = 300
	HalloweenWaveRingDepth  = 150

	// halloweenPeakHour sits inside the 21:00-23:00 UTC peak.
	halloweenPeakHour = 22
	// halloweenWavesPerDay is the peak wave and the rotating wave.
	halloweenWavesPerDay = 2
	hoursPerDay          = 24 * time.Hour
)

// halloweenRotatingHours is the rotating wave's hour, one entry a day. A
// wave, its 30-minute lifetime and its minute of staggered drain keep
// clear of 03:00-06:00 UTC.
var halloweenRotatingHours = [...]int{7, 11, 15, 19, 1}

/*
================
HalloweenField

One wave field: an ordinary field monster named by codename (its nests
give the field its ground, its ring and its monster name) and the area a
player finds it in. Area is the minimap's name (textzonename) of the
region holding the nest the wave lands on (simulation FieldNest), read
once from the shipped tables when the field was chosen (#2545): the
server loads no region names, and a notice that names only a monster
read as a unique spawning.
================
*/
type HalloweenField struct {
	Codename string
	Area     string
}

// halloweenFieldBands are the three level bands, low (Lv 15-23), high
// (Lv 60-70) and end game (Lv 80-88). Index i of each band is one trio.
var halloweenFieldBands = [halloweenBands][halloweenTrios]HalloweenField{
	{{"MOB_CH_WHITETIGER", "South-Tiger Mt."}, {"MOB_EU_KYKLOPES", "Desperado Hill"}, {"MOB_AM_CRAB", "Center-Asia Minor"}},
	{{"MOB_KK_YETI_CLON", "Karakoram Oasis"}, {"MOB_TK_DEATHKARA_CLON", "Anahita River"}, {"MOB_TK_DEVILEYE", "South-Taklamakan"}},
	{{"MOB_RM_WINGTRIBE", "Mt. Roke field"}, {"MOB_RM_ANTELOPE", "Mt. Roke field"}, {"MOB_RM_ROCKY", "Wing peak"}},
}

// halloweenBands is how many fields one wave lands in; halloweenTrios how
// many trios the timetable rotates through.
const (
	halloweenBands = 3
	halloweenTrios = 3
)

/*
================
HalloweenWaveSpan

How long a field's wave can still have monsters in it: the lifetime
plus the staggered drain of its last monster. The reset and backup
clearance is judged against this, not the bare lifetime.
================
*/
func HalloweenWaveSpan() time.Duration {
	perField := HalloweenWaveGourdsPerField + HalloweenWaveRingPerField
	return HalloweenWaveLifetime + time.Duration(perField-1)*HalloweenWaveDespawnStep
}

/*
================
HalloweenWaveFields

Every field the timetable can name, so shipped data can prove each exists.
================
*/
func HalloweenWaveFields() []HalloweenField {
	var fields []HalloweenField
	for _, band := range halloweenFieldBands {
		fields = append(fields, band[:]...)
	}
	return fields
}

/*
================
HalloweenWave

One wave: when its monsters land, and its fields, lowest band first.
================
*/
type HalloweenWave struct {
	At     time.Time
	Fields [halloweenBands]HalloweenField
}

/*
================
HalloweenWaveMoment

One thing due on the timetable: a wave's warning, or the wave itself.
================
*/
type HalloweenWaveMoment struct {
	Wave    HalloweenWave
	Warning bool
}

/*
================
halloweenDayWaves

A day's two waves in time order (every rotating hour is before the
peak), by whole UTC days since the window's first day. The trio index
moves on every wave, so the crowd does not meet in one field twice running.
================
*/
func halloweenDayWaves(day time.Time, dayIndex int) [halloweenWavesPerDay]HalloweenWave {
	hours := [halloweenWavesPerDay]int{
		halloweenRotatingHours[dayIndex%len(halloweenRotatingHours)],
		halloweenPeakHour,
	}
	var waves [halloweenWavesPerDay]HalloweenWave
	for i, hour := range hours {
		trio := (dayIndex*halloweenWavesPerDay + i) % halloweenTrios
		waves[i].At = day.Add(time.Duration(hour) * time.Hour)
		for band := range halloweenFieldBands {
			waves[i].Fields[band] = halloweenFieldBands[band][trio]
		}
	}
	return waves
}

/*
================
HalloweenEvent.WaveMomentsBetween

What falls in (after, until], oldest first. A wave outside the window, or
a warning before the window opens, does not exist. The action owner keeps
the last instant it asked about, so nothing fires twice and a restart
never replays a wave that already landed.
================
*/
func (e HalloweenEvent) WaveMomentsBetween(after, until time.Time) []HalloweenWaveMoment {
	if !e.enabled || !e.WindowValid() || !until.After(after) {
		return nil
	}
	first := e.startsAt.Truncate(hoursPerDay)
	var moments []HalloweenWaveMoment
	due := func(moment time.Time) bool { return moment.After(after) && !moment.After(until) && e.ActiveAt(moment) }
	for day := after.UTC().Truncate(hoursPerDay); !day.After(until); day = day.Add(hoursPerDay) {
		dayIndex := int(day.Sub(first) / hoursPerDay)
		if dayIndex < 0 {
			continue
		}
		for _, wave := range halloweenDayWaves(day, dayIndex) {
			if due(wave.At.Add(-HalloweenWaveWarning)) && e.ActiveAt(wave.At) {
				moments = append(moments, HalloweenWaveMoment{Wave: wave, Warning: true})
			}
			if due(wave.At) {
				moments = append(moments, HalloweenWaveMoment{Wave: wave})
			}
		}
	}
	sort.SliceStable(moments, func(i, j int) bool { return moments[i].instant().Before(moments[j].instant()) })
	return moments
}

/*
================
HalloweenEvent.NextWave

The first wave landing after now, for the entry notice (#2545). Two days
covers the longest gap between waves (a peak at 22:00 to the next day's
rotating hour); a wave past the window's end does not exist.
================
*/
func (e HalloweenEvent) NextWave(now time.Time) (HalloweenWave, bool) {
	for _, moment := range e.WaveMomentsBetween(now, now.Add(2*hoursPerDay)) {
		if !moment.Warning {
			return moment.Wave, true
		}
	}
	return HalloweenWave{}, false
}

/*
================
HalloweenWaveMoment.instant

When this moment falls: a warning leads its wave.
================
*/
func (m HalloweenWaveMoment) instant() time.Time {
	if m.Warning {
		return m.Wave.At.Add(-HalloweenWaveWarning)
	}
	return m.Wave.At
}
