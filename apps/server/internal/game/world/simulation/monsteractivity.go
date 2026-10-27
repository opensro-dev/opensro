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
type activityBucket struct {
	owner activityWorld
	block worldgeom.MessageBlock
}
type monsterActivitySnapshot struct {
	counts     map[activityBucket]uint16
	unresolved map[activityWorld]bool
	world      activityWorld
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
	a := &monsterActivitySnapshot{counts: make(map[activityBucket]uint16), unresolved: make(map[activityWorld]bool)}
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
		for _, n := range b.Neighbors() {
			a.counts[activityBucket{owner, n}]++
		}
	}
	return a
}

// runActivityGate precedes state expiry/acquisition (the retail server's
// tactics tick entry runs its timer setter the same way).
// A missing navigation answer is not an empty native message block.
func (ops *MonsterMoverOps) runActivityGate(division string, actor monster.Instance, m monster.MoverState, now int64) ([]Frame, bool) {
	if ops.activity == nil {
		return nil, false
	}
	pending := m.Mode() == monster.MoverPending
	if !pending && !monster.MaySuspendWander(actor.Nest.NativeTacticsFlags, m.ControllerGID() != 0, m.Mode()) {
		return nil, false
	}
	if m.Activity.Interval == 0 {
		panic("monster activity cadence was not initialized at activation")
	}
	before := m
	if m.Activity.Due(uint32(now)) {
		b, ok := ops.activityBlock(poseToSpawn(m.LivePoseAt(now, ops.GroundHeight)))
		resolved := ok && !ops.activity.unresolved[ops.activity.world]
		active := resolved && ops.activity.counts[activityBucket{ops.activity.world, b}] != 0
		if pending && active {
			mustMoverTransition(&m, monster.MoverEventActivityResumed, 0)
			return ops.startWanderLeg(division, actor, ops.resolveTactics(actor), m, now), true
		}
		if !pending && resolved && !active {
			mustMoverTransition(&m, monster.MoverEventActivityLost, 0)
			m.BehaviorDeadlineMs = 0
			pending = true
		}
		if !ops.Monsters.commitActivity(division, actor.Gid, before, m, now) {
			return nil, true
		}
	}
	if !pending {
		return nil, false
	}
	// The retail server's strategy clear clears the strategy target, but never stops the movement
	// channel. Arrival runs the PENDING callback without entering IDLE.
	if navigationNeedsContinuation(m, now) {
		goal, _ := m.NavigationGoal()
		return ops.commitSegment(division, actor, m, goal, actor.WalkSpeed(), currentChannel(m.Channel), now), true
	}
	if m.ArriveMs > m.DepartMs && !m.InFlight(now) {
		m.Pose = m.To
		m.From, m.To = monster.Pose{}, monster.Pose{}
		m.DepartMs, m.ArriveMs = 0, 0
		if !m.NoteMoveStopped(m.Pose) {
			mustMoverTransition(&m, monster.MoverEventSegmentArrived, 0)
		}
		return ops.Monsters.CommitMoverFrames(division, actor.Gid, m, []Frame{correctionFrame(actor.Gid, m.Pose)}), true
	}
	return nil, true
}

// Clock consumption must not run IDLE's target-ledger entry effects. A
// concurrent hit invalidates the entire planned suspension, including clock.
func (s *MonsterState) commitActivity(division string, gid uint32, before, after monster.MoverState, now int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	current, exists := state.movers.lookup(gid)
	if !exists || current != before || state.instances.isDead(gid) {
		return false
	}
	if err := after.Validate(); err != nil {
		panic(err)
	}
	state.movers.set(gid, after)
	state.syncApproachActor(gid, after)
	s.scheduleBehavior(state, gid, now)
	return true
}
