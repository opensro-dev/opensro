/*
===========================================================================

muhan_chain.go - the Muhan wave director (port-only, #3273)

MuhanChain is the only owner of wave-run state. It exists only while
SRO_MUHAN is on (enterworld installs it); with the flag off the wave
file is never read and this file never runs.

The source authors no monster nests for these worlds: its wave table
(_RefMuhanMonsterWave, extracted in muhan-population.json) names, per
arena, twenty waves of (point, delay, count, level offset, radius,
monster type). Two semantics no binary this port may cite settles are
inference, shipped as the scope rule requires and recorded in
docs/features/MUHAN.md:

  - Kind selection: a wave spawns the kind at (world band + the
    wave's LevelOffset). The v1.696 kinds are level-stamped clones,
    exactly one combat kind per level, so the level names the kind.
  - Delay: a wave's DelayMs counts from the previous wave's
    clearance (the first wave's from the run's start). The rows'
    delays are not monotonic in sequence, so they cannot be absolute
    offsets from a start; clearance-relative is the reading that
    makes every row meaningful.

A run: twenty waves in sequence, each spawning Count instances of
its kind scattered inside the wave's radius at its point, the next
wave only once the whole wave is dead (the tomb chain's stage rule).
After the last wave falls the arena rests for a boss respawn window
(the tomb's 1800..3600 s inference) and the run re-arms, as the
permanently open tomb floors re-arm. The chain owns these lives: no
respawn bookkeeping, and a wave never refills.

The arenas are permanent worlds a division opens at boot, so a
population's run starts on its first tick and cycles whether or not
anyone stands inside - the world owns its timers, as the tomb chain
does. Who may enter an arena is the entry slice's question.

===========================================================================
*/
package simulation

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/muhan"
)

// The admitted kind families: the combat clones, never the transform
// forms (the import keeps those out of the shard; the director
// refuses them here too, so a hand-built shard cannot sneak one in).
const (
	muhanKindPrefix       = "MOB_MUHAN_"
	muhanChangePrefix     = "MOB_MUHAN_CHANGE_"
	muhanKindGrade        = 0
	muhanRearmMinSec      = 1800
	muhanRearmMaxSec      = 3600
	muhanKindFirstLevel   = 21
	muhanKindLastLevel    = 120
	muhanRadiusScatterMax = 1000
)

// A population's run phases.
const (
	muhanPhaseIdle = iota
	muhanPhaseWaiting
	muhanPhaseRunning
	muhanPhaseRearm
)

// muhanResolvedWave is a wave row bound to the kind it spawns.
type muhanResolvedWave struct {
	row monster.MuhanWave
	ref monster.MonsterRef
}

// muhanPopulation is one population's run progress.
type muhanPopulation struct {
	phase     int
	next      int
	live      []uint32
	dueMs     int64
	rearmAtMs int64
}

/*
================
MuhanChain

The wave director. Waves are resolved once against the immutable
template at install (kinds by level); per-population progress lives
in populations. All entry points run under MonsterState's mutex (the
population advance calls in).
================
*/
type MuhanChain struct {
	byWorld     map[instance.DefinitionID][]muhanResolvedWave
	populations map[*divisionMonsterState]*muhanPopulation
}

/*
================
MonsterState.InstallMuhanChain

Installs the wave director from the import's wave rows. Fails when a
wave names a world the muhan leaf does not carry, when a world's
sequences do not run contiguously from 1, or when some wave's level
(band + offset) does not resolve to exactly one admitted kind in the
template: wave data and the character shard ship together, so a
mismatch is a build error, not a runtime state.
================
*/
func (s *MonsterState) InstallMuhanChain(rows []monster.MuhanWave) error {
	chain := &MuhanChain{
		byWorld:     map[instance.DefinitionID][]muhanResolvedWave{},
		populations: map[*divisionMonsterState]*muhanPopulation{},
	}
	byLevel := map[int]monster.MonsterRef{}
	for _, ref := range s.template.Refs {
		if !muhanAdmittedKind(ref) {
			continue
		}
		if prior, dup := byLevel[int(ref.Level)]; dup {
			return fmt.Errorf("muhan chain: level %d resolves to two kinds, %s and %s; a wave names a kind by level alone",
				ref.Level, prior.Codename, ref.Codename)
		}
		byLevel[int(ref.Level)] = ref
	}
	grouped := map[int][]muhanResolvedWave{}
	for _, row := range rows {
		world, ok := muhan.Lookup(row.WorldID)
		if !ok {
			return fmt.Errorf("muhan chain: wave %d/%d names world %d, which is not a Muhan wave world", row.WorldID, row.Seq, row.WorldID)
		}
		if row.Count < 1 {
			return fmt.Errorf("muhan chain: wave %d/%d spawns nothing", row.WorldID, row.Seq)
		}
		level := world.Band + row.LevelOffset
		ref, ok := byLevel[level]
		if !ok {
			return fmt.Errorf("muhan chain: wave %d/%d names level %d (band %d %+d), which no admitted MOB_MUHAN_ kind carries",
				row.WorldID, row.Seq, level, world.Band, row.LevelOffset)
		}
		grouped[row.WorldID] = append(grouped[row.WorldID], muhanResolvedWave{row: row, ref: ref})
	}
	if len(grouped) == 0 {
		return fmt.Errorf("muhan chain: no waves")
	}
	for worldID, waves := range grouped {
		sort.Slice(waves, func(i, j int) bool { return waves[i].row.Seq < waves[j].row.Seq })
		for index, wave := range waves {
			if wave.row.Seq != index+1 {
				return fmt.Errorf("muhan chain: world %d sequences run to %d but wave %d sits at position %d; waves must run contiguously from 1",
					worldID, len(waves), wave.row.Seq, index+1)
			}
		}
		chain.byWorld[instance.DefinitionID(worldID)] = waves
	}
	s.muhanChain = chain
	return nil
}

