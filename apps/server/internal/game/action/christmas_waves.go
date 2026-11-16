/*
===========================================================================

christmas_waves.go - the Christmas event's unique waves

Port-only, not native (SRO_CHRISTMAS_EVENT, default off). At the
timetabled moments (progression/christmas_waves.go) the winter event's
uniques land in three fields, one per level band, after a notice:

  - every open hour, a White Knight of the field's level band;
  - twice a day, Princess Miyene, the event's boss, at the field's band.

The Snow Slaves are no wave: they roam every field all the time
(christmas_snow_slaves.go).

The kinds are the later clients' winter rows, imported into the variant
by scripts/winter_event_import.py; a build without them refuses each
spawn by codename, so a wave lands nothing and says so in the log, never
a panic. Waves go through the quest spawn path, as the Halloween waves
do: a GM spawn is conjured, not hunted. A wave monster has no nest, so
it never respawns, and leaves on its own timer, staggered by spawn order.

The tick step lives on the tick goroutine alone, so its one field needs
no lock. It holds no door: the notices queue through the announcer and
the spawns take only the monster owner's lock.

===========================================================================
*/
package action

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

// christmasWaveCheckEvery keeps the idle step to one clock compare a tick.
const christmasWaveCheckEvery = time.Second

// The minimap's coordinates: a sector is 192 game units, the local
// position is in tenths, and the origin is sector (135, 92).
const (
	gameUnitsPerSector = 192
	gameUnitsPerLocal  = 10
	gameOriginSectorX  = 135
	gameOriginSectorY  = 92
)

// The notices, each inside the notification's 100-unit budget
// (noticeMaxUnits): one line per field at the warning and at the
// landing, then the line that says what the wave is.
const (
	christmasKnightNoticeLead    = "Christmas: a White Knight comes to %s in 5 minutes"
	christmasKnightNoticeNow     = "Christmas: a White Knight has come to %s"
	christmasKnightNoticeHow     = "Snow Slaves come with the snow; the White Knights are rare guests."
	christmasPrincessNoticeLead  = "Christmas: Princess Miyene comes to %s in 10 minutes"
	christmasPrincessNoticeNow   = "Christmas: Princess Miyene has come to %s"
	christmasPrincessNoticeHow   = "Princess Miyene is the winter event's boss: bring a party."
	christmasEntryNoticeOn       = "The Christmas event runs until %s UTC: Snow Slaves come with the snow."
	christmasEntryNoticeSnowing  = "It is snowing until %s UTC: Snow Slaves roam every field."
	christmasEntryNoticeNextSnow = "Next snowfall %s UTC, for an hour."
	christmasEntryNoticeKnight   = "Next White Knights %s UTC: %s"
	christmasEntryNoticePrin     = "Next Princess Miyene %s UTC"
	christmasEntryTimeShape      = "Jan 2 15:04"
)

/*
================
Runtime.advanceChristmasWaves

Asks the timetable what fell since the last ask. The first ask only sets
the clock, so a restart never replays a wave. With the event off this is
one branch, and the shipped game is untouched.
================
*/
func (rt *Runtime) advanceChristmasWaves() {
	if !rt.Christmas.Enabled() || rt.Monsters == nil {
		return
	}
	now := rt.Now()
	if rt.christmasWavesAsked.IsZero() {
		rt.christmasWavesAsked = now
		return
	}
	if now.Sub(rt.christmasWavesAsked) < christmasWaveCheckEvery {
		return
	}
	moments := rt.Christmas.WaveMomentsBetween(rt.christmasWavesAsked, now)
	rt.christmasWavesAsked = now
	for _, moment := range moments {
		lead, landed, how := christmasWaveNotices(moment.Wave.Kind)
		for _, population := range rt.Monsters.OpenWorldPopulations() {
			if moment.Warning {
				rt.announceChristmasWave(population.Division, lead, how, moment.Wave, now.UnixMilli())
				continue
			}
			for _, field := range moment.Wave.Fields {
				rt.spawnChristmasField(population, moment.Wave, field.Codename, now.UnixMilli())
			}
			rt.announceChristmasWave(population.Division, landed, how, moment.Wave, now.UnixMilli())
		}
	}
}

/*
================
christmasWaveNotices

The warning, landing and explanation lines of a wave kind.
================
*/
func christmasWaveNotices(kind progression.ChristmasWaveKind) (lead, landed, how string) {
	if kind == progression.ChristmasPrincess {
		return christmasPrincessNoticeLead, christmasPrincessNoticeNow, christmasPrincessNoticeHow
	}
	return christmasKnightNoticeLead, christmasKnightNoticeNow, christmasKnightNoticeHow
}

/*
================
Runtime.announceChristmasWave

One line per field (area and level), then what the wave is: four
notices, inside the per-minute cap of six for this kind. A field whose
monster the data lacks is left out, as spawnChristmasField leaves it out
of the wave.
================
*/
func (rt *Runtime) announceChristmasWave(division, lead, how string, wave progression.ChristmasWave, nowMs int64) {
	for _, field := range wave.Fields {
		if place, ok := rt.christmasFieldPlace(field); ok {
			rt.announce(division, announceChristmasWave, fmt.Sprintf(lead, place), wire.NoticeLink{}, 0, nowMs)
		}
	}
	rt.announce(division, announceChristmasWave, how, wire.NoticeLink{}, 0, nowMs)
}

