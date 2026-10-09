/*
===========================================================================

monster_dormancy.go - sleeping populations in empty regions

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"time"
)

const regionSleepGraceMs int64 = 30000

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
/*
================
prepareDormancy
================
*/
func (s *MonsterState) prepareDormancy(now int64, division string, sessions []SessionSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.regionDormancy {
		return
	}
	for _, key := range s.populationKeys() {
		if division != "" && key.division != division {
			continue
		}
		state := s.populationForLease(key.division, key.lease)
		if state == nil {
			continue
		}
		if state.keepAwakeUntil == nil {
			state.keepAwakeUntil = make(map[uint16]int64)
		}
		for _, session := range sessions {
			if session.DivisionID != key.division || session.Population != key.lease {
				continue
			}
			for _, region := range RegionScopeRing(session.World.LiveSpawnAt(now).RegionID) {
				state.keepAwakeUntil[region] = now + regionSleepGraceMs
				for gid := range state.dormantRegions[region] {
					state.forgetDormant(gid)
					state.instances.wake(gid)
					if state.instances.contains(gid) {
						state.behavior.set(gid, now)
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
}

/*
================
tryDormant
================
*/
func (s *MonsterState) tryDormant(state *divisionMonsterState, gid uint32, m monster.MoverState, now int64) bool {
	if !s.regionDormancy || state.keepAwakeUntil == nil {
		return false
	}
	if m.Mode() != monster.MoverPending || IsDungeonRegion(m.Pose.RegionID) {
		return false
	}
	if now < state.keepAwakeUntil[m.Pose.RegionID] {
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
		state.dormant = make(map[uint32]uint16)
		state.dormantRegions = make(map[uint16]map[uint32]struct{})
	}
	region := m.Pose.RegionID
	if state.dormantRegions[region] == nil {
		state.dormantRegions[region] = make(map[uint32]struct{})
	}
	state.dormant[gid] = region
	state.dormantRegions[region][gid] = struct{}{}
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
================
*/
func (state *divisionMonsterState) forgetDormant(gid uint32) {
	state.aiTimer(gid)
	if region, ok := state.dormant[gid]; ok {
		delete(state.dormant, gid)
		delete(state.dormantRegions[region], gid)
		if len(state.dormantRegions[region]) == 0 {
			delete(state.dormantRegions, region)
		}
	}
}
