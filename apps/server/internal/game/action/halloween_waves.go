/*
===========================================================================

halloween_waves.go - the Halloween Day event's Gourd waves

Port-only, not native (#2124; SRO_HALLOWEEN_EVENT, default off). At a
timetabled moment (progression/halloween_waves.go) many Gourds land in two
fields with a denser ring of the field's own mobs, after a notice ten
minutes ahead. Waves go through the quest spawn path, never the GM one: a
GM-spawned kill is conjured, not hunted, and would pay no box. Each wave
monster has no nest, so it never respawns, and leaves on its own timer.

The tick step lives on the tick goroutine alone, so its one field needs
no lock. It holds no door: the notices queue through the announcer and
the spawns take only the monster owner's lock.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

// halloweenWaveCheckEvery keeps the idle step to one clock compare a tick.
const halloweenWaveCheckEvery = time.Second

// halloweenWaveNoticeLead and halloweenWaveNoticeNow are the two notices.
const (
	halloweenWaveNoticeLead = "Halloween: Gourds land in 10 minutes around the "
	halloweenWaveNoticeNow  = "Halloween: the Gourds have landed around the "
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
			rt.spawnHalloweenField(population, moment.Wave.Low, now.UnixMilli())
			rt.spawnHalloweenField(population, moment.Wave.High, now.UnixMilli())
			rt.announceHalloweenWave(population.Division, halloweenWaveNoticeNow, moment.Wave, now.UnixMilli())
		}
	}
}

/*
================
Runtime.announceHalloweenWave

Names both fields by their own monsters' names, as a player finds them.
================
*/
func (rt *Runtime) announceHalloweenWave(division, lead string, wave progression.HalloweenWave, nowMs int64) {
	low, lowFound := rt.Monsters.ReferenceByCodename(wave.Low)
	high, highFound := rt.Monsters.ReferenceByCodename(wave.High)
	if !lowFound || !highFound {
		return
	}
	text := lead + low.Name + " and " + high.Name + " fields"
	rt.announce(division, announceHalloweenWave, text, wire.NoticeLink{}, 0, nowMs)
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
