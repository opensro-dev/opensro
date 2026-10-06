/*
===========================================================================

monsterstate_lifecycle.go - native hive and nest population lifecycle

===========================================================================
*/
package simulation

import (
	"math"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/monster"
)

// Native nest population lifecycle. Each world's CAIHive owns one timer entry
// per nest; CNest owns the party-monster flag.
//
//	55E910  hive callback: runs the hive's mode at most once per 1000 ms.
//	55EA90  ordinary hive: every enabled nest whose interval elapsed and whose
//	        live count is below dwMaxTotalCount gets one 5607B0 attempt.
//	55EC10  overwrite hive (dwOverwriteMaxTotalCount > 0): while the shared
//	        live count is below the limit, only the selected alternate nest,
//	        or else each member in hive order, gets one attempt until full.
//	5607B0  success: live+1, last=now, interval re-rolled (560DC0/560E40) and
//	        a party grade clears CNest +30. A clipped creation halves the
//	        interval (560E10); any other failure retries next callback.
//	560D00  death: a nest dying from full restarts its timer (last=now); an
//	        overwrite hive dying from full picks the next location and restarts
//	        every member (55EEC0); then live-1 (560DF0).
//	54BD2D  world init: last=0, interval rolled, enabled=!btFlag,
//	        respawn=btRespawn, remaining=dwMaxTotalCount.
//
// World creation initializes every nest timer. Only elapsed hive callbacks
// create monsters; observing a region never fast-forwards or retries spawns.

// nestRuntime is one nest's CAIHive +1C timer entry plus CNest +30.
/*
================
nestRuntime
================
*/
type nestRuntime struct {
	lastMs     int64   // +00 last spawn or restart
	intervalMs uint32  // +04 after the spawn-speed reduction
	live       int     // +08
	ratePct    float32 // +0C spawn-speed increase percent (hive density)
	reduceMs   uint32  // +10
	enabled    bool    // +14 !btFlag
	respawn    bool    // +18 btRespawn
	remaining  int     // +1C spawns left for a non-respawning nest
	partyArmed bool    // CNest +30
	// tickMs is the hive callback phase of a nest outside an overwrite hive.
	tickMs    int64
	scheduled int64
}

/*
================
bool
================
*/
func (n *nestRuntime) elapsed(nowMs int64) bool {
	return nowMs >= n.lastMs+int64(n.intervalMs)
}

// ordinaryEligible is 55EB0F..55EB43 without the elapsed-timer test.
/*
================
bool
================
*/
func (n *nestRuntime) ordinaryEligible(limit int) bool {
	return n.enabled && (n.respawn || n.remaining > 0) && n.live < limit
}

// hiveRuntime is an overwrite hive's +28 callback tick, +30 live count and
// +3C selected nest (-1 for none).
/*
================
hiveRuntime
================
*/
type hiveRuntime struct {
	density   monster.HiveDensity
	ratePct   float32
	tickMs    int64
	live      int
	selected  int
	scheduled int64
}

// spawnGroup is one hive callback: an overwrite hive, or a single nest.
/*
================
spawnGroup
================
*/
type spawnGroup struct {
	hive string
	nest int
}

/*
================
spawnTick
================
*/
type spawnTick struct {
	dueMs int64
	group spawnGroup
}

type spawnQueue []spawnTick

/*
================
int
================
*/
func (q spawnQueue) Len() int { return len(q) }

/*
================
bool
================
*/
func (q spawnQueue) Less(i, j int) bool {
	if q[i].dueMs != q[j].dueMs {
		return q[i].dueMs < q[j].dueMs
	}
	if q[i].group.hive != q[j].group.hive {
		return q[i].group.hive < q[j].group.hive
	}
	return q[i].group.nest < q[j].group.nest
}

/*
================
Swap
================
*/
func (q spawnQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

/*
================
Push
================
*/
func (q *spawnQueue) Push(value any) {
	*q = append(*q, value.(spawnTick))
}

/*
================
any
================
*/
func (q *spawnQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]
	return last
}

