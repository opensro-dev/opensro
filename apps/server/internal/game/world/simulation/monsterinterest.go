/*
===========================================================================

monsterinterest.go - which monsters each session sees

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/domain"
	"slices"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
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

/*
================
scopeViewerMemo

One viewer's last full scope pass (#2134): the population, block and
interest stamp its shown set synced under. A later leg that matches
division, lease, block and stamp provably faces the in-scope set the
viewer already holds, so runScopeVisibility skips the delta read,
the list clone and both set scans for it. The stamp is sampled
before the pass it describes: a write landing between the sample
and the pass can only make the memo stale, never wrong-side.
================
*/
type scopeViewerMemo struct {
	division string
	lease    instance.Lease
	block    worldgeom.InterestBlock
	stamp    interestMemoStamp
}

/*
================
scopeViewerState

One session's scope record: the monster gids it has been sent
spawns for, and the memo of the pass its shown set last synced
under. The record is born at the seeding store, with the shown
set, so memoOK implies a shown set exists - the purge and the
unchanged-leg skip both rely on that pairing.
================
*/
type scopeViewerState struct {
	shown  map[uint32]bool
	memo   scopeViewerMemo
	memoOK bool
}

/*
================
scopeViewerSlot

One viewer's dense leg slot (#2994, the per-leg snapshot shape of
#2984): everything the leg reads for the viewer, gathered by index
once per leg - the stamp sampled at leg start, the block and pose
it keys on, and the viewer's scope record. The leg used to re-key
every session in every phase instead. The resolve phase's delta
answer rides at the end for the publish phase. shown aliases the
record's set; state is nil until the session seeds, which is also
what seeded means.
================
*/
type scopeViewerSlot struct {
	stamp   interestMemoStamp
	block   worldgeom.InterestBlock
	viewer  worldgeom.RegionXZ
	shown   map[uint32]bool
	state   *scopeViewerState
	stampOK bool

	sameScope bool
	entered   []uint32
	snapshots map[uint32]monster.Instance
	left      []uint32
	total     int
	bridged   bool
}

