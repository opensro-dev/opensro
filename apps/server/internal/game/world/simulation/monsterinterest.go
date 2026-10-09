/*
===========================================================================

monsterinterest.go - which monsters each session sees

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/domain"
	"sort"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// Monster visibility uses the native message-block interest area
// (worldgeom.InterestVisible): the viewer's 320-unit block and its eight
// neighbours, measured at live positions. RegionScopeRing stays the
// nest/materialization index. Live mover bounds supply visibility candidates
// independently, including actors travelling far from their spawn regions.

type monsterObjectListKey struct {
	divisionID string
	playerGid  uint32
}

// InterestInstances returns the instances whose live position lies inside the
// native interest area of a viewer at viewer, ordered by gid.
func (s *MonsterState) InterestInstances(divisionID string, viewer worldgeom.RegionXZ, nowMs int64) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.divs[divisionID]
	if state == nil {
		return nil
	}
	gids := populationInterestGIDs(state, viewer, nowMs)
	visible := make([]monster.Instance, 0, len(gids))
	for _, gid := range gids {
		visible = append(visible, state.instances.get(gid))
	}
	return visible
}

// CurrentInterestInstances is InterestInstances at the registry clock, for
// the bootstrap object list.
func (s *MonsterState) CurrentInterestInstances(divisionID string, viewer worldgeom.RegionXZ) []monster.Instance {
	s.mu.Lock()
	nowMs := s.nowMillis()
	s.mu.Unlock()
	return s.InterestInstances(divisionID, viewer, nowMs)
}

/*
==================
RecordObjectList

RecordObjectList remembers the monster gids a player's bootstrap object
list created. A session reaches the tick only after 0x3012 game-ready, so
monsters can cross block boundaries while the client loads; the first scope
tick reconciles against this record instead of assuming the sets still
agree. A later bootstrap for the same player replaces the record.
==================
*/
func (s *MonsterState) RecordObjectList(divisionID string, playerGid uint32, gids []uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.objectLists == nil {
		s.objectLists = make(map[monsterObjectListKey][]uint32)
	}
	s.objectLists[monsterObjectListKey{divisionID, playerGid}] = append([]uint32(nil), gids...)
}

// TakeObjectList returns and forgets the player's recorded object list.
func (s *MonsterState) TakeObjectList(divisionID string, playerGid uint32) ([]uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := monsterObjectListKey{divisionID, playerGid}
	gids, ok := s.objectLists[key]
	delete(s.objectLists, key)
	return gids, ok
}

