/*
===========================================================================

tomb_chain.go - the Qin-Shi tomb trigger chain (port-only, #2362)

TombChain is the only owner of trigger-chain state. It exists only while
SRO_QINSHI_TOMB is on (enterworld installs it); with the flag off the
tomb template is never built and this file never runs.

Native shape (the reference server build; evidence in
post-cut/2362-tomb-asm.md): the Floor 5 trigger setup builds one wave
spawner per stage as string parameters. A stage spawns Count instances
of its codename at its nest point when it is activated; the death
handler erases each dead instance from the stage's live set, so a
stage fires only when its whole wave is dead. A fired stage counts
toward the needed-trigger set of the stage that names it; when every
needed stage has fired, that stage spawns.

Floor 5 (slice A, #2372): the four guardians are initialized at arm
time; the Flame Master needs all four; its death opens the Floor 6
entry gate (the native RuntimePortal Chins_Floor5_Portal).

Two native points the ASM pass could not isolate are inference, shipped
as the scope rule requires:
  - Fired triggers latch: a dead guardian stays counted. (The only
    reading under which the Flame Master gate can ever open.)
  - The chain re-arms BossRespawnWindowSec after the boss dies. Natively
    the chain resets by the tomb instance closing and reopening on a
    schedule; the port keeps the tomb permanently open (#1013), so the
    floor's unique respawn window (1800..3600 s, the Floor 1-3 unique
    rows) stands in for the reset.

===========================================================================
*/
package simulation

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/monster"
)

// TombGateFloor6Entry is the Floor 6 entry gate: the walk-in zones on
// Floor 5 teleport only while this gate is open.
const TombGateFloor6Entry = "Chins_Floor5_Portal"

// Boss window after the floor boss dies before the chain re-arms
// (inference above; the Floor 1-3 unique respawn window).
const (
	tombRearmMinSec = 1800
	tombRearmMaxSec = 3600
)

/*
================
TombSpawnerDef

One native WaveSpawner definition: Count instances of Codename at the
codename's tomb nest point. Initialize spawners activate when the chain
arms; the others activate when every spawner in Needed has fired.
Trigger names what a completed stage fires: another spawner (through
its Needed set) or a gate (a portal), for spawners that end a floor.
================
*/
type TombSpawnerDef struct {
	Name       string
	Codename   string
	Count      int
	Initialize bool
	Trigger    string
	Needed     []string
}

/*
================
TombFloorDef

One floor's spawners. The spawner whose Trigger is a gate (not a
spawner name) is the floor's boss: its death opens the gate and starts
the rearm window.
================
*/
type TombFloorDef struct {
	Spawners []TombSpawnerDef
}

/*
================
TombChainDef
================
*/
type TombChainDef struct {
	Floors []TombFloorDef
}

