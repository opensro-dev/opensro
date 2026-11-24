/*
===========================================================================

temple_sanctum.go - the temple sanctums' scheduled gates (#3787)

Port-only, not native to v1.150: the job temple rides SRO_PHARAOH_TEMPLE.
The later server opens each pair of sanctums twice a day and lets players
in through gates it places itself (pharaohtemple/sanctum.go has the
table and its evidence). Without them no sanctum can be entered.

What this owns, per division, each tick:

	open    half an hour into a pair's window (pharaohtemple.OpenAfter)
	        the pair's enter gates stand in the Temple and their leave
	        gates in the sanctums. The Haroeris & Seth window also
	        spawns Haroeris once, where the native wave places him.
	seth    when that Haroeris is dead, Seth's enter gate (in Haroeris's
	        sanctum) and Seth's leave gate stand: the native wave fires
	        its trigger once its one monster is gone.
	close   at the window's end every gate of the pair comes down, and
	        everyone still inside one of its sanctums leaves through the
	        sanctum's leave target, as the native world's end returns its
	        residents. Haroeris's lifetime ends with the window.

The port keeps the sanctums as regions of the persistent world (the
temple's model, pharaohtemple), not as instance worlds, so the gates
stand in the open world and are scoped to their division.

Not here yet, each its own stage of #3787: the countdown notices, the
temple association reputation the native entry check reads, and the
keepers living only while their sanctum is open.

===========================================================================
*/
package action

import (
	"log"
	"sync"
	"time"

	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// haroerisCodename is the one monster of the native wave
	// Temple_Of_Haroeris (a MobShower of count 1).
	haroerisCodename = "MOB_SD_HAROERIS"
	// sanctumGateLabel names a sanctum gate's move in the store.
	sanctumGateLabel = "temple-sanctum-gate"
	// sanctumCloseLabel names the move out of a closing sanctum.
	sanctumCloseLabel = "temple-sanctum-close"
)

// haroerisSpawn is the wave's fixed point: region -32754 (0x800E),
// position (9919, 100, 24).
var haroerisSpawn = simulation.Spawn{RegionID: 0x800E, X: 9919, Y: 100, Z: 24}

/*
================
sanctumRuntime

The temple's sanctum state, per division. Nil while the temple is off.
================
*/
type sanctumRuntime struct {
	mu        sync.Mutex
	loc       *time.Location
	divisions map[string]*sanctumDivision
	spawn     func(simulation.QuestMonsterSpawn) (uint32, bool)
	alive     func(division string, gid uint32) bool
}

/*
================
sanctumDivision

One division's open pairs, by schedule define.
================
*/
type sanctumDivision struct {
	open map[int]*sanctumPair
}

/*
================
sanctumPair

One open pair: the window it opened for, the gates standing for each
sanctum group, and Haroeris's gid (zero for the other pairs, or when he
did not spawn).
================
*/
type sanctumPair struct {
	windowStartMs int64
	windowEndMs   int64
	gates         map[string][]uint32
	haroeris      uint32
	sethOpened    bool
}

/*
================
ConfigureTempleSanctums

Turns the sanctum gates on with the temple (SRO_PHARAOH_TEMPLE). Off
leaves rt.sanctums nil: no gate stands and every hook returns at once.
================
*/
func (rt *Runtime) ConfigureTempleSanctums(enabled bool) {
	if !enabled || rt.Monsters == nil {
		rt.sanctums = nil
		return
	}
	rt.sanctums = &sanctumRuntime{
		loc:       time.Local,
		divisions: map[string]*sanctumDivision{},
		spawn:     rt.Monsters.SpawnQuestMonsterGID,
		alive:     rt.Monsters.QuestMonsterAlive,
	}
}

/*
================
advanceTempleSanctums

The tick's pass over every division: opens, chains and closes each pair
as its windows say.
================
*/
func (rt *Runtime) advanceTempleSanctums(nowMs int64) {
	s := rt.sanctums
	if s == nil || rt.Monsters == nil || rt.portals == nil {
		return
	}
	now := rt.Now()
	for _, population := range rt.Monsters.OpenWorldPopulations() {
		unlock := rt.lockDivision(population.Division)
		for _, define := range []int{
			pharaohtemple.ScheduleSelkisNeith, pharaohtemple.ScheduleAnubisIsis, pharaohtemple.ScheduleHaroerisSeth,
		} {
			rt.advanceSanctumPair(population, define, now, nowMs)
		}
		unlock()
	}
}

/*
================
sanctumOpenWindow

The window of a define whose gates stand at now: its start and end, and
true from the open (OpenAfter in) to the end.
================
*/
func sanctumOpenWindow(define int, now time.Time, loc *time.Location) (time.Time, time.Time, bool) {
	for _, window := range pharaohtemple.Windows(define) {
		start, end, in := window.Current(now, loc)
		if in && !now.Before(start.Add(pharaohtemple.OpenAfter)) {
			return start, end, true
		}
	}
	return time.Time{}, time.Time{}, false
}