// SetTimeSource replaces the population clock. Composition should install it
// before listeners start; tests use it to drive respawn deterministically.
/*
================
time
================
*/
func (s *MonsterState) SetTimeSource(clock func() time.Time) {
	if clock == nil {
		panic("simulation: nil monster time source")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = clock
}

// SetRandomSource replaces the population PRNG. Every population draw is
// projected into the VC CRT rand() domain; it is always called under s.mu.
/*
================
float64
================
*/
func (s *MonsterState) SetRandomSource(random func() float64) {
	if random == nil {
		panic("simulation: nil monster random source")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.random = random
}

/*
================
int64
================
*/
func (s *MonsterState) nowMillis() int64 {
	return s.clock().UnixMilli()
}

/*
================
uint32
================
*/
func (s *MonsterState) randomWord() uint32 {
	return monster.SummonRandomWord(s.random())
}

// nextHiveTick is the first hive callback at or after dueMs; callbacks run
// once per NestHiveTickMs from the previous one.
/*
================
nextHiveTick
================
*/
func nextHiveTick(tickMs, dueMs int64) int64 {
	next := tickMs + monster.NestHiveTickMs
	if dueMs > next {
		steps := (dueMs - tickMs + monster.NestHiveTickMs - 1) / monster.NestHiveTickMs
		next = tickMs + steps*monster.NestHiveTickMs
	}
	return next
}

// schedule keeps one live queue entry per group: the earliest. A popped entry
// whose time no longer matches the group's is stale and skipped.
/*
================
schedule
================
*/
func (state *divisionMonsterState) schedule(group spawnGroup, scheduled *int64, dueMs int64) {
	if *scheduled != 0 && *scheduled <= dueMs {
		return
	}
	*scheduled = dueMs
	state.due.pushTick(spawnTick{dueMs: dueMs, group: group})
}

/*
================
newNestRuntime
================
*/
func (s *MonsterState) newNestRuntime(index int) *nestRuntime {
	nest := s.template.Nests[index]
	n := &nestRuntime{enabled: true, respawn: nest.Respawn, remaining: nest.InstanceLimit()}
	n.intervalMs, n.reduceMs = monster.NestDelay(nest.RespawnDelayMinSec, nest.RespawnDelayMaxSec, n.ratePct, s.randomWord)
	return n
}

/*
================
materializeNest
================
*/
func (s *MonsterState) materializeNest(state *divisionMonsterState, index int, nowMs int64) {
	if _, ok := state.nests[index]; ok {
		return
	}
	n := s.newNestRuntime(index)
	state.nests[index] = n
	s.scheduleNest(state, index)
	// A hive member is visited as its hive, which materializeHive awaits.
	if s.template.Nests[index].HiveKey == "" {
		state.awaitFill(populationFillKey{nest: index}, n.scheduled, nowMs)
	}
}

/*
================
materializeHive
================
*/
func (s *MonsterState) materializeHive(state *divisionMonsterState, key string, nowMs int64) {
	if _, ok := state.hives[key]; ok {
		return
	}
	members := s.template.HiveNestIndexes(key)
	h := &hiveRuntime{selected: -1}
	state.hives[key] = h
	for _, index := range members {
		state.nests[index] = s.newNestRuntime(index)
	}
	s.scheduleHive(state, key)
	state.awaitFill(populationFillKey{hive: key}, h.scheduled, nowMs)
}

/*
================
populationFillKey

One callback unit of the boot fill: a hive, or a nest outside any hive.
================
*/
type populationFillKey struct {
	hive string
	nest int
}

/*
================
divisionMonsterState.awaitFill

A unit joins the fill only when its first visit is due within the next
native nest tick. One with nothing to place is never scheduled (0), and
one first due later (a long spawn timer: uniques wait tens of minutes)
places nothing while the world loads, so neither holds readiness.
================
*/
func (state *divisionMonsterState) awaitFill(key populationFillKey, dueMs, nowMs int64) {
	if dueMs == 0 || dueMs > nowMs+monster.NestHiveTickMs {
		return
	}
	if state.fillPending == nil {
		state.fillPending = make(map[populationFillKey]struct{})
	}
	state.fillPending[key] = struct{}{}
}

/*
================
divisionMonsterState.visitedFill

A callback visit ended. It settles its unit when it placed nothing, or
when the unit was not rescheduled (a full overwrite hive, or one with no
eligible member, is not visited again and has nothing left to fill).
================
*/
func (state *divisionMonsterState) visitedFill(key populationFillKey, spawnsBefore uint64, rescheduled bool) {
	if state.spawns == spawnsBefore || !rescheduled {
		delete(state.fillPending, key)
	}
}

/*
================
scheduleNest
================
*/
func (s *MonsterState) scheduleNest(state *divisionMonsterState, index int) {
	if key := s.template.Nests[index].HiveKey; key != "" {
		s.scheduleHive(state, key)
		return
	}
	n := state.nests[index]
	state.schedule(spawnGroup{nest: index}, &n.scheduled, n.tickMs+monster.NestHiveTickMs)
}

/*
================
scheduleHive
================
*/
func (s *MonsterState) scheduleHive(state *divisionMonsterState, key string) {
	h := state.hives[key]
	limit := s.template.Nests[s.template.HiveNestIndexAt(key, 0)].HiveMaxCount
	if limit == 0 {
		// Ordinary callbacks advance their phase even when every nest is full.
		// Skipping idle callbacks changes the first eligible tick after a death.
		state.schedule(spawnGroup{hive: key}, &h.scheduled, h.tickMs+monster.NestHiveTickMs)
		return
	}
	if limit > 0 && h.live >= limit {
		return
	}
	members := s.template.HiveNestIndexes(key)
	if h.selected >= 0 {
		members = []int{h.selected}
	}
	due, found := int64(math.MaxInt64), false
	for _, index := range members {
		n := state.nests[index]
		if limit == 0 && !n.ordinaryEligible(s.template.Nests[index].InstanceLimit()) {
			continue
		}
		if n.live >= s.template.Nests[index].InstanceLimit() {
			continue
		}
		if next := n.lastMs + int64(n.intervalMs); next < due {
			due, found = next, true
		}
	}
	if found {
		state.schedule(spawnGroup{hive: key}, &h.scheduled, nextHiveTick(h.tickMs, due))
	}
}

/*
================
runDueHiveTicks
================
*/
func (s *MonsterState) runDueHiveTicks(state *divisionMonsterState, nowMs int64) {
	for state.due.Len() > 0 && state.due[0].dueMs <= nowMs {
		tick := state.due.popTick()
		if tick.group.hive != "" {
			h := state.hives[tick.group.hive]
			if h == nil || h.scheduled != tick.dueMs {
				continue
			}
			h.scheduled = 0
			spawns := state.spawns
			if s.template.Nests[s.template.HiveNestIndexAt(tick.group.hive, 0)].HiveMaxCount == 0 {
				s.tickOrdinaryHive(state, tick.group.hive, nowMs)
			} else {
				s.tickOverwriteHive(state, tick.group.hive, nowMs)
			}
			state.visitedFill(populationFillKey{hive: tick.group.hive}, spawns, h.scheduled != 0)
			continue
		}
		n := state.nests[tick.group.nest]
		if n == nil || n.scheduled != tick.dueMs {
			continue
		}
		n.scheduled = 0
		spawns := state.spawns
		s.tickNest(state, tick.group.nest, nowMs)
		state.visitedFill(populationFillKey{nest: tick.group.nest}, spawns, n.scheduled != 0)
	}
}

// 55EA90 visits ordinary hive members in the same authored vector order as
// overwrite hives. A single callback phase owns every member's admission.
/*
================
tickOrdinaryHive
================
*/
func (s *MonsterState) tickOrdinaryHive(state *divisionMonsterState, key string, nowMs int64) {
	h := state.hives[key]
	if nowMs-h.tickMs >= monster.NestHiveTickMs {
		h.tickMs = nowMs
		for index := range s.template.HiveNestIndexSequence(key) {
			n := state.nests[index]
			if n.ordinaryEligible(s.template.Nests[index].InstanceLimit()) && n.elapsed(nowMs) {
				s.attemptNestSpawn(state, index, nowMs)
			}
		}
	}
	s.scheduleHive(state, key)
}

// tickNest is 55EA90 for a nest outside an overwrite hive.
/*
================
tickNest
================
*/
func (s *MonsterState) tickNest(state *divisionMonsterState, index int, nowMs int64) {
	n := state.nests[index]
	if nowMs-n.tickMs >= monster.NestHiveTickMs {
		n.tickMs = nowMs
		if n.ordinaryEligible(s.template.Nests[index].InstanceLimit()) && n.elapsed(nowMs) {
			s.attemptNestSpawn(state, index, nowMs)
		}
	}
	s.scheduleNest(state, index)
}

// tickOverwriteHive is 55EC10.
/*
================
tickOverwriteHive
================
*/
func (s *MonsterState) tickOverwriteHive(state *divisionMonsterState, key string, nowMs int64) {
	h := state.hives[key]
	members := s.template.HiveNestIndexes(key)
	limit := s.template.Nests[members[0]].HiveMaxCount
	// 55EC26: a full hive neither ticks nor advances its callback phase.
	if h.live < limit && nowMs-h.tickMs >= monster.NestHiveTickMs {
		h.tickMs = nowMs
		if h.selected >= 0 {
			members = []int{h.selected}
		}
		for _, index := range members {
			n := state.nests[index]
			if n.elapsed(nowMs) && n.live < s.template.Nests[index].InstanceLimit() &&
				s.attemptNestSpawn(state, index, nowMs) {
				h.live++
			}
			if h.live >= limit {
				break
			}
		}
	}
	s.scheduleHive(state, key)
}

type spawnPlacement uint8

const (
	spawnPlaced spawnPlacement = iota
	// spawnPlacementRejected: creation returned zero; retry next callback.
	spawnPlacementRejected
	// spawnPlacementClipped: the 5F7126 out flag; 560E10 halves the interval.
	spawnPlacementClipped
)

// attemptNestSpawn is one 5607B0 call for a materialized nest.
/*
================
bool
================
*/
func (s *MonsterState) attemptNestSpawn(state *divisionMonsterState, index int, nowMs int64) bool {
	n := state.nests[index]
	nest := s.template.Nests[index]
	ref := s.template.Refs[nest.RefObjID]
	roll := monster.RollNativeSpawn(ref, nest, n.partyArmed, s.randomWord)
	spawn, placement := s.placeNativeSpawn(nest, ref, roll.Grade())
	switch placement {
	case spawnPlacementClipped:
		n.intervalMs = monster.HalvedNestInterval(n.intervalMs)
		n.lastMs = nowMs
		return false
	case spawnPlacementRejected:
		return false
	}
	if s.counter >= domain.MaxMonsterGIDCounter {
		return false
	}
	s.counter++
	gid := monster.GidBase + s.counter
	instance := monster.Instance{
		Gid:          gid,
		Ref:          ref,
		Nest:         roll.Nest,
		Spawn:        spawn,
		SpawnHeading: HeadingWordFromRadians(float64(roll.HeadingRadians)),
	}
	// INFERENCE: 4C1030 draws the variant for every mob, but no other mob
	// reads it, and this server's draws do not replay the CRT rand stream;
	// only the thief and hunter draw, so ordinary spawn rolls keep their
	// sequence.
	if monster.TradeNpcMonster(ref) {
		instance.TradeVariant = uint8(s.randomWord())
	}
	instance.CurrentHP = instance.EffectiveMaxHP()
	state.instances.set(gid, instance)
	state.spawns++
	armLifetimeLocked(state, instance, nowMs)
	if instance.Rarity()&15 == 3 {
		state.uniqueNotices = append(state.uniqueNotices, uniqueNotice(5, ref.RefObjID, ""))
	}
	if state.movers == nil {
		state.movers = make(moverStorage)
	}
	mover := monster.NewSpawnMover(instance, nowMs)
	mover.Activity = monster.NewActivityCadence(uint32(nowMs), s.randomWord())
	state.movers.set(gid, mover)
	state.behavior.set(gid, 0)
	state.byRegion[spawn.RegionID] = append(state.byRegion[spawn.RegionID], gid)
	state.gidNests[gid] = index
	// 560C91..560CB3.
	n.live++
	n.lastMs = nowMs
	n.intervalMs, n.reduceMs = monster.NestDelay(nest.RespawnDelayMinSec, nest.RespawnDelayMaxSec, n.ratePct, s.randomWord)
	if roll.Grade()&0xf0 == 0x10 {
		n.partyArmed = false
	}
	// 55EB77..55EBCC: a non-respawning nest spends one of its spawns.
	if !n.respawn && n.remaining > 0 {
		n.remaining--
	}
	return true
}

// placeNativeSpawn is 5F6EB0's position stage: 531240 offsets the float32
// centre, the navmesh move test from the centre admits the candidate, and a
// blocked candidate falls back to the centre for promoted grades.
/*
================
placeNativeSpawn
================
*/
func (s *MonsterState) placeNativeSpawn(nest monster.NestRow, ref monster.MonsterRef, grade uint8) (monster.SpawnPoint, spawnPlacement) {
	centre := nest.SpawnPoint
	candidate := centre
	// The authored centre is grounded at its authored height. An admitted
	// candidate keeps the height its walk from the centre came to rest at:
	// 5F6EB0 spawns at the move test's written position, so a monster can
	// only stand where walking reaches (never a bridge parapet above a gorge
	// that nearest-height arbitration from the nest's Y would pick).
	surfaceY := nest.Y
	cx, cz := float32(nest.X), float32(nest.Z)
	x, z, moved := monster.NativeSpawnPosition(cx, cz, float32(nest.GenerateRadius), s.randomWord)
	// 98B320: a zero-length move test returns zero.
	if moved && (x != cx || z != cz) {
		candidate = normalizeGeneratedMonsterSpawn(monster.SpawnPoint{
			RefObjID: nest.RefObjID,
			RegionID: nest.RegionID,
			X:        float64(x),
			Y:        nest.Y,
			Z:        float64(z),
		})
		admitted := ClampGeneratedSpawnRegion(spawnPointFrame(centre), spawnPointFrame(candidate), s.spawnRegionAvailable)
		candidate.RegionID, candidate.X, candidate.Y, candidate.Z = admitted.RegionID, admitted.X, admitted.Y, admitted.Z
		if s.collide != nil {
			move := s.collide(spawnPointFrame(centre), spawnPointFrame(candidate))
			result := move.Result
			if result&monster.NavResultBlocked != 0 {
				if !monster.SpawnCollisionFallsBackToCentre(ref, grade) {
					return monster.SpawnPoint{}, spawnPlacementRejected
				}
				candidate = centre
			}
			if result&monster.NavResultClipped != 0 {
				return monster.SpawnPoint{}, spawnPlacementClipped
			}
			if result == 0 {
				surfaceY = move.Rest.Y
			}
		}
	}
	grounded, ok := s.resolveSpawnGround(candidate, surfaceY)
	if !ok {
		return monster.SpawnPoint{}, spawnPlacementRejected
	}
	return grounded, spawnPlaced
}

/*
================
spawnPointFrame
================
*/
func spawnPointFrame(point monster.SpawnPoint) Spawn {
	return Spawn{RegionID: point.RegionID, X: point.X, Y: point.Y, Z: point.Z}
}

// Defeat removes one live monster and runs the native nest death path
// (560D00). A later spawn receives a new gid and a freshly generated
// position, matching entity lifecycle rather than resurrecting a stale wire
// identity. Non-population/dev gids are not accepted.
/*
================
bool
================
*/
func (s *MonsterState) Defeat(divisionID string, gid uint32, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	index, attached := state.gidNests[gid]
	instance, exists := state.instances.lookup(gid)
	if !exists || !attached && !instance.NestDetached && instance.SummonerGID == 0 {
		return false
	}
	if instance.Rarity()&15 == 3 && !state.uniqueDeaths[gid] {
		state.uniqueNotices = append(state.uniqueNotices, uniqueNotice(6, instance.Ref.RefObjID, "???"))
	}
	delete(state.uniqueDeaths, gid)
	delete(state.lifetimes, gid)
	state.instances.remove(gid)
	state.forgetDormant(gid)
	delete(state.contributions, gid)
	state.releaseApproachActor(gid)
	delete(state.movers, gid)
	state.behavior.remove(gid)
	delete(state.aiTimers, gid)
	delete(state.gidNests, gid)
	removeRegionGid(state.byRegion, instance.Spawn.RegionID, gid)
	if attached {
		s.nestDeath(state, index, at.UnixMilli())
	}
	return true
}

// nestDeath is 560D00.
/*
================
nestDeath
================
*/
func (s *MonsterState) nestDeath(state *divisionMonsterState, index int, nowMs int64) {
	n := state.nests[index]
	nest := s.template.Nests[index]
	if n.live == nest.InstanceLimit() {
		n.lastMs = nowMs
	}
	if nest.HiveMaxCount > 0 {
		s.overwriteHiveDeath(state, nest.HiveKey, nowMs)
	}
	if n.live > 0 {
		n.live--
	}
	if nest.HiveMaxCount > 0 {
		s.scheduleHive(state, nest.HiveKey)
	} else {
		s.scheduleNest(state, index)
	}
}

// overwriteHiveDeath is 55EEC0: a hive losing a member while full draws the
// next location (single-occupant hives only) and restarts every member.
/*
================
overwriteHiveDeath
================
*/
func (s *MonsterState) overwriteHiveDeath(state *divisionMonsterState, key string, nowMs int64) {
	h := state.hives[key]
	members := s.template.HiveNestIndexes(key)
	limit := s.template.Nests[members[0]].HiveMaxCount
	if h.live == limit {
		selected := monster.HiveRespawnSelection(len(members), s.randomWord)
		for i, index := range members {
			n := state.nests[index]
			nest := s.template.Nests[index]
			if i == selected && limit == 1 {
				h.selected = index
				// 55EF7A: rolled into the selected nest, then re-rolled by 560E50.
				n.intervalMs, n.reduceMs = monster.NestDelay(nest.RespawnDelayMinSec, nest.RespawnDelayMaxSec, n.ratePct, s.randomWord)
			}
			n.lastMs = nowMs
			n.intervalMs, n.reduceMs = monster.NestDelay(nest.RespawnDelayMinSec, nest.RespawnDelayMaxSec, n.ratePct, s.randomWord)
		}
	}
	if h.live > 0 {
		h.live--
	}
}

// pendingRefills counts materialized groups waiting for a hive callback.
/*
================
int
================
*/
func (state *divisionMonsterState) pendingRefills() int {
	pending := 0
	for _, n := range state.nests {
		if n.scheduled != 0 {
			pending++
		}
	}
	for _, h := range state.hives {
		if h.scheduled != 0 {
			pending++
		}
	}
	return pending
}

/*
================
removeRegionGid
================
*/
func removeRegionGid(byRegion map[uint16][]uint32, regionID uint16, gid uint32) {
	gids := byRegion[regionID]
	for index, candidate := range gids {
		if candidate != gid {
			continue
		}
		gids[index] = gids[len(gids)-1]
		gids = gids[:len(gids)-1]
		if len(gids) == 0 {
			delete(byRegion, regionID)
		} else {
			byRegion[regionID] = gids
		}
		return
	}
}
