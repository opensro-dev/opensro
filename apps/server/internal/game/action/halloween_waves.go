/*
===========================================================================

halloween_waves.go - the Halloween Day event's Gourd waves

Port-only, not native (#2124; SRO_HALLOWEEN_EVENT, default off). At a
timetabled moment (progression/halloween_waves.go) many Gourds land in
three fields, one per level band, with a denser ring of the field's own
mobs, after notices ten minutes ahead. Every notice names the area and
the field's level and says what a Gourd is (#2545): the first live run
named only the field monster ("around the White Tiger and Yeti fields"),
on the channel the unique notices use, and players read it as a unique
spawning. A character entering the world while the event runs is told it
is on and where the next wave lands. Waves go through the quest spawn path, never the GM one: a
GM-spawned kill is conjured, not hunted, and would pay no box. Each wave
monster has no nest, so it never respawns, and leaves on its own timer.

The tick step lives on the tick goroutine alone, so its one field needs
no lock. It holds no door: the notices queue through the announcer and
the spawns take only the monster owner's lock.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

// halloweenWaveCheckEvery keeps the idle step to one clock compare a tick.
const halloweenWaveCheckEvery = time.Second

// The notices, each inside the notification's 100-unit budget
// (noticeMaxUnits) with the longest field area: one line per field at the
// warning and at the landing, then the line that says what a Gourd is. The
// entry notices name the window's end and the next wave.
const (
	halloweenWaveNoticeLead = "Halloween: Gourds land near %s in 10 minutes"
	halloweenWaveNoticeNow  = "Halloween: Gourds have landed near %s"
	halloweenWaveNoticeHow  = "Gourds are event monsters: each drops a Jewel Box. So-ok in town trades boxes for gifts."
	halloweenEntryNoticeOn  = "The Halloween event runs until %s UTC."
	halloweenEntryNoticeAt  = "Next Gourd wave %s UTC: %s"
	halloweenEntryTimeShape = "Jan 2 15:04"
)

/*
================
Runtime.advanceHalloweenWaves

Asks the timetable what fell since the last ask. The first ask only sets
the clock, so a restart never replays a wave. With the event off this is
one branch, and the shipped game is untouched.
================
*/
func (rt *Runtime) advanceHalloweenWaves() {
	if !rt.Halloween.Enabled() || rt.Monsters == nil {
		return
	}
	now := rt.Now()
	if rt.halloweenWavesAsked.IsZero() {
		rt.halloweenWavesAsked = now
		return
	}
	if now.Sub(rt.halloweenWavesAsked) < halloweenWaveCheckEvery {
		return
	}
	moments := rt.Halloween.WaveMomentsBetween(rt.halloweenWavesAsked, now)
	rt.halloweenWavesAsked = now
	for _, moment := range moments {
		for _, population := range rt.Monsters.OpenWorldPopulations() {
			if moment.Warning {
				rt.announceHalloweenWave(population.Division, halloweenWaveNoticeLead, moment.Wave, now.UnixMilli())
				continue
			}
			for _, field := range moment.Wave.Fields {
				rt.spawnHalloweenField(population, field.Codename, now.UnixMilli())
			}
			rt.announceHalloweenWave(population.Division, halloweenWaveNoticeNow, moment.Wave, now.UnixMilli())
		}
	}
}

/*
================
Runtime.announceHalloweenWave

One line per field (area and level), then what a Gourd is: four notices,
inside the per-minute cap of six for this kind. A field whose monster the
data lacks is left out of the notice, as spawnHalloweenField leaves it out
of the wave.
================
*/
func (rt *Runtime) announceHalloweenWave(division, lead string, wave progression.HalloweenWave, nowMs int64) {
	for _, field := range wave.Fields {
		if place, ok := rt.halloweenFieldPlace(field); ok {
			rt.announce(division, announceHalloweenWave, fmt.Sprintf(lead, place), wire.NoticeLink{}, 0, nowMs)
		}
	}
	rt.announce(division, announceHalloweenWave, halloweenWaveNoticeHow, wire.NoticeLink{}, 0, nowMs)
}

