package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

func TestNestDetachRetainsActorAndReleasesCapacityOnce(t *testing.T) {
	ops, actor, now := activityFixture(t)
	s := ops.Monsters
	s.mu.Lock()
	state := s.populationForObject(monsterTestDivision, actor.Gid)
	index := state.gidNests[actor.Gid]
	live := state.nests[index].live
	before := state.movers.get(actor.Gid)
	if !s.detachNestLocked(state, actor.Gid, now) || s.detachNestLocked(state, actor.Gid, now+1) {
		t.Fatal("detachment must release exactly once")
	}
	after := state.instances.get(actor.Gid)
	if !after.NestDetached || after.Nest != actor.Nest || after.CurrentHP != actor.CurrentHP || state.movers.get(actor.Gid) != before || state.nests[index].live != live-1 {
		t.Fatal("detachment changed actor state or failed to release capacity")
	}
	s.mu.Unlock()
	if !s.Defeat(monsterTestDivision, actor.Gid, time.UnixMilli(now+2)) || s.Defeat(monsterTestDivision, actor.Gid, time.UnixMilli(now+3)) {
		t.Fatal("detached actor death was lost or accepted twice")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.nests[index].live != live-1 {
		t.Fatal("death released the detached slot twice")
	}
}

func TestCrossPlaneDetachPrecedesActivityAndIdle(t *testing.T) {
	ops, actor, now := activityFixture(t)
	s := ops.Monsters
	s.mu.Lock()
	state := s.populationForObject(monsterTestDivision, actor.Gid)
	m := state.movers.get(actor.Gid)
	m.Pose.RegionID = actor.Nest.RegionID ^ 0x8000
	m.From, m.To = monster.Pose{}, monster.Pose{}
	m.DepartMs, m.ArriveMs = 0, 0
	state.movers.set(actor.Gid, m)
	s.mu.Unlock()
	frames, targeted := ops.advanceInstance(monsterTestDivision, actor, nil, now+1001)
	if len(frames) != 0 || targeted != nil {
		t.Fatal("detachment must return before emitting behavior")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !state.instances.get(actor.Gid).NestDetached || state.movers.get(actor.Gid) != m {
		t.Fatal("plane change failed to detach or advanced activity/state")
	}
}

func TestDetachedHomeRetainsNestGeometryAndTacticsGate(t *testing.T) {
	a := monster.Instance{NestDetached: true, Ref: monster.MonsterRef{RunSpeed: 10}}
	a.Nest.HasControls = true
	a.Nest.RegionID, a.Spawn.RegionID = monsterTestRegion, monsterTestRegion
	a.Nest.X, a.Nest.Z, a.Nest.Radius = 100, 100, 20
	a.Spawn.X, a.Spawn.Z = 500, 500
	p := monster.Pose{RegionID: monsterTestRegion, X: 125, Z: 100}
	if needsHoming(a, p) {
		t.Fatal("detached actor without HomingData was constrained by its former nest")
	}
	a.Nest.Controls.HomingData = 1
	if !needsHoming(a, p) {
		t.Fatal("retained nest center/radius was replaced by generated spawn or HomingData")
	}
}