/*
================
muhanAdmittedKind

One template ref against the family's admission, the server mirror
of the import's: a combat MOB_MUHAN_* kind (never a MOB_MUHAN_CHANGE_*
transform form) at grade 0 inside the authored clone window.
================
*/
func muhanAdmittedKind(ref monster.MonsterRef) bool {
	if !strings.HasPrefix(ref.Codename, muhanKindPrefix) || strings.HasPrefix(ref.Codename, muhanChangePrefix) {
		return false
	}
	if ref.MonsterType != muhanKindGrade {
		return false
	}
	return int(ref.Level) >= muhanKindFirstLevel && int(ref.Level) <= muhanKindLastLevel
}

/*
================
MuhanChain.tickLocked

Runs one population's wave clock. Populations whose lease is not a
Muhan arena never start. Called from the population advance with the
state lock held.
================
*/
func (c *MuhanChain) tickLocked(s *MonsterState, state *divisionMonsterState, nowMs int64) {
	waves, ok := c.byWorld[state.lease.ID.Definition()]
	if !ok {
		return
	}
	pop := c.populations[state]
	if pop == nil {
		pop = &muhanPopulation{}
		c.populations[state] = pop
	}
	switch pop.phase {
	case muhanPhaseIdle:
		pop.phase = muhanPhaseWaiting
		pop.dueMs = nowMs + waves[0].row.DelayMs
	case muhanPhaseWaiting:
		if nowMs < pop.dueMs {
			return
		}
		c.spawnWaveLocked(s, state, pop, waves[pop.next], nowMs)
		pop.phase = muhanPhaseRunning
	case muhanPhaseRunning:
		for _, gid := range pop.live {
			if state.instances.contains(gid) {
				return
			}
		}
		pop.live = nil
		pop.next++
		if pop.next >= len(waves) {
			pop.next = 0
			intervalMs, _ := monster.NestDelay(muhanRearmMinSec, muhanRearmMaxSec, 1, s.randomWord)
			pop.rearmAtMs = nowMs + int64(intervalMs)
			pop.phase = muhanPhaseRearm
			return
		}
		pop.dueMs = nowMs + waves[pop.next].row.DelayMs
		pop.phase = muhanPhaseWaiting
	case muhanPhaseRearm:
		if nowMs < pop.rearmAtMs {
			return
		}
		pop.rearmAtMs = 0
		pop.dueMs = nowMs + waves[0].row.DelayMs
		pop.phase = muhanPhaseWaiting
	}
}

/*
================
MuhanChain.spawnWaveLocked

Spawns the wave's Count instances of its kind, scattered inside the
wave's radius around its authored point, and records them as the
wave's live set. The spawn mirrors the tomb chain's: no respawn
bookkeeping (the director owns these lives), the point stands as
authored, and the instance is nest-detached so Defeat accepts it.
The wave's monster type rides as the spawn's rarity override.
================
*/
func (c *MuhanChain) spawnWaveLocked(s *MonsterState, state *divisionMonsterState, pop *muhanPopulation, wave muhanResolvedWave, nowMs int64) {
	for i := 0; i < wave.row.Count; i++ {
		x, z := wave.row.X, wave.row.Z
		if wave.row.Radius > 0 {
			angle := float64(float32(float64(float32(float64(s.randomWord())/nativeRandomMaximum)) * nativeCircleRadians))
			distance := float64(s.randomWord()%muhanRadiusScatterMax) / muhanRadiusScatterMax * float64(wave.row.Radius)
			x += math.Cos(angle) * distance
			z += math.Sin(angle) * distance
		}
		nest := monster.NestRow{
			SpawnPoint: monster.SpawnPoint{
				RefObjID: wave.ref.RefObjID, RegionID: uint16(wave.row.RegionID), X: x, Y: wave.row.Y, Z: z,
			},
			PolicyPinned: true,
		}
		if wave.row.MonsterType != 0 {
			nest.HasRarityOverride = true
			nest.RarityOverride = uint8(wave.row.MonsterType)
		}
		gid, ok := s.spawnChainInstanceLocked(state, nest, nowMs)
		if !ok {
			return
		}
		pop.live = append(pop.live, gid)
	}
}
