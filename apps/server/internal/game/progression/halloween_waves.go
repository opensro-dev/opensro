/*
===========================================================================

halloween_waves.go - the Halloween Day event's Gourd wave timetable

Port-only, not native (#2124, the #2131 tally). A pure timetable: which
wave falls when, and in which pair of fields. One peak wave a day plus
one wave that rotates around the clock, each in two fields, one low and
one high, with the pair rotating. The action owner spawns what this names
(action/halloween_waves.go).

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
// wave and its 30-minute lifetime keep clear of 03:00-06:00 UTC.
var halloweenRotatingHours = [...]int{7, 11, 15, 19, 1}

// halloweenLowFields and halloweenHighFields are ordinary field monsters
// named by codename: their nests give each field its ground, its ring and
// its name. Index i of each list is one pair.
var (
	halloweenLowFields  = [...]string{"MOB_CH_WHITETIGER", "MOB_EU_KYKLOPES", "MOB_AM_CRAB"}
	halloweenHighFields = [...]string{"MOB_KK_YETI_CLON", "MOB_TK_DEATHKARA_CLON", "MOB_TK_DEVILEYE"}
)

/*
================
HalloweenWaveFields

Every field the timetable can name, so shipped data can prove each exists.
================
*/
func HalloweenWaveFields() []string {
	fields := append([]string{}, halloweenLowFields[:]...)
	return append(fields, halloweenHighFields[:]...)
}

/*
================
HalloweenWave

One wave: when its monsters land, and the two fields, named by the field
monster's codename.
================
*/
type HalloweenWave struct {
	At   time.Time
	Low  string
	High string
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
peak), by whole UTC days since the window's first day. The pair index
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
		pair := (dayIndex*halloweenWavesPerDay + i) % len(halloweenLowFields)
		waves[i] = HalloweenWave{
			At:  day.Add(time.Duration(hour) * time.Hour),
			Low: halloweenLowFields[pair], High: halloweenHighFields[pair],
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
