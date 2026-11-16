/*
===========================================================================

christmas_snow_slaves.go - the Snow Slaves that come with the snow

Port-only, not native (SRO_CHRISTMAS_EVENT, default off). While a
snowfall falls (progression/christmas_snow.go) the open world fills with
Snow Slaves, one for every progression.ChristmasSnowSlaveNestShare
ordinary nests, met in any field. Every ChristmasSnowSlaveTopUpEvery the
crowd is counted per division and what the hunters killed comes back
beside nests drawn at random, a batch at a time, so a snowfall's start
fills the world over a few counts. Each slave melts when its snowfall
stops, spread over ChristmasSnowMeltSpread so the leaving does not land
in one tick; between snowfalls no slave stands.

The slaves go through the quest spawn path, as the waves do, so they
have no nest and never respawn on their own: this step is their only
source. It lives on the tick goroutine alone, so its fields need no lock.

===========================================================================
*/
package action

import (
	"math/rand/v2"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
christmasSnowSlaves

The step's own state: the last count, the open world's ordinary nests
(the template never changes, so they are read once) and the draw.
================
*/
type christmasSnowSlaves struct {
	counted time.Time
	ground  []monster.NestRow
	draw    *rand.Rand
}

/*
================
Runtime.advanceChristmasSnowSlaves

With the event off, or between snowfalls, this is one branch.
================
*/
func (rt *Runtime) advanceChristmasSnowSlaves() {
	if rt.Monsters == nil || !rt.Christmas.Enabled() {
		return
	}
	now := rt.Now()
	if !rt.Christmas.SnowingAt(now) {
		return
	}
	slaves := &rt.christmasSlaves
	if !slaves.counted.IsZero() && now.Sub(slaves.counted) < progression.ChristmasSnowSlaveTopUpEvery {
		return
	}
	slaves.counted = now
	if slaves.ground == nil {
		slaves.ground = rt.Monsters.OpenWorldNests()
		slaves.draw = rand.New(rand.NewPCG(uint64(now.UnixNano()), 0)) //nolint:gosec // a spread over the map, not a secret
	}
	target := len(slaves.ground) / progression.ChristmasSnowSlaveNestShare
	if target == 0 {
		return
	}
	for _, population := range rt.Monsters.OpenWorldPopulations() {
		held := rt.Monsters.CountByCodename(population.Division, progression.ChristmasSnowSlaveCodename)
		missing := min(target-held, progression.ChristmasSnowSlaveTopUpBatch)
		if missing <= 0 {
			continue
		}
		landed := rt.landSnowSlaves(population, missing, now)
		log.Infof("christmas snow slaves: %s held %d of %d; %d landed", population.Division, held, target, landed)
	}
}

/*
================
Runtime.landSnowSlaves

count Snow Slaves beside ordinary nests drawn at random, each melting
when the snowfall stops, at a random instant of the melt spread. A
ground refusal drops one slave; the next count brings it back.
================
*/
func (rt *Runtime) landSnowSlaves(population simulation.WorldPopulation, count int, now time.Time) int {
	slaves := &rt.christmasSlaves
	melt := rt.Christmas.SnowEndsAt(now).Sub(now)
	spread := int(progression.ChristmasSnowMeltSpread.Milliseconds())
	landed := 0
	for range count {
		nest := slaves.ground[slaves.draw.IntN(len(slaves.ground))]
		request := simulation.QuestMonsterSpawn{
			Division: population.Division, Population: population.Lease, Codename: progression.ChristmasSnowSlaveCodename,
			Position:      simulation.Spawn{RegionID: nest.RegionID, X: nest.X, Y: nest.Y, Z: nest.Z},
			FixedPosition: true, NowMs: now.UnixMilli(), RadiusSpan: progression.ChristmasScatterRadius,
			LifetimeMs: melt.Milliseconds() + int64(slaves.draw.IntN(spread)),
		}
		if rt.Monsters.SpawnQuestMonster(request) {
			landed++
		}
	}
	return landed
}
