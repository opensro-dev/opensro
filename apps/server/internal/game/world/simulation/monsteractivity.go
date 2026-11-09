/*
===========================================================================

monsteractivity.go - which monsters are active near players

===========================================================================
*/

package simulation

import (
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

type activityWorld struct {
	division   string
	world      uint32
	generation uint64
}
type monsterActivitySnapshot struct {
	// counts is the census kept by world, then block: the gate
	// asks one world at a time (the world rotates per batch), so
	// its probe hashes the small block key alone, never the whole
	// bucket with its division string (#3027).
	counts     map[activityWorld]map[worldgeom.MessageBlock]uint16
	unresolved map[activityWorld]bool
	world      activityWorld
	// bound is the asked world's count map, bound on rotation
	// (countedHere): per batch, not per due actor.
	bound      map[worldgeom.MessageBlock]uint16
	boundWorld activityWorld
	boundSet   bool
}

func (ops *MonsterMoverOps) activityBlock(p Spawn) (worldgeom.MessageBlock, bool) {
	if ops.MessageBlockAt != nil {
		return ops.MessageBlockAt(p)
	}
	// Geometry-free outdoor fixtures still use the native outdoor grid.
	// Indoor queries require the resident navigation owner.
	return worldgeom.OutdoorMessageBlock(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
}

func (ops *MonsterMoverOps) captureActivity(sessions []SessionSnapshot, now int64) *monsterActivitySnapshot {
	a := &monsterActivitySnapshot{counts: make(map[activityWorld]map[worldgeom.MessageBlock]uint16), unresolved: make(map[activityWorld]bool)}
	type identity struct {
		owner activityWorld
		gid   uint32
	}
	seen := make(map[identity]bool)
	for _, s := range sessions {
		if s.Population.ID != instance.ID(s.WorldInstance) || s.Population.Generation == 0 {
			continue
		}
		owner := activityWorld{s.DivisionID, s.WorldInstance, s.Population.Generation}
		id := identity{owner, PlayerObjectID(s.CharacterID)}
		if seen[id] {
			continue
		}
		seen[id] = true
		b, ok := ops.activityBlock(s.World.LiveSpawnAt(now))
		if !ok {
			a.unresolved[owner] = true
			continue
		}
		// The retail server's spawn broadcast counts every admitted PC, including dead PCs. Counts live
		// in that actor partition, not the combat-eligible player list.
		inner := a.counts[owner]
		if inner == nil {
			// Sized for one NeighborsEach ring (3x3 blocks).
			inner = make(map[worldgeom.MessageBlock]uint16, 16)
			a.counts[owner] = inner
		}
		b.NeighborsEach(func(n worldgeom.MessageBlock) {
			inner[n]++
		})
	}
	return a
}

/*
================
monsterActivitySnapshot.countedHere

Whether the census counted any admitted PC into the snapshot's
current world at block b - the gate's activity verdict. The
census only ever increments, so a nonzero count is membership.
The gate used to probe the census by the whole bucket key,
hashing the division string into every due actor's probe. The
nested census plus the per-rotation binding pays one small
block hash per actor instead: the world-keyed lookup happens
once per batch rotation, where world itself is assigned
(#3027).
================
*/
func (a *monsterActivitySnapshot) countedHere(b worldgeom.MessageBlock) bool {
	if !a.boundSet || a.boundWorld != a.world {
		a.bound, a.boundWorld, a.boundSet = a.counts[a.world], a.world, true
	}
	return a.bound[b] != 0
}

/*
================
activityBlockMemo

One actor's resolved activity block at one pose. The block is a pure
function of the pose (the plane surfaces are resident before the leg
runs), so the memo cannot go stale while the pose stands; a moved actor
misses on the pose guard and resolves fresh.
================
*/
type activityBlockMemo struct {
	pose  Spawn
	block worldgeom.MessageBlock
	ok    bool
}

/*
================
activityBlockFor

activityBlockFor is the gate's block resolution: memoized on the ops, so
a settled actor pays the cave surface walk once instead of once per
cadence. Shard-owned like resumePending; each ticker clone owns its map.
================
*/
func (ops *MonsterMoverOps) activityBlockFor(gid uint32, at Spawn) (worldgeom.MessageBlock, bool) {
	if memo, hit := ops.activityBlocks[gid]; hit && memo.pose == at {
		return memo.block, memo.ok
	}
	block, ok := ops.activityBlock(at)
	if ops.activityBlocks == nil {
		ops.activityBlocks = make(map[uint32]activityBlockMemo)
	}
	ops.activityBlocks[gid] = activityBlockMemo{pose: at, block: block, ok: ok}
	return block, ok
}

// runActivityGate precedes state expiry/acquisition (the retail server's
// tactics tick entry runs its timer setter the same way).
// A missing navigation answer is not an empty native message block.
// The returned mover is the value the caller continues with: the gate's
// own copy, which any commit it made has already carried into storage -
// the caller must not re-read the row it just gated. The last answer
// says whether a commit here advanced the row's generation: a pure
// cadence re-check stamps the cadence in place (monsteractivity.go's
// cadenceOnlyWrite) and leaves the generation, so the caller's chain
// base still names the row; every other commit rewrites the row and
// the caller must re-read before chaining generation-guarded commits
// off it. The answer is load-bearing only on the unhandled return.
func (ops *MonsterMoverOps) runActivityGate(division string, actor *monster.Instance, m monster.MoverState, now int64) ([]Frame, monster.MoverState, bool, bool) {
	if ops.activity == nil {
		return nil, m, false, false
	}
	pending := m.Mode() == monster.MoverPending
	if !pending && !monster.MaySuspendWander(actor.Nest.NativeTacticsFlags, m.ControllerGID() != 0, m.Mode()) {
		return nil, m, false, false
	}
	if m.Activity.Interval == 0 {
		panic("monster activity cadence was not initialized at activation")
	}
	before := m
	generationAdvanced := false
	if m.Activity.Due(uint32(now)) {
		b, ok := ops.activityBlockFor(actor.Gid, poseToSpawn(m.LivePoseAt(now, ops.GroundHeight)))
		resolved := ok && !ops.activity.unresolved[ops.activity.world]
		active := resolved && ops.activity.countedHere(b)
		if pending && active {
			mustMoverTransition(&m, monster.MoverEventActivityResumed, 0)
			return ops.startWanderLeg(division, actor, ops.resolveTactics(actor), m, now), m, true, true
		}
		if !pending && resolved && !active {
			mustMoverTransition(&m, monster.MoverEventActivityLost, 0)
			m.BehaviorDeadlineMs = 0
			pending = true
		}
		committed, cadenceOnly := ops.commitSteppedActivity(division, actor.Gid, before, m, now)
		if !committed {
			return nil, m, true, false
		}
		generationAdvanced = !cadenceOnly
	}
	if !pending {
		return nil, m, false, generationAdvanced
	}
	// The retail server's strategy clear clears the strategy target, but never stops the movement
	// channel. Arrival runs the PENDING callback without entering IDLE.
	if navigationNeedsContinuation(m, now) {
		goal, _ := m.NavigationGoal()
		return ops.commitSegment(division, actor, m, goal, actor.WalkSpeed(), currentChannel(m.Channel), now), m, true, true
	}
	if m.ArriveMs > m.DepartMs && !m.InFlight(now) {
		m.Pose = m.To
		m.From, m.To = monster.Pose{}, monster.Pose{}
		m.DepartMs, m.ArriveMs = 0, 0
		if !m.NoteMoveStopped(m.Pose) {
			mustMoverTransition(&m, monster.MoverEventSegmentArrived, 0)
		}
		return ops.commitSteppedMoverFrames(division, actor.Gid, m, []Frame{correctionFrame(actor.Gid, m.Pose)}), m, true, true
	}
	return nil, m, true, generationAdvanced
}

// Clock consumption must not run IDLE's target-ledger entry effects. A
// concurrent hit invalidates the entire planned suspension, including clock.
func (s *MonsterState) commitActivity(division string, gid uint32, before, after monster.MoverState, now int64) bool {
	committed, _ := s.commitActivityAt(division, gid, behaviorNoMoverSlot, behaviorNoMonsterSlot, behaviorNoQueueSlot, behaviorNoBaseGen, before, after, now)
	return committed
}

/*
================
commitActivityAt

commitActivity through the slots the behaviour batch carried for
the actor (#2133): the population, the guard read, the death check,
the write and the reschedule's queue arm go through the verified
hints and fall back to the gid resolve
whenever a hint no longer names the gid, so the commit lands - or
is refused - exactly as commitActivity's. The gate runs this once
per due actor per tick, which is where the profile charged the
storages' gid-to-slot maps. Validation rides only the full write:
a cadence-only commit differs from the validated stored row in
the stamp alone (#2029).

The second answer is the write's cadence verdict: whether the pair
differed only in the cadence stamp. The gate's chain-base rule
(generationAdvanced) reads it from here, where the pair was just
compared against the stored row, instead of re-comparing the two
whole rows at the gate (#2029). The reschedule reads no row back
either: the commit hands it the written row's schedule
(scheduleBehaviorCommitted).

baseGen is the commit generation of the row before was read
from - the step snapshot's, or behaviorNoBaseGen when the
caller holds none. The value guard is paid only when it can
fail: while the stored row's generation is baseGen and its
cadence is before's, the row is still before (a whole-value
write bumps the generation, the only other writes stamp the
cadence), so the expansion and whole-row compare are skipped;
anything else expands the row and compares it whole, exactly
as every commit did before (#3027).
================
*/
func (s *MonsterState) commitActivityAt(division string, gid uint32, slot, monsterSlot uint32, queueSlot uint32, baseGen uint64, before, after monster.MoverState, now int64) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObjectAt(division, gid, monsterSlot)
	current := before
	if gen, activity, exists := state.movers.activityGuardAt(slot, gid); !exists || gen != baseGen || activity != before.Activity {
		row, exists := state.movers.lookupAt(slot, gid)
		if !exists {
			row, exists = state.movers.lookup(gid)
		}
		if !exists || row != before {
			return false, false
		}
		current = row
	}
	if state.instances.isDeadAt(monsterSlot, gid) {
		return false, false
	}
	cadenceOnly := cadenceOnlyWrite(current, after)
	if cadenceOnly {
		// No Validate here: the pair differs only in the cadence
		// stamp, and the stored row passed validation when its own
		// commit wrote it, so the check would re-prove the row the
		// guard just read. The full write below keeps it (#2029).
		state.movers.setActivityAt(slot, gid, after.Activity)
	} else {
		if err := after.Validate(); err != nil {
			panic(err)
		}
		state.movers.setAt(slot, gid, after)
		// Approach membership derives from mode, target and instance
		// eligibility, none of which a cadence-only write moves (and
		// the gate's rows are never chasing or attacking), so the sync
		// rides only this full write and the mover commits (#2136).
		state.syncApproachActor(gid, after)
	}
	s.scheduleBehaviorCommitted(state, gid, scheduleOf(&after), monsterSlot, queueSlot, now)
	return true, cadenceOnly
}

/*
================
cadenceOnlyWrite

Whether after differs from current only in its activity cadence:
the gate's idle re-check commit. The new stamp must still be stored
(the next gate read fires off it) and still re-arms the scheduler,
but the row itself has nothing to rebuild (#2136).
================
*/
func cadenceOnlyWrite(current, after monster.MoverState) bool {
	current.Activity, after.Activity = monster.ActivityCadence{}, monster.ActivityCadence{}
	return current == after
}
