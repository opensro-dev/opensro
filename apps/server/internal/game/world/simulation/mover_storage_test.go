package simulation

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"unsafe"
)

func TestResidentPendingPreservesIndependentActorState(t *testing.T) {
	a := monster.PendingMover{Spawning: true, BehaviorDeadlineMs: 918273, Pose: monster.Pose{RegionID: monsterTestRegion, X: 12.125, Y: -3, Z: 98, Heading: 123}, Activity: monster.ActivityCadence{Interval: 1739, LastCheck: 0xfffffffe}, PreviousEvent: 2, LastEvent: 3, TransitionSerial: 919, RetaliationRevision: 17, AttackIntervalMs: 2000, LastBattleActivityMs: 123, HomingStartedMs: 345, HomingAcquireAfterMs: 678, Channel: 2, PursuitChannel: 3, NavigationChannel: 4, NavigationSpeed: math.Copysign(0, -1)}
	b := a
	b.Pose.X = 45.5
	b.Activity.LastCheck = 9
	var x, y residentPendingMover
	x.set(a)
	y.set(b)
	if x.shared != y.shared {
		t.Fatal("identical immutable fields were not shared")
	}
	check := func(p *residentPendingMover, want monster.PendingMover) {
		t.Helper()
		got, ok := p.value().PendingSnapshot()
		if !ok || got != want || math.Float64bits(got.NavigationSpeed) != math.Float64bits(want.NavigationSpeed) {
			t.Fatalf("actor state changed: got %+v want %+v", got, want)
		}
	}
	check(&x, a)
	check(&y, b)
	a.TransitionSerial++
	a.Pose.Z = 77
	x.set(a)
	check(&x, a)
	check(&y, b)
	detached := y.value()
	detached.Pose.X = 999
	check(&y, b)
	if unsafe.Sizeof(residentPendingMover{}) >= unsafe.Sizeof(monster.PendingMover{}) {
		t.Fatal("resident representation did not shrink")
	}
}