/*
================
TombChainDefaultDef

The chain as extracted from the two native InitTriggers bodies. Slice A
(#2372) carries Floor 5; Floor 6 joins in slice B (#2373). Positions
come from the codename's tomb nest row at install time, not from here:
the native positions are in the later server's global dungeon frame,
the nest rows carry the port's region-local frame.
================
*/
func TombChainDefaultDef() TombChainDef {
	flameMaster := "Chins_Floor5_FlameMaster"
	return TombChainDef{Floors: []TombFloorDef{{
		Spawners: []TombSpawnerDef{
			{Name: "Chins_Floor5_East", Codename: "MOB_TQ_EASTGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
			{Name: "Chins_Floor5_South", Codename: "MOB_TQ_SOUTHGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
			{Name: "Chins_Floor5_West", Codename: "MOB_TQ_WESTGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
			{Name: "Chins_Floor5_North", Codename: "MOB_TQ_NORTHGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
			{Name: flameMaster, Codename: "MOB_TQ_FLAMEMASTER", Count: 1, Trigger: TombGateFloor6Entry,
				Needed: []string{"Chins_Floor5_East", "Chins_Floor5_South", "Chins_Floor5_West", "Chins_Floor5_North"}},
		},
	}}}
}

// tombResolvedSpawner is a definition bound to its template nest.
type tombResolvedSpawner struct {
	def   TombSpawnerDef
	floor int
	nest  monster.NestRow
}

// tombStage is one spawner's live state in one population.
type tombStage struct {
	live    int
	fired   bool
	spawned bool
}

// tombFloorState is one floor's progress in one population.
type tombFloorState struct {
	stages    map[string]*tombStage
	rearmAtMs int64
}

// tombPopulation is the chain's state for one division population.
type tombPopulation struct {
	armed    bool
	floors   []tombFloorState
	gates    map[string]bool
	liveGids map[uint32]string
}

/*
================
TombChain

The chain owner. Resolved once against the immutable template at
install; per-population progress lives in populations. All entry points
run under MonsterState's mutex (the tick and Defeat call in, the gate
query locks).
================
*/
type TombChain struct {
	spawners    []tombResolvedSpawner
	byName      map[string]int
	dependents  map[string][]int
	gateFloor   map[string]int
	populations map[*divisionMonsterState]*tombPopulation
}

/*
================
MonsterState.InstallTombChain

Installs the default chain (Floor 5). Fails when the template lacks a
chain codename's nest: chain data and tomb data ship together, so a
mismatch is a build error, not a runtime state.
================
*/
func (s *MonsterState) InstallTombChain() error {
	return s.InstallTombChainDef(TombChainDefaultDef())
}

/*
================
MonsterState.InstallTombChainDef

Installs a chain definition against this state's template.
================
*/
func (s *MonsterState) InstallTombChainDef(def TombChainDef) error {
	chain := &TombChain{
		byName:      map[string]int{},
		dependents:  map[string][]int{},
		gateFloor:   map[string]int{},
		populations: map[*divisionMonsterState]*tombPopulation{},
	}
	for floorIndex, floor := range def.Floors {
		for _, spawner := range floor.Spawners {
			if spawner.Name == "" || spawner.Codename == "" || spawner.Count <= 0 {
				return fmt.Errorf("tomb chain spawner %+v: name, codename and a positive count are required", spawner)
			}
			if _, dup := chain.byName[spawner.Name]; dup {
				return fmt.Errorf("tomb chain spawner %s defined twice", spawner.Name)
			}
			if !spawner.Initialize && len(spawner.Needed) == 0 {
				return fmt.Errorf("tomb chain spawner %s is neither initialized nor gated", spawner.Name)
			}
			nest, ok := s.tombChainNest(spawner.Codename)
			if !ok {
				return fmt.Errorf("tomb chain spawner %s: no tomb nest for %s", spawner.Name, spawner.Codename)
			}
			chain.byName[spawner.Name] = len(chain.spawners)
			chain.spawners = append(chain.spawners, tombResolvedSpawner{def: spawner, floor: floorIndex, nest: nest})
		}
	}
	for index, resolved := range chain.spawners {
		for _, needed := range resolved.def.Needed {
			neededIndex, ok := chain.byName[needed]
			if !ok {
				return fmt.Errorf("tomb chain spawner %s needs unknown spawner %s", resolved.def.Name, needed)
			}
			chain.dependents[chain.spawners[neededIndex].def.Name] = append(chain.dependents[chain.spawners[neededIndex].def.Name], index)
		}
		if resolved.def.Trigger != "" {
			if _, isSpawner := chain.byName[resolved.def.Trigger]; !isSpawner {
				chain.gateFloor[resolved.def.Trigger] = resolved.floor
			}
		}
	}
	s.tombChain = chain
	return nil
}

/*
================
MonsterState.tombChainNest

The tomb nest row a chain codename spawns from (its authored point and
tactics). Chain rows carry max count 0, so the nest lifecycle never
spawns them; the first authored row wins.
================
*/
func (s *MonsterState) tombChainNest(codename string) (monster.NestRow, bool) {
	for _, nest := range s.template.Nests {
		ref, ok := s.template.Refs[nest.RefObjID]
		if ok && ref.Codename == codename {
			return nest, true
		}
	}
	return monster.NestRow{}, false
}

/*
================
TombChain.population

The population's chain state, created (unarmed) on first touch.
================
*/
func (c *TombChain) population(state *divisionMonsterState, floors int) *tombPopulation {
	pop, ok := c.populations[state]
	if !ok {
		pop = &tombPopulation{
			floors:   make([]tombFloorState, floors),
			gates:    map[string]bool{},
			liveGids: map[uint32]string{},
		}
		for i := range pop.floors {
			pop.floors[i].stages = map[string]*tombStage{}
		}
		c.populations[state] = pop
	}
	return pop
}

/*
================
TombChain.tickLocked

Arms the chain for a population that has none running, and runs the
rearm timer. Called from the population advance with the state lock
held.
================
*/
func (c *TombChain) tickLocked(s *MonsterState, state *divisionMonsterState, nowMs int64) {
	pop := c.population(state, c.floorCount())
	if !pop.armed {
		c.armLocked(s, state, pop, nowMs)
		return
	}
	for floor := range pop.floors {
		floorState := &pop.floors[floor]
		if floorState.rearmAtMs == 0 || nowMs < floorState.rearmAtMs {
			continue
		}
		if !c.floorIdle(pop, floor) {
			// A straggler is still in combat; check again shortly
			// rather than resetting a live stage (inference guard).
			floorState.rearmAtMs = nowMs + 30_000
			continue
		}
		c.resetFloor(pop, floor)
		c.armFloorLocked(s, state, pop, floor, nowMs)
	}
}

/*
================
TombChain.floorCount
================
*/
func (c *TombChain) floorCount() int {
	max := 0
	for _, spawner := range c.spawners {
		if spawner.floor+1 > max {
			max = spawner.floor + 1
		}
	}
	return max
}

/*
================
TombChain.floorIdle

No stage of the floor has a live instance.
================
*/
func (c *TombChain) floorIdle(pop *tombPopulation, floor int) bool {
	for _, stage := range pop.floors[floor].stages {
		if stage.live > 0 {
			return false
		}
	}
	return true
}

/*
================
TombChain.resetFloor
================
*/
func (c *TombChain) resetFloor(pop *tombPopulation, floor int) {
	floorState := &pop.floors[floor]
	floorState.stages = map[string]*tombStage{}
	floorState.rearmAtMs = 0
	for gate, gateF := range c.gateFloor {
		if gateF == floor {
			delete(pop.gates, gate)
		}
	}
}

/*
================
TombChain.armLocked
================
*/
func (c *TombChain) armLocked(s *MonsterState, state *divisionMonsterState, pop *tombPopulation, nowMs int64) {
	for floor := 0; floor < c.floorCount(); floor++ {
		c.armFloorLocked(s, state, pop, floor, nowMs)
	}
	pop.armed = true
}

/*
================
TombChain.armFloorLocked

Spawns the floor's Initialize spawners (the native InitTriggers tail
raises Initialize on exactly these).
================
*/
func (c *TombChain) armFloorLocked(s *MonsterState, state *divisionMonsterState, pop *tombPopulation, floor int, nowMs int64) {
	for index := range c.spawners {
		resolved := &c.spawners[index]
		if resolved.floor != floor || !resolved.def.Initialize {
			continue
		}
		c.spawnSpawnerLocked(s, state, pop, resolved, nowMs)
	}
}

/*
================
TombChain.spawnSpawnerLocked

Spawns the spawner's Count instances at its nest point and records
them as the stage's live set. One spawn per stage per arm: the stage
never refills on its own (the tomb tomb nests are max count 0).
================
*/
func (c *TombChain) spawnSpawnerLocked(s *MonsterState, state *divisionMonsterState, pop *tombPopulation, resolved *tombResolvedSpawner, nowMs int64) {
	stage := pop.floors[resolved.floor].stages[resolved.def.Name]
	if stage == nil {
		stage = &tombStage{}
		pop.floors[resolved.floor].stages[resolved.def.Name] = stage
	}
	if stage.spawned {
		return
	}
	stage.spawned = true
	for i := 0; i < resolved.def.Count; i++ {
		gid, ok := s.spawnChainInstanceLocked(state, resolved.nest, nowMs)
		if !ok {
			return
		}
		stage.live++
		pop.liveGids[gid] = resolved.def.Name
	}
}

/*
================
TombChain.onInstanceDefeated

One tracked instance died. The stage fires when its live set empties
(the native OnDead erase): dependents whose Needed set is complete
spawn, and a boss stage opens its gate and starts the rearm window.
Called from Defeat with the state lock held.
================
*/
func (c *TombChain) onInstanceDefeated(s *MonsterState, state *divisionMonsterState, gid uint32, nowMs int64) {
	pop, ok := c.populations[state]
	if !ok {
		return
	}
	name, tracked := pop.liveGids[gid]
	if !tracked {
		return
	}
	delete(pop.liveGids, gid)
	index := c.byName[name]
	resolved := &c.spawners[index]
	stage := pop.floors[resolved.floor].stages[name]
	if stage == nil {
		return
	}
	if stage.live > 0 {
		stage.live--
	}
	if stage.live > 0 || stage.fired {
		return
	}
	stage.fired = true
	if floor, isGate := c.gateFloor[resolved.def.Trigger]; isGate && resolved.def.Trigger != "" {
		pop.gates[resolved.def.Trigger] = true
		intervalMs, _ := monster.NestDelay(tombRearmMinSec, tombRearmMaxSec, 1, s.randomWord)
		pop.floors[floor].rearmAtMs = nowMs + int64(intervalMs)
	}
	for _, dependent := range c.dependents[name] {
		candidate := &c.spawners[dependent]
		candidateStage := pop.floors[candidate.floor].stages[candidate.def.Name]
		if candidateStage != nil && candidateStage.spawned {
			continue
		}
		if c.neededComplete(pop, candidate) {
			c.spawnSpawnerLocked(s, state, pop, candidate, nowMs)
		}
	}
}

/*
================
TombChain.neededComplete

Every spawner in the candidate's Needed set has fired (latched: a
fired stage stays fired until the floor re-arms).
================
*/
func (c *TombChain) neededComplete(pop *tombPopulation, candidate *tombResolvedSpawner) bool {
	for _, needed := range candidate.def.Needed {
		neededIndex := c.byName[needed]
		stage := pop.floors[c.spawners[neededIndex].floor].stages[needed]
		if stage == nil || !stage.fired {
			return false
		}
	}
	return true
}

/*
================
MonsterState.spawnChainInstanceLocked

Creates one chain instance at its nest's authored point, mirroring
attemptNestSpawn's instance creation minus the nest lifecycle: no
respawn bookkeeping (the chain owns this life), the authored point
stands as authored (MobShower spawns on the spot), and the instance is
nest-detached so Defeat accepts it.
================
*/
func (s *MonsterState) spawnChainInstanceLocked(state *divisionMonsterState, nest monster.NestRow, nowMs int64) (uint32, bool) {
	ref, ok := s.template.Refs[nest.RefObjID]
	if !ok || s.counter >= domain.MaxMonsterGIDCounter {
		return 0, false
	}
	s.counter++
	gid := monster.GidBase + s.counter
	instance := monster.Instance{
		Gid:          gid,
		Ref:          ref,
		Nest:         nest,
		Spawn:        nest.SpawnPoint,
		NestDetached: true,
	}
	instance.CurrentHP = instance.EffectiveMaxHP()
	state.instances.set(gid, instance)
	state.spawns++
	armLifetimeLocked(state, instance, nowMs)
	if instance.Rarity()&15 == 3 {
		state.uniqueNotices = append(state.uniqueNotices, uniqueNotice(5, ref.RefObjID, ""))
		state.markUniqueSpawn(ref.RefObjID, nest.RegionID, nowMs)
	}
	if state.movers == nil {
		state.movers = newMoverStorage(nil)
	}
	mover := monster.NewSpawnMover(instance, nowMs)
	mover.Activity = monster.NewActivityCadence(uint32(nowMs), s.randomWord())
	state.movers.set(gid, mover)
	state.behavior.set(gid, 0)
	state.listRegionGid(nest.RegionID, gid)
	return gid, true
}

/*
================
MonsterState.TombGateOpen

Whether a chain gate (a portal) is open in the division. No chain (the
flag is off, or this build never installed one) reads closed.
================
*/
func (s *MonsterState) TombGateOpen(division string, gate string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tombChain == nil {
		return false
	}
	state := s.divs[division]
	if state == nil {
		return false
	}
	pop, ok := s.tombChain.populations[state]
	return ok && pop.gates[gate]
}