/*
================
MonsterMoverOps.scopeShown

The monster gids session has been sent spawns for, nil when the
session holds no scope record.
================
*/
func (ops *MonsterMoverOps) scopeShown(sessionID string) map[uint32]bool {
	if state := ops.scopeViewers[sessionID]; state != nil {
		return state.shown
	}
	return nil
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
The stamp keys on the viewer's own blocks, so churn in a far
neighbourhood neither re-reads its shown set nor re-scans its answer
(#2134); a leg the per-block memo cannot bridge with a recorded
difference runs the full pass below.
==================
*/
func (ops *MonsterMoverOps) runScopeVisibility(nowMs int64, sessions []SessionSnapshot, viewers map[string]worldgeom.RegionXZ, live map[string]bool, push Pusher) {
	if ops.scopeViewers == nil {
		ops.scopeViewers = make(map[string]*scopeViewerState)
	}
	if cap(ops.scopeSlots) < len(sessions) {
		ops.scopeSlots = make([]scopeViewerSlot, len(sessions))
	}
	slots := ops.scopeSlots[:len(sessions)]

	// Gather each viewer's leg inputs into its dense slot by index
	// (#2994): the leg used to re-key every session in every phase
	// - stamp sampling, the skip read, the shown read, the purge -
	// and range a by-value copy of every snapshot to do it.
	for i := range sessions {
		slot := &slots[i]
		*slot = scopeViewerSlot{}
		slot.viewer = viewers[sessions[i].SessionID]
		slot.block = worldgeom.InterestBlockAt(slot.viewer)
		if state := ops.scopeViewers[sessions[i].SessionID]; state != nil {
			slot.state = state
			slot.shown = state.shown
		}
	}
	ops.Monsters.fillScopeStamps(slots, sessions, nowMs)
	for i := range sessions {
		slot := &slots[i]
		if slot.state == nil || !slot.state.memoOK {
			continue
		}
		memo := slot.state.memo
		slot.sameScope = memo.division == sessions[i].DivisionID && memo.lease == sessions[i].Population &&
			slot.block == memo.block
	}
	ops.Monsters.resolveScopeDeltas(sessions, slots, nowMs)

	var inScopeGids map[uint32]bool
	for i := range sessions {
		session := &sessions[i]
		slot := &slots[i]
		stamp, stampOK := slot.stamp, slot.stampOK

		// The unchanged-leg skip (#2134): a seeded viewer whose block
		// and interest stamp match its last pass faces the in-scope
		// set its shown set already equals, so the delta below would
		// publish nothing. The budget-breach size is the one already
		// logged for this division occupancy, so it is not re-read
		// either. sameScope is only set for a memo-holding viewer,
		// and a memo exists only beside a seeded shown set, so the
		// skip needs no shown read (#2988).
		if stampOK && slot.sameScope && stamp == slot.state.memo.stamp {
			continue
		}
		shown := slot.shown

		// enterOne publishes one scope-enter: one 0x30D7 single at the
		// monster's LIVE pose (a wanderer spawns where it is, not at
		// its anchor). If a segment is IN FLIGHT, follow with the
		// channel push (the spawn row ships the walk channel) and the
		// CURRENT goal, so the new viewer's client integrates the same
		// walk the rest of the division sees (without this the monster
		// would stand frozen until its next leg). A gid whose mover
		// row is gone is not marked shown; the actor is gone too,
		// which moves the stamp, so a later leg re-reads in full.
		enterOne := func(instance monster.Instance) {
			mover, ok := ops.Monsters.Mover(session.DivisionID, instance.Gid)
			if !ok {
				return
			}
			// Mid-flight scope-enter: resolve the interpolated XZ onto the
			// terrain (BUG-8 second injection path, board seq735/seq779) so
			// the spawn single never ships a chord height.
			pose := mover.LivePoseAt(nowMs, ops.GroundHeight)
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

		// emitExits publishes scope-exit despawns: one 0x36AB for a
		// single exit, the byListSub=2 bracket for a bulk exit (a
		// block shift evicts whole blocks at once; both shapes are
		// native and both are client-safe post WIP seq373). Callers
		// remove the gids from shown first.
		emitExits := func(exits []uint32) {
			if len(exits) == 1 {
				despawn := wire.ObjectDespawn{Gid: exits[0]}
				push.PushToSession(session.SessionID, []Frame{{
					ScopeGID: exits[0],
					Opcode:   wire.OpObjectDespawn,
					Payload:  despawn.Encode(),
				}})
			} else if len(exits) > 1 {
				slices.Sort(exits)
				push.PushToSession(session.SessionID, MonsterDespawnBracketFrames(exits))
			}
		}

		// The block-delta fast path (#2134): the stamp moved, but only
		// in blocks some other neighbourhood reads - or this block's
		// answer changed by a difference the per-block memo recorded.
		// A viewer whose shown set still equals the replaced answer
		// applies just that difference, resolved into the slot under
		// one lock hold before this loop (#2994). Anything the memo
		// cannot bridge falls through to the full pass, which is
		// always correct.
		if slot.state != nil && stampOK && slot.sameScope && slot.bridged {
			// Detection only: never truncate the server-owned existence set.
			if slot.total > monsterScopeSoftLimit {
				ops.logScopeBudgetBreach(session.DivisionID, slot.total)
			}
			for _, gid := range slot.entered {
				enterOne(slot.snapshots[gid])
			}
			for _, gid := range slot.left {
				delete(shown, gid)
			}
			emitExits(slot.left)
			slot.state.memo = scopeViewerMemo{
				division: session.DivisionID, lease: session.Population,
				block: slot.block, stamp: stamp,
			}
			continue
		}

		inScope, snapshots := ops.Monsters.populationInterestDeltaSampled(session.DivisionID, session.Population, slot.viewer, slot.block, slot.stamp, shown)
		// Detection only: never truncate the server-owned existence set.
		if len(inScope) > monsterScopeSoftLimit {
			ops.logScopeBudgetBreach(session.DivisionID, len(inScope))
		}

		if slot.state == nil {
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
				livePose := mover.LivePoseAt(nowMs, ops.GroundHeight)
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
			state := &scopeViewerState{shown: shown}
			ops.scopeViewers[session.SessionID] = state
			slot.state, slot.shown = state, shown
		}

		if inScopeGids == nil {
			inScopeGids = make(map[uint32]bool, len(inScope))
		}
		clear(inScopeGids)
		for _, gid := range inScope {
			inScopeGids[gid] = true
			if shown[gid] {
				continue
			}
			enterOne(snapshots[gid])
		}

		// Scope-exit: every shown gid the answer no longer holds.
		var exits []uint32
		for gid := range shown {
			if !inScopeGids[gid] {
				exits = append(exits, gid)
				delete(shown, gid)
			}
		}
		emitExits(exits)

		// Remember the pass for the unchanged-leg skip: the stamp
		// sampled before it ran and the block it read.
		if stampOK {
			slot.state.memo = scopeViewerMemo{
				division: session.DivisionID, lease: session.Population,
				block: slot.block, stamp: stamp,
			}
			slot.state.memoOK = true
		} else {
			slot.state.memoOK = false
		}
	}

	// Departed viewers must not leak show bookkeeping (the settled-map rule).
	// The loop above leaves a record for every session it served,
	// so when the counts agree every record belongs to a served
	// session and none can be departed; only a surplus sweeps.
	if len(ops.scopeViewers) > len(sessions) {
		for sessionID := range ops.scopeViewers {
			if !live[sessionID] {
				delete(ops.scopeViewers, sessionID)
			}
		}
	}

	// Divisions without a live viewer must not leak breach bookkeeping
	// either: breachLogged is division-keyed, so without this it grows for
	// every division ever seen. Clearing on empty re-arms the once-per-
	// division breach log for the division's next occupancy, which is the
	// A19-b detection intent. Tick-goroutine-only state, like shownMonsters.
	if len(ops.breachLogged) > 0 {
		liveDivisions := make(map[string]bool, len(sessions))
		for i := range sessions {
			liveDivisions[sessions[i].DivisionID] = true
		}
		for divisionID := range ops.breachLogged {
			if !liveDivisions[divisionID] {
				delete(ops.breachLogged, divisionID)
			}
		}
	}
}
