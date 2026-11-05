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

Floor 6 (slice B, #2373): four independent stages, each its own rearm
unit below. East and North are one snake each (the Medusa pair), South
is the SNAKEGENERAL and its three clones under one trigger, West is
fifteen codenames five each. A stage's portal (a gate here) opens when
every spawner naming it is fully dead; the arena exit gate names no
spawner - it follows the first entry gate, as the native entry
portals each name it as their trigger (the exit stands on the arena,
reachable only through an entry portal, so the two readings agree).
West and South spawn at the native stage point carried in the
definition: most of their codenames have no Floor 6 nest row of their
own, and a codename's first nest elsewhere would be the wrong floor.

Two native points the ASM pass could not isolate are inference, shipped
as the scope rule requires:
  - Fired triggers latch: a dead guardian stays counted. (The only
    reading under which the Flame Master gate can ever open.)
  - A floor re-arms BossRespawnWindowSec after its gate opens. Natively
    the chain resets by the tomb instance closing and reopening on a
    schedule; the port keeps the tomb permanently open (#1013), so the
    floor's unique respawn window (1800..3600 s, the Floor 1-3 unique
    rows) stands in for the reset.

===========================================================================
*/
package simulation

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/monster"
)

// TombGateFloor6Entry is the Floor 6 entry gate: the walk-in zones on
// Floor 5 teleport only while this gate is open.
const TombGateFloor6Entry = "Chins_Floor5_Portal"

// The Floor 6 arena gates (#2373): one entry portal per stage, and the
// arena exit, which follows the first entry gate (see the header).
const (
	TombGateFloor6Portal1 = "Chins_Floor6_Portal1"
	TombGateFloor6Portal2 = "Chins_Floor6_Portal2"
	TombGateFloor6Portal3 = "Chins_Floor6_Portal3"
	TombGateFloor6Portal5 = "Chins_Floor6_Portal5"
	TombGateFloor6Portal4 = "Chins_Floor6_Portal4"
)

// Boss window after the floor boss dies before the chain re-arms
// (inference above; the Floor 1-3 unique respawn window).
const (
	tombRearmMinSec = 1800
	tombRearmMaxSec = 3600
)

/*
================
TombSpawnerDef

One native WaveSpawner definition: Count instances of Codename.
Initialize spawners activate when the chain arms; the others activate
when every spawner in Needed has fired. Trigger names what a completed
stage fires: another spawner (through its Needed set) or a gate (a
portal), for spawners that end a floor. A stage may be several
spawners under one gate Trigger; the gate opens when all of them are
fully dead.

A zero Region spawns at the codename's tomb nest point (Floor 5). A
nonzero Region spawns at (X, Y, Z) in that region - the native stage
point, for the Floor 6 codenames that have no nest of their own.
================
*/
type TombSpawnerDef struct {
	Name       string
	Codename   string
	Count      int
	Initialize bool
	Trigger    string
	Needed     []string
	Region     uint16
	X          float64
	Y          float64
	Z          float64
}

/*
================
TombFloorDef

One rearm unit's spawners: a floor on Floor 5, one compass stage on
Floor 6. The spawners whose Trigger is a gate (not a spawner name)
end the unit: the last of them to finish opens the gate and starts
the rearm window.
================
*/
type TombFloorDef struct {
	Spawners []TombSpawnerDef
}

/*
================
TombDerivedGate

A gate with no spawner of its own: it reads open while any gate in
AnyOf is open (the Floor 6 arena exit, named as a trigger by all four
entry portals natively).
================
*/
type TombDerivedGate struct {
	Name  string
	AnyOf []string
}

/*
================
TombChainDef
================
*/
type TombChainDef struct {
	Floors  []TombFloorDef
	Derived []TombDerivedGate
}

