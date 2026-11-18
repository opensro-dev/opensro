/*
===========================================================================

monster_dormancy.go - sleeping populations where no player is near

Port-only, not native: a settled, unhurt monster no player is near leaves
the behaviour queue until one comes close. Nothing a player can see
changes, since only monsters outside every viewer's reach sleep.

The unit of sleep is a sleepCell. Outdoors it is the region, woken by the
3x3 sector ring around each player. A cave floor is one region id for the
whole dungeon, so there the unit is the native 320-unit message block
(world/dungeon_grid.go), woken CaveWakeBlocks around each player: a player
entering Ch'in Tomb Floor 3 wakes the blocks near them, not the floor's
6,866 monsters in one tick. A cave without an installed grid, or a point
outside it, never sleeps.

===========================================================================
*/

package simulation

import (
	"time"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

const regionSleepGraceMs int64 = 30000

// CaveWakeBlocks is how many message blocks past a player's own block a
// cave stays awake on each axis (a 7x7 block square). Viewers see the 3x3
// neighbourhood (one block out); the two further blocks, at least 640 units,
// exceed the largest cave sight range in the shipped nest data (400 plus a
// body radius), so no monster that could see or be seen sleeps.
const CaveWakeBlocks = 3

/*
================
sleepCell

The unit monsters sleep and wake by: a region outdoors (x and z zero), a
message block in a cave. whole marks a cave floor's region-wide key, kept
awake while a player stands on the floor outside its grid.
================
*/
type sleepCell struct {
	region uint16
	whole  bool
	x, z   int32
}

/*
================
sleepCellAt

The cell of a point, false for a cave point that falls on no message
block (no grid installed, or outside its bounds): such a monster stays
awake, the behaviour before caves slept.
================
*/
func sleepCellAt(regionID uint16, x, z float64) (sleepCell, bool) {
	if !FeaturesAt(regionID).DormancyByBlock {
		return sleepCell{region: regionID}, true
	}
	block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: regionID, X: x, Z: z})
	if block.Whole {
		return sleepCell{}, false
	}
	return sleepCell{region: regionID, x: int32(block.X), z: int32(block.Z)}, true
}

/*
================
divisionMonsterState.wakeCells

Appends the cells one player at spawn keeps awake: the sector ring
outdoors, the block square in a cave. A player on a cave floor but off its
grid wakes the floor's region-wide key and every cell of the floor that
sleeps, which is how a cave behaved when it was one block.
================
*/
func (state *divisionMonsterState) wakeCells(spawn Spawn, out []sleepCell) []sleepCell {
	if !FeaturesAt(spawn.RegionID).DormancyByBlock {
		for _, region := range RegionScopeRing(spawn.RegionID) {
			out = append(out, sleepCell{region: region})
		}
		return out
	}
	center, ok := sleepCellAt(spawn.RegionID, spawn.X, spawn.Z)
	if !ok {
		out = append(out, sleepCell{region: spawn.RegionID, whole: true})
		for cell := range state.dormantCells {
			if cell.region == spawn.RegionID {
				out = append(out, cell)
			}
		}
		return out
	}
	for dz := int32(-CaveWakeBlocks); dz <= CaveWakeBlocks; dz++ {
		for dx := int32(-CaveWakeBlocks); dx <= CaveWakeBlocks; dx++ {
			out = append(out, sleepCell{region: spawn.RegionID, x: center.x + dx, z: center.z + dz})
		}
	}
	return out
}

// EnableRegionDormancy must be installed before tickers start. Nest/unique
// timers remain global; only safe settled PENDING actors leave the due queue.
/*
================
EnableRegionDormancy
================
*/
func (s *MonsterState) EnableRegionDormancy() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.regionDormancy = true
}

// prepareDormancy runs before visibility publication. A full sector ring wakes
// before the much smaller message-block visibility/aggro boundary is reached.
// It returns the gids it woke, keyed by population (nil when it woke
// nobody); the leg counts them and tracks their first dispatch (#1820
// part C).
/*
================
prepareDormancy
================
*/
func (s *MonsterState) prepareDormancy(now int64, division string, sessions []SessionSnapshot) map[populationKey]map[uint32]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.regionDormancy {
		return nil
	}
	var woken map[populationKey]map[uint32]struct{}
	for _, key := range s.populationKeys() {
		if division != "" && key.division != division {
			continue
		}
		state := s.populationForLease(key.division, key.lease)
		if state == nil {
			continue
		}
		if state.keepAwakeUntil == nil {
			state.keepAwakeUntil = make(map[sleepCell]int64)
		}
		var cellBuffer [(2*CaveWakeBlocks + 1) * (2*CaveWakeBlocks + 1)]sleepCell
		for _, session := range sessions {
			if session.DivisionID != key.division || session.Population != key.lease {
				continue
			}
			for _, cell := range state.wakeCells(session.World.LiveSpawnAt(now), cellBuffer[:0]) {
				state.keepAwakeUntil[cell] = now + regionSleepGraceMs
				for gid := range state.dormantCells[cell] {
					state.forgetDormant(gid)
					state.instances.wake(gid)
					if state.instances.contains(gid) {
						// Phase-continuous wake (#1820 B part A): the
						// cadence is caught up to the phase it would
						// hold had it never slept, then the scheduler
						// lands the next due time on that phase - the
						// ring no longer resumes on one tick. (tryDormant
						// cannot re-sleep it here: keepAwakeUntil was
						// just set above.)
						state.movers.catchUpActivity(gid, uint32(now))
						s.scheduleBehavior(state, gid, now)
						if woken == nil {
							woken = make(map[populationKey]map[uint32]struct{})
						}
						set := woken[key]
						if set == nil {
							set = make(map[uint32]struct{})
							woken[key] = set
						}
						set[gid] = struct{}{}
					}
				}
			}
		}
		// Bound serialization/disk work under the authority lock. Actor state
		// remains hot until its complete record has been written successfully.
		deadline := time.Now().Add(2 * time.Millisecond)
		for n := 0; n < 128 && len(state.archiveQueue) > 0 && time.Now().Before(deadline); n++ {
			gid := state.archiveQueue[0]
			state.archiveQueue = state.archiveQueue[1:]
			delete(state.archiveQueued, gid)
			if _, sleeping := state.dormant[gid]; sleeping {
				state.instances.freeze(gid)
			}
		}
		if len(state.archiveQueue) == 0 {
			state.archiveQueue = nil
		}
		state.compactDormantIndexes(deadline)
	}
	return woken
}