/*
================
Runtime.christmasFieldPlace

A field as a winter notice names it: the area, the field monster's level
and the minimap coordinates of the anchor the wave lands on. A wave lands
within a few steps of that point, so the area alone sent players looking
across the whole field.
================
*/
func (rt *Runtime) christmasFieldPlace(field progression.HalloweenField) (string, bool) {
	ref, found := rt.Monsters.ReferenceByCodename(field.Codename)
	if !found {
		return "", false
	}
	nest, found := rt.Monsters.FieldNest(field.Codename)
	if !found {
		return "", false
	}
	x, y := gameCoordinates(nest.RegionID, nest.X, nest.Z)
	return fmt.Sprintf("%s (Lv %d) X %d Y %d", field.Area, ref.Level, x, y), true
}

/*
================
gameCoordinates

A region-local position as the minimap prints it.
================
*/
func gameCoordinates(regionID uint16, x, z float64) (int, int) {
	gameX := (simulation.SectorX(regionID)-gameOriginSectorX)*gameUnitsPerSector + int(x/gameUnitsPerLocal)
	gameY := (simulation.SectorY(regionID)-gameOriginSectorY)*gameUnitsPerSector + int(z/gameUnitsPerLocal)
	return gameX, gameY
}

/*
================
Runtime.ChristmasEntryFrames

The notices a character entering the world gets while the event runs:
that it is on and until when, whether it snows now (until when) or when
the next snowfall comes, and when the next White Knights and the next
princess land. Sent once per login; the event off sends nothing.
================
*/
func (rt *Runtime) ChristmasEntryFrames(_ string, character *enterworld.Character) []enterworld.Packet {
	now := rt.Now()
	if character == nil || rt.Monsters == nil || !rt.Christmas.ActiveAt(now) {
		return nil
	}
	lines := []string{fmt.Sprintf(christmasEntryNoticeOn, rt.Christmas.EndsAt().UTC().Format(christmasEntryTimeShape))}
	if rt.Christmas.SnowingAt(now) {
		lines = append(lines, fmt.Sprintf(christmasEntryNoticeSnowing, rt.Christmas.SnowEndsAt(now).UTC().Format(christmasEntryTimeShape)))
	} else if at, ok := rt.Christmas.NextSnowfall(now); ok {
		lines = append(lines, fmt.Sprintf(christmasEntryNoticeNextSnow, at.UTC().Format(christmasEntryTimeShape)))
	}
	if wave, ok := rt.Christmas.NextWave(now, progression.ChristmasKnights); ok {
		lines = append(lines, fmt.Sprintf(christmasEntryNoticeKnight, wave.At.UTC().Format(christmasEntryTimeShape), christmasWaveAreas(wave)))
	}
	if wave, ok := rt.Christmas.NextWave(now, progression.ChristmasPrincess); ok {
		lines = append(lines, fmt.Sprintf(christmasEntryNoticePrin, wave.At.UTC().Format(christmasEntryTimeShape)))
	}
	packets := make([]enterworld.Packet, 0, len(lines))
	for _, line := range lines {
		frame := wire.DropPlusNotificationFrame(clampNotice(line), wire.NoticeLink{})
		packets = append(packets, enterworld.NewPacket(frame.Opcode, frame.Payload))
	}
	return packets
}

/*
================
christmasWaveAreas
================
*/
func christmasWaveAreas(wave progression.ChristmasWave) string {
	areas := ""
	for i, field := range wave.Fields {
		if i > 0 {
			areas += ", "
		}
		areas += field.Area
	}
	return areas
}

/*
================
Runtime.spawnChristmasField

One field's share of a wave: its White Knight, or its princess, near
the field's anchor nest. The winter rows pick their level band
from the field monster's level (progression.ChristmasWinterLevel). A
ground refusal drops one monster, never the wave; every landing is
logged with its counts, as the Halloween waves' are (#2950).
================
*/
func (rt *Runtime) spawnChristmasField(population simulation.WorldPopulation, wave progression.ChristmasWave, field string, nowMs int64) {
	nest, found := rt.Monsters.FieldNest(field)
	if !found {
		log.Warnf("christmas wave: field %s has no anchor nest; nothing landed", field)
		return
	}
	ref, found := rt.Monsters.ReferenceByCodename(field)
	if !found {
		log.Warnf("christmas wave: field %s has no monster reference; nothing landed", field)
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division: population.Division, Population: population.Lease,
		Position:      simulation.Spawn{RegionID: nest.RegionID, X: nest.X, Y: nest.Y, Z: nest.Z},
		FixedPosition: true, NowMs: nowMs, RadiusSpan: progression.ChristmasWaveRadius,
	}
	lifetime := wave.Lifetime()
	index, landed, refused := 0, 0, 0
	spawn := func(codename string, count int) {
		for range count {
			monster := request
			monster.Codename = codename
			monster.LifetimeMs = (lifetime + time.Duration(index)*progression.ChristmasDespawnStep).Milliseconds()
			if rt.Monsters.SpawnQuestMonster(monster) {
				landed++
			} else {
				refused++
			}
			index++
		}
	}
	level := int(ref.Level)
	switch wave.Kind {
	case progression.ChristmasPrincess:
		spawn(progression.ChristmasPrincessCodename(level), 1)
	default:
		spawn(progression.ChristmasKnightCodename(level), progression.ChristmasKnightsPerField)
	}
	log.Infof("christmas wave: field %s (Lv %d) landed at region %d (%.0f, %.0f): %d monsters (%d refused)",
		field, level, nest.RegionID, nest.X, nest.Z, landed, refused)
}