/*
================
TombChainDefaultDef

The chain as extracted from the two native InitTriggers bodies
(post-cut/2362-tomb-asm.md). Floor 5 spawners resolve to their tomb
nest rows at install time: the native positions are in the later
server's global dungeon frame, the nest rows carry the port's
region-local frame. Floor 6 positions come from the definition: the
native stage points are already region-local (region 0x8002), and
most Floor 6 codenames have no nest of their own.
================
*/
func TombChainDefaultDef() TombChainDef {
	flameMaster := "Chins_Floor5_FlameMaster"
	floor6 := uint16(0x8002)
	point := func(name, codename string, count int, trigger string, x, y, z float64) TombSpawnerDef {
		return TombSpawnerDef{Name: name, Codename: codename, Count: count, Initialize: true,
			Trigger: trigger, Region: floor6, X: x, Y: y, Z: z}
	}
	west := []TombSpawnerDef{}
	for _, codename := range []string{
		"MOB_TQ_TOMBBUGGHOST_CLON2", "MOB_TQ_SNAKESLAVE_CLON2", "MOB_TQ_TOMBBUGGHOST_CLON",
		"MOB_TQ_SNAKEWOMAN_CLON2", "MOB_TQ_TOMBBUGGHOST", "MOB_TQ_SNAKESLAVE_CLON",
		"MOB_TQ_SNAKEWOMAN_CLON", "MOB_TQ_SNAKEDEMON_CLON2", "MOB_TQ_SNAKESLAVE",
		"MOB_TQ_SNAKEDEMON_CLON", "MOB_TQ_SNAKEMAN_CLON2", "MOB_TQ_SNAKEWOMAN",
		"MOB_TQ_SNAKEMAN_CLON", "MOB_TQ_SNAKEDEMON", "MOB_TQ_SNAKEMAN",
	} {
		west = append(west, point("Chins_Floor6_West_"+strings.TrimPrefix(codename, "MOB_TQ_"),
			codename, 5, TombGateFloor6Portal3, -8199, 132, 25))
	}
	return TombChainDef{
		Floors: []TombFloorDef{
			{Spawners: []TombSpawnerDef{
				{Name: "Chins_Floor5_East", Codename: "MOB_TQ_EASTGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
				{Name: "Chins_Floor5_South", Codename: "MOB_TQ_SOUTHGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
				{Name: "Chins_Floor5_West", Codename: "MOB_TQ_WESTGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
				{Name: "Chins_Floor5_North", Codename: "MOB_TQ_NORTHGUARDIAN", Count: 1, Initialize: true, Trigger: flameMaster},
				{Name: flameMaster, Codename: "MOB_TQ_FLAMEMASTER", Count: 1, Trigger: TombGateFloor6Entry,
					Needed: []string{"Chins_Floor5_East", "Chins_Floor5_South", "Chins_Floor5_West", "Chins_Floor5_North"}},
			}},
			{Spawners: []TombSpawnerDef{
				point("Chins_Floor6_East", "MOB_TQ_BLACKSNAKE", 1, TombGateFloor6Portal1, 8207, 132, -10),
			}},
			{Spawners: []TombSpawnerDef{
				point("Chins_Floor6_South_SNAKEGENERAL", "MOB_TQ_SNAKEGENERAL", 1, TombGateFloor6Portal2, -17, 132, -8213),
				point("Chins_Floor6_South_SNAKEGENERAL_CLON", "MOB_TQ_SNAKEGENERAL_CLON", 1, TombGateFloor6Portal2, -17, 132, -8213),
				point("Chins_Floor6_South_SNAKEGENERAL_CLON2", "MOB_TQ_SNAKEGENERAL_CLON2", 1, TombGateFloor6Portal2, -17, 132, -8213),
				point("Chins_Floor6_South_SNAKEGENERAL_CLON3", "MOB_TQ_SNAKEGENERAL_CLON3", 1, TombGateFloor6Portal2, -17, 132, -8213),
			}},
			{Spawners: west},
			{Spawners: []TombSpawnerDef{
				point("Chins_Floor6_North", "MOB_TQ_WHITESNAKE", 1, TombGateFloor6Portal5, 37, 132, 8199),
			}},
		},
		Derived: []TombDerivedGate{
			{Name: TombGateFloor6Portal4, AnyOf: []string{
				TombGateFloor6Portal1, TombGateFloor6Portal2, TombGateFloor6Portal3, TombGateFloor6Portal5}},
		},
	}
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
	derived     map[string][]string
	populations map[*divisionMonsterState]*tombPopulation
}

/*
================
MonsterState.InstallTombChain

Installs the default chain (Floors 5 and 6). Fails when the template
lacks a chain codename's nest - or, for a point spawner, its ref:
chain data and tomb data ship together, so a mismatch is a build
error, not a runtime state.
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
		derived:     map[string][]string{},
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
			nest, err := s.tombChainSpawnerNest(spawner)
			if err != nil {
				return err
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
	for _, derived := range def.Derived {
		if derived.Name == "" || len(derived.AnyOf) == 0 {
			return fmt.Errorf("tomb chain derived gate %+v: a name and at least one source gate are required", derived)
		}
		if _, isSpawner := chain.byName[derived.Name]; isSpawner {
			return fmt.Errorf("tomb chain derived gate %s names a spawner", derived.Name)
		}
		for _, gate := range derived.AnyOf {
			if _, ok := chain.gateFloor[gate]; !ok {
				return fmt.Errorf("tomb chain derived gate %s follows unknown gate %s", derived.Name, gate)
			}
		}
		chain.derived[derived.Name] = derived.AnyOf
	}
	s.tombChain = chain
	return nil
}

/*
================
MonsterState.tombChainSpawnerNest

The nest row a spawner spawns from. A point spawner (nonzero Region)
places its row at the native stage point - the instance still
carries a nest, just one no lifecycle owns - and takes its AI
fields from the codename's authored nest in the region when it
carries one. A nest spawner needs the codename's authored tomb nest.
================
*/
func (s *MonsterState) tombChainSpawnerNest(spawner TombSpawnerDef) (monster.NestRow, error) {
	if spawner.Region != 0 {
		ref, ok := s.tombChainRef(spawner.Codename)
		if !ok {
			return monster.NestRow{}, fmt.Errorf("tomb chain spawner %s: no ref for %s", spawner.Name, spawner.Codename)
		}
		point := monster.SpawnPoint{
			RefObjID: ref.RefObjID, RegionID: spawner.Region, X: spawner.X, Y: spawner.Y, Z: spawner.Z,
		}
		// An anchor whose codename carries an authored nest in this
		// region (Floor 6's snakes) spawns with that nest's AI fields
		// at the stage point: the bare point row resolves to no
		// tactics contract at all, so the unique stood inert under
		// any amount of damage (#2600).
		if authored, ok := s.tombChainRegionNest(spawner.Codename, spawner.Region); ok {
			authored.SpawnPoint = point
			return authored, nil
		}
		// The wave codenames carry refs only. A pinned point nest -
		// the quest spawn shape - resolves their tactics to the
		// retail wander contract instead of nothing, so a damaged
		// wave mob retaliates like any other spawn.
		return monster.NestRow{SpawnPoint: point, PolicyPinned: true}, nil
	}
	nest, ok := s.tombChainNest(spawner.Codename)
	if !ok {
		return monster.NestRow{}, fmt.Errorf("tomb chain spawner %s: no tomb nest for %s", spawner.Name, spawner.Codename)
	}
	return nest, nil
}

/*
================
MonsterState.tombChainRegionNest

The codename's authored tomb nest in one region, when it carries
one. The same codename can nest on other floors (the Snake General
on Floor 3); only the spawner's own region row describes this
stage.
================
*/
func (s *MonsterState) tombChainRegionNest(codename string, region uint16) (monster.NestRow, bool) {
	for _, nest := range s.template.Nests {
		if nest.RegionID != region {
			continue
		}
		if ref, ok := s.template.Refs[nest.RefObjID]; ok && ref.Codename == codename {
			return nest, true
		}
	}
	return monster.NestRow{}, false
}

/*
================
MonsterState.tombChainRef

The template ref for a codename (chain codenames are unique across
the template: they all come from the tomb overlay).
================
*/
func (s *MonsterState) tombChainRef(codename string) (monster.MonsterRef, bool) {
	for _, ref := range s.template.Refs {
		if ref.Codename == codename {
			return ref, true
		}
	}
	return monster.MonsterRef{}, false
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

Spawns the spawner's Count instances at its spawn point and records
them as the stage's live set. One spawn per stage per arm: the stage
never refills on its own (the chain owns these lives).
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
spawn, and when every spawner under a gate has fired the gate opens
and its floor starts the rearm window. Called from Defeat with the
state lock held.
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
	if floor, isGate := c.gateFloor[resolved.def.Trigger]; isGate && resolved.def.Trigger != "" && c.gateComplete(pop, resolved.def.Trigger) {
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
TombChain.gateComplete

Every spawner naming this gate as its Trigger has fired: a stage of
several spawners (Floor 6's South and West) opens its portal only
when the whole stage is dead, never when the first spawner empties.
================
*/
func (c *TombChain) gateComplete(pop *tombPopulation, gate string) bool {
	for index := range c.spawners {
		spawner := &c.spawners[index]
		if spawner.def.Trigger != gate {
			continue
		}
		stage := pop.floors[spawner.floor].stages[spawner.def.Name]
		if stage == nil || !stage.fired {
			return false
		}
	}
	return true
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
	if !ok {
		return false
	}
	if pop.gates[gate] {
		return true
	}
	for _, source := range s.tombChain.derived[gate] {
		if pop.gates[source] {
			return true
		}
	}
	return false
}