/*
================
Runtime.halloweenFieldPlace

A field as a notice names it: the area and the field monster's level, so
a player knows where to ride and whether the field suits them.
================
*/
func (rt *Runtime) halloweenFieldPlace(field progression.HalloweenField) (string, bool) {
	ref, found := rt.Monsters.ReferenceByCodename(field.Codename)
	if !found {
		return "", false
	}
	return fmt.Sprintf("%s (Lv %d field)", field.Area, ref.Level), true
}

/*
================
Runtime.HalloweenEntryFrames

The notices a character entering the world gets while the event runs:
that it is on and until when, where and when the next wave lands, and
what a Gourd is. Sent once per login (enterworld's login outcome, not a
teleport's re-entry); the event off sends nothing.
================
*/
func (rt *Runtime) HalloweenEntryFrames(_ string, character *enterworld.Character) []enterworld.Packet {
	now := rt.Now()
	if character == nil || rt.Monsters == nil || !rt.Halloween.ActiveAt(now) {
		return nil
	}
	lines := []string{fmt.Sprintf(halloweenEntryNoticeOn, rt.Halloween.EndsAt().UTC().Format(halloweenEntryTimeShape))}
	if wave, ok := rt.Halloween.NextWave(now); ok {
		areas := make([]string, 0, len(wave.Fields))
		for _, field := range wave.Fields {
			areas = append(areas, field.Area)
		}
		lines = append(lines, fmt.Sprintf(halloweenEntryNoticeAt, wave.At.UTC().Format(halloweenEntryTimeShape), strings.Join(areas, ", ")))
	}
	lines = append(lines, halloweenWaveNoticeHow)
	packets := make([]enterworld.Packet, 0, len(lines))
	for _, line := range lines {
		frame := wire.DropPlusNotificationFrame(clampNotice(line), wire.NoticeLink{})
		packets = append(packets, enterworld.NewPacket(frame.Opcode, frame.Payload))
	}
	return packets
}

/*
================
Runtime.spawnHalloweenField

One field's share of a wave: the Gourds close to the field's anchor nest,
then the ring of its own mobs just outside them, as hostile as that nest.
A ground refusal drops one monster, never the wave. The leaving is
staggered by spawn order (halloweenWaveLifetimeMs), so the field drains
over about a minute instead of expiring in one tick.
================
*/
func (rt *Runtime) spawnHalloweenField(population simulation.WorldPopulation, field string, nowMs int64) {
	nest, found := rt.Monsters.FieldNest(field)
	if !found {
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division: population.Division, Population: population.Lease,
		Position:      simulation.Spawn{RegionID: nest.RegionID, X: nest.X, Y: nest.Y, Z: nest.Z},
		FixedPosition: true, NowMs: nowMs,
	}
	index := 0
	gourd := request
	gourd.Codename, gourd.RadiusSpan = halloweenGourdCodename, progression.HalloweenWaveGourdRadius
	for range progression.HalloweenWaveGourdsPerField {
		gourd.LifetimeMs = halloweenWaveLifetimeMs(index)
		rt.Monsters.SpawnQuestMonster(gourd)
		index++
	}
	ring := request
	ring.Codename, ring.TacticsFrom = field, &nest
	ring.RadiusMin, ring.RadiusSpan = progression.HalloweenWaveRingRadius, progression.HalloweenWaveRingDepth
	for range progression.HalloweenWaveRingPerField {
		ring.LifetimeMs = halloweenWaveLifetimeMs(index)
		rt.Monsters.SpawnQuestMonster(ring)
		index++
	}
}

/*
================
halloweenWaveLifetimeMs

When a field's index-th wave monster leaves: the wave lifetime plus one
despawn step per place in the spawn order. The #2124 crowded-field run
showed one synchronized 140-monster expiry costing an 11-tick burst
(worst tick 6.2 s); the ladder drains a field over about a minute.
================
*/
func halloweenWaveLifetimeMs(index int) int64 {
	lifetime := progression.HalloweenWaveLifetime + time.Duration(index)*progression.HalloweenWaveDespawnStep
	return lifetime.Milliseconds()
}