/*
==================
runScopeVisibility

runScopeVisibility is the per-viewer scoped spawn/despawn stream: the
native model is server-owned deltas (the client NEVER self-culls -
triangulated REV/DUMP/RZ), so every scope-exit MUST emit an explicit
despawn. A stationary viewer's interest blocks are constant, so after the
initial seed only monsters crossing a block boundary produce deltas
(coordinator seq401 condition 3 - flicker without movement is a bug).
==================
*/
func (ops *MonsterMoverOps) runScopeVisibility(nowMs int64, sessions []SessionSnapshot, viewers map[string]worldgeom.RegionXZ, live map[string]bool, push Pusher) {
	if ops.shownMonsters == nil {
		ops.shownMonsters = make(map[string]map[uint32]bool)
	}

	inScopeGids := make(map[uint32]bool)
	for _, session := range sessions {
		inScope, snapshots := ops.Monsters.populationInterestDelta(session.DivisionID, session.Population, viewers[session.SessionID], nowMs, ops.shownMonsters[session.SessionID])
		// Detection only: never truncate the server-owned existence set.
		if len(inScope) > monsterScopeSoftLimit {
			ops.logScopeBudgetBreach(session.DivisionID, len(inScope))
		}

		shown, seeded := ops.shownMonsters[session.SessionID]
		if !seeded {
			// First sight of this session: the client already holds the
			// bootstrap object list, recorded when it was built. The session
			// reaches the tick only after 0x3012 game-ready, so that list may
			// differ from the current interest set; seed from the record and
			// let the diff below create and remove the difference. Sessions
			// without a record have no proven creates; publish their scope
			// before sending any movement or abnormal-state continuations.
			created, recorded := ops.Monsters.TakeObjectList(session.DivisionID, PlayerObjectID(session.CharacterID))
			if session.PublishedObjects != nil {
				created = nil
				for _, gid := range session.PublishedObjects {
					if gid > domain.MonsterGIDBase && gid <= domain.MonsterGIDLimit {
						created = append(created, gid)
					}
				}
				recorded = true
			}
			shown = make(map[uint32]bool, len(inScope))
			if recorded {
				for _, gid := range created {
					shown[gid] = true
				}
			}
			// Reconcile the other half of that snapshot here: an in-flight
			// mover needs its current channel + goal AFTER the create bracket.
			// Assuming the static spawn row was sufficient lost the first B738
			// whenever a mover survived a browser/session replacement; the
			// later B2F5 then corrected only PathCtl's logical cursor while the
			// rendered owner remained at its anchor.
			for _, gid := range inScope {
				instance := snapshots[gid]
				if !shown[instance.Gid] {
					continue
				}
				if instance.AbnormalMask() != 0 {
					push.PushToSession(session.SessionID, []Frame{{Opcode: OpVitalsUpdate, Payload: MonsterAbnormalPayload(instance)}})
				}
				mover, ok := ops.Monsters.Mover(session.DivisionID, instance.Gid)
				if !ok || instance.CurrentHP == 0 || !mover.InFlight(nowMs) {
					continue
				}
				livePose := mover.LivePoseAt(nowMs, ops.TerrainHeight)
				source := &MovementSource{
					RegionID: livePose.RegionID,
					X:        livePose.X,
					Y:        livePose.Y,
					Z:        livePose.Z,
				}
				push.PushToSession(
					session.SessionID,
					monsterInFlightSnapshotFrames(instance, mover, source),
				)
			}
			ops.shownMonsters[session.SessionID] = shown
		}

		clear(inScopeGids)
		for _, gid := range inScope {
			inScopeGids[gid] = true
			if shown[gid] {
				continue
			}
			instance := snapshots[gid]
			// Scope-enter: one 0x30D7 single at the monster's LIVE pose
			// (a wanderer spawns where it is, not at its anchor). If a
			// segment is IN FLIGHT, follow with the channel push (the
			// spawn row ships the walk channel) and the CURRENT goal, so
			// the new viewer's client integrates the same walk the rest
			// of the division sees (without this the monster would stand
			// frozen until its next leg).
			mover, ok := ops.Monsters.Mover(session.DivisionID, instance.Gid)
			if !ok {
				continue
			}
			// Mid-flight scope-enter: resolve the interpolated XZ onto the
			// terrain (BUG-8 second injection path, board seq735/seq779) so
			// the spawn single never ships a chord height.
			pose := mover.LivePoseAt(nowMs, ops.TerrainHeight)
			frames := []Frame{{
				ScopeGID: instance.Gid, ScopeVisible: true,
				Opcode: wire.OpSingleObjectSpawn,
				Payload: BuildMonsterSpawnSingle(monsterWireDefFromInstance(instance, nowMs), instance.Gid, Spawn{
					RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z, Angle: pose.Heading,
				}),
			}}
			if instance.AbnormalMask() != 0 {
				frames = append(frames, Frame{Opcode: OpVitalsUpdate, Payload: MonsterAbnormalPayload(instance)})
			}
			if instance.CurrentHP != 0 && mover.InFlight(nowMs) {
				frames = append(frames, monsterInFlightSnapshotFrames(instance, mover, nil)...)
			}
			push.PushToSession(session.SessionID, frames)
			shown[instance.Gid] = true
		}

		// Scope-exit: explicit despawns - one 0x36AB for a single exit,
		// the byListSub=2 bracket for a bulk exit (a block shift evicts
		// whole blocks at once; both shapes are native and both are
		// client-safe post WIP seq373).
		var exits []uint32
		for gid := range shown {
			if !inScopeGids[gid] {
				exits = append(exits, gid)
				delete(shown, gid)
			}
		}
		if len(exits) == 1 {
			despawn := wire.ObjectDespawn{Gid: exits[0]}
			push.PushToSession(session.SessionID, []Frame{{
				ScopeGID: exits[0],
				Opcode:   wire.OpObjectDespawn,
				Payload:  despawn.Encode(),
			}})
		} else if len(exits) > 1 {
			sort.Slice(exits, func(i, j int) bool { return exits[i] < exits[j] })
			push.PushToSession(session.SessionID, MonsterDespawnBracketFrames(exits))
		}
	}

	// Departed viewers must not leak show bookkeeping (the settled-map rule).
	for sessionID := range ops.shownMonsters {
		if !live[sessionID] {
			delete(ops.shownMonsters, sessionID)
		}
	}

	// Divisions without a live viewer must not leak breach bookkeeping
	// either: breachLogged is division-keyed, so without this it grows for
	// every division ever seen. Clearing on empty re-arms the once-per-
	// division breach log for the division's next occupancy, which is the
	// A19-b detection intent. Tick-goroutine-only state, like shownMonsters.
	if len(ops.breachLogged) > 0 {
		liveDivisions := make(map[string]bool, len(sessions))
		for _, session := range sessions {
			liveDivisions[session.DivisionID] = true
		}
		for divisionID := range ops.breachLogged {
			if !liveDivisions[divisionID] {
				delete(ops.breachLogged, divisionID)
			}
		}
	}
}