/*
================
advanceSanctumPair

One pair in one division. The caller holds the division lock.
================
*/
func (rt *Runtime) advanceSanctumPair(population simulation.WorldPopulation, define int, now time.Time, nowMs int64) {
	s := rt.sanctums
	division := population.Division
	start, end, open := sanctumOpenWindow(define, now, s.loc)
	s.mu.Lock()
	state := s.divisions[division]
	if state == nil {
		state = &sanctumDivision{open: map[int]*sanctumPair{}}
		s.divisions[division] = state
	}
	pair := state.open[define]
	s.mu.Unlock()
	if pair != nil && (!open || pair.windowStartMs != start.UnixMilli()) {
		rt.closeSanctumPair(population, define, pair)
		s.mu.Lock()
		delete(state.open, define)
		s.mu.Unlock()
		pair = nil
	}
	if !open {
		return
	}
	if pair == nil {
		pair = rt.openSanctumPair(population, define, start, end, nowMs)
		s.mu.Lock()
		state.open[define] = pair
		s.mu.Unlock()
	}
	if pair.haroeris != 0 && !pair.sethOpened && !s.alive(division, pair.haroeris) {
		pair.sethOpened = true
		for _, sanctum := range pharaohtemple.Sanctums() {
			if sanctum.Schedule == define && sanctum.OpenedBy == haroerisCodename {
				pair.gates[sanctum.Group] = rt.standSanctumGates(population, sanctum)
			}
		}
	}
}

/*
================
openSanctumPair

Stands the gates of every sanctum the schedule opens and, for the
Haroeris & Seth pair, spawns Haroeris for the rest of the window.
================
*/
func (rt *Runtime) openSanctumPair(population simulation.WorldPopulation, define int, start, end time.Time, nowMs int64) *sanctumPair {
	pair := &sanctumPair{windowStartMs: start.UnixMilli(), windowEndMs: end.UnixMilli(), gates: map[string][]uint32{}}
	for _, sanctum := range pharaohtemple.Sanctums() {
		if sanctum.Schedule != define {
			continue
		}
		if sanctum.OpenedBy == "" {
			pair.gates[sanctum.Group] = rt.standSanctumGates(population, sanctum)
		}
		if sanctum.Keeper == haroerisCodename {
			pair.haroeris = rt.spawnHaroeris(population, pair.windowEndMs-nowMs, nowMs)
		}
	}
	return pair
}

/*
================
spawnHaroeris

The wave's one monster at its fixed point, living until the window
ends. Zero when the data lacks him: Seth's gate then never opens, as the
native wave whose spawn failed never fires its trigger.
================
*/
func (rt *Runtime) spawnHaroeris(population simulation.WorldPopulation, lifetimeMs, nowMs int64) uint32 {
	gid, ok := rt.sanctums.spawn(simulation.QuestMonsterSpawn{
		Division: population.Division, Population: population.Lease, Codename: haroerisCodename,
		Position: haroerisSpawn, FixedPosition: true, NowMs: nowMs, LifetimeMs: lifetimeMs,
	})
	if !ok {
		log.Printf("temple: division %s: %s did not spawn", population.Division, haroerisCodename)
		return 0
	}
	return gid
}

/*
================
standSanctumGates

A sanctum's enter gate and leave gate, standing in the division's open
world. Answers the gids that stood; a building the data lacks stands
nothing and says so once in the log.
================
*/
func (rt *Runtime) standSanctumGates(population simulation.WorldPopulation, sanctum pharaohtemple.Sanctum) []uint32 {
	world := population.Lease.ID
	var gids []uint32
	for _, gate := range []struct {
		building   string
		at, target pharaohtemple.Point
	}{
		{sanctum.EnterTeleporter(), sanctum.Enter, sanctum.EnterTarget},
		{pharaohtemple.TempleTeleporter, sanctum.Leave, sanctum.LeaveTarget},
	} {
		building := rt.portals.teleporters[gate.building]
		if building == nil {
			log.Printf("temple: %s: no %s building in the data", sanctum.Group, gate.building)
			continue
		}
		to := travelPoint{spawn: sanctumSpawn(gate.target), world: world}
		gid, ok := rt.placeGate(questGate{
			world: world, division: population.Division, spawn: sanctumSpawn(gate.at),
			to: &to, building: building, label: sanctumGateLabel,
		})
		if ok {
			gids = append(gids, gid)
		}
	}
	return gids
}

/*
================
closeSanctumPair

Takes the pair's gates down and moves everyone still inside one of its
sanctums out through that sanctum's leave target.
================
*/
func (rt *Runtime) closeSanctumPair(population simulation.WorldPopulation, define int, pair *sanctumPair) {
	for _, gids := range pair.gates {
		for _, gid := range gids {
			rt.removeGate(gid)
		}
	}
	targets := map[uint16]travelPoint{}
	for _, sanctum := range pharaohtemple.Sanctums() {
		if sanctum.Schedule == define {
			targets[sanctum.Region] = travelPoint{spawn: sanctumSpawn(sanctum.LeaveTarget), world: population.Lease.ID}
		}
	}
	for _, c := range rt.worldResidents(population.Division, population.Lease.ID) {
		state, ok := rt.Worlds.SnapshotExisting(simulation.WorldKey(population.Division, c.Name))
		if !ok {
			continue
		}
		target, inside := targets[state.Spawn.RegionID]
		if !inside {
			continue
		}
		rt.relocateCharacter(population.Division, c, sanctumCloseLabel, func() (travelPoint, bool) { return target, true })
	}
}

/*
================
sanctumSpawn

A table point as a world spawn.
================
*/
func sanctumSpawn(point pharaohtemple.Point) simulation.Spawn {
	return simulation.Spawn{RegionID: point.Region, X: float64(point.X), Y: float64(point.Y), Z: float64(point.Z)}
}