/*
================
tryDormant
================
*/
func (s *MonsterState) tryDormant(state *divisionMonsterState, gid uint32, m moverSchedule, now int64) bool {
	if !s.regionDormancy || state.keepAwakeUntil == nil {
		return false
	}
	if m.mode != monster.MoverPending || !FeaturesAt(m.regionID).Dormancy {
		return false
	}
	cell, ok := state.sleepCellOf(gid, m.regionID)
	if !ok || now < state.keepAwakeUntil[cell] {
		return false
	}
	if FeaturesAt(cell.region).DormancyByBlock && now < state.keepAwakeUntil[sleepCell{region: cell.region, whole: true}] {
		return false
	}
	if !state.movers.compact(gid) {
		return false
	}
	actor, ok := state.instances.lookup(gid)
	if !ok || actor.Rarity()&15 == 3 || actor.SummonerGID != 0 || actor.NestDetached || !actor.Nest.Respawn || actor.Nest.NativeTacticsFlags&0x84 != 0 {
		return false
	}
	unhurt := actor.CurrentHP == actor.EffectiveMaxHP() && !actor.Help.HasPending() && actor.Abnormal == nil
	still := actor.Motion == (monster.MotionHold{}) && actor.Opponents == ([2]monster.Opponent{}) && actor.SummonActionUntilMs == 0
	if !unhurt || !still {
		return false
	}
	if len(state.contributions[gid]) != 0 {
		return false
	}
	if _, ok := state.pendingSummons[gid]; ok {
		return false
	}
	if state.dormant == nil {
		state.dormant = make(map[uint32]sleepCell)
		state.dormantCells = make(map[sleepCell]map[uint32]struct{})
	}
	if state.dormantCells[cell] == nil {
		state.dormantCells[cell] = make(map[uint32]struct{})
	}
	state.dormant[gid] = cell
	state.dormantCells[cell][gid] = struct{}{}
	if state.instances.archive != nil {
		if state.archiveQueued == nil {
			state.archiveQueued = make(map[uint32]struct{})
		}
		if _, queued := state.archiveQueued[gid]; !queued {
			state.archiveQueue = append(state.archiveQueue, gid)
			state.archiveQueued[gid] = struct{}{}
		}
	}
	state.behavior.remove(gid)
	state.storeAITimer(gid)
	return true
}

/*
================
forgetDormant

The bank restore has work to do only for an actor that actually
slept: tryDormant registers the gid in dormant before it stores the
bank away (storeAITimer), and this function is the only place that
unregisters one, so a stored bank exists exactly while its gid is
registered here. The schedulers call this for every due actor, and
the awake majority used to pay the bank's two gid-map probes for a
restore that could not fire (#2029: aiTimer was most of this
function's cost in the seeded leg profile). Probe dormant first;
the asleep path below is unchanged, restore before unregister.
================
*/
func (state *divisionMonsterState) forgetDormant(gid uint32) {
	cell, ok := state.dormant[gid]
	if !ok {
		return
	}
	state.aiTimer(gid)
	delete(state.dormant, gid)
	delete(state.dormantCells[cell], gid)
	if len(state.dormantCells[cell]) == 0 {
		delete(state.dormantCells, cell)
	}
}

/*
================
divisionMonsterState.sleepCellOf

The cell a pending actor would sleep in. Outdoors the schedule's region is
the whole answer; a cave reads the pending pose (moverStorage.pendingPose),
so the per-actor schedule read stays as small as it was (#3027).
================
*/
func (state *divisionMonsterState) sleepCellOf(gid uint32, regionID uint16) (sleepCell, bool) {
	if !FeaturesAt(regionID).DormancyByBlock {
		return sleepCell{region: regionID}, true
	}
	pose, ok := state.movers.pendingPose(gid)
	if !ok {
		return sleepCell{}, false
	}
	return sleepCellAt(pose.RegionID, pose.X, pose.Z)
}
