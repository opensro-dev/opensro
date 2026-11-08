package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

func TestNearestPlayerWithinSeesAcrossAdjacentRegion(t *testing.T) {
	west := RegionIDForSectors(40, 50)
	east := RegionIDForSectors(41, 50)
	monster := monster.Pose{RegionID: west, X: 1910, Y: 20, Z: 100}
	player := playerPose{Gid: 77, Pose: Spawn{RegionID: east, X: 10, Y: 20, Z: 100}}

	got, ok := nearestPlayerWithin(monster, []playerPose{player}, 25)
	if !ok || got.Gid != player.Gid {
		t.Fatalf("adjacent-region player was not acquired: got=%+v ok=%v", got, ok)
	}
}

func TestMonsterHeadingUsesRegionAwareTravelVector(t *testing.T) {
	west := RegionIDForSectors(40, 50)
	east := RegionIDForSectors(41, 50)
	from := monster.Pose{RegionID: west, X: 1910, Y: 20, Z: 100}
	to := monster.Pose{RegionID: east, X: 10, Y: 20, Z: 100}
	want := headingWordFromDelta(20, 0)
	if got := headingWordToward(from, to); got != want {
		t.Fatalf("cross-region heading=%#04x, want eastward %#04x", got, want)
	}
}

func TestMonsterSourceGateTreatsSeamCrossingAsShallowContinuation(t *testing.T) {
	west := RegionIDForSectors(40, 50)
	east := RegionIDForSectors(41, 50)
	mover := monster.MoverState{}
	mover.From = monster.Pose{RegionID: west, X: 1900, Y: 20, Z: 100}
	mover.To = monster.Pose{RegionID: west, X: 1910, Y: 20, Z: 100}
	from := mover.To
	dest := monster.Pose{RegionID: east, X: 10, Y: 20, Z: 100}

	if monsterMovementSourceRequired(mover, from, dest, true) {
		t.Fatal("straight eastward seam crossing was misclassified as a hard turn")
	}
}

func TestCommitSegmentCanonicalizesOverflowForEveryDestinationProducer(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if !ok {
		t.Fatal("fixture mover missing")
	}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatalf("start wander: %v", err)
	}
	fromRegion := mover.Pose.RegionID
	dest := monster.Pose{
		RegionID: fromRegion,
		X:        NativeRegionSize + 5,
		Y:        mover.Pose.Y,
		Z:        mover.Pose.Z,
	}
	frames := ops.commitSegment(
		monsterTestDivision,
		&instance,
		mover,
		dest,
		instance.Ref.WalkSpeed,
		wire.MoveStateWalk,
		1_784_000_000_000,
	)
	if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
		t.Fatalf("canonical segment frames=%+v, want one movement goal", frames)
	}
	_, region, x, _, _, _ := decodeGoalPayload(t, frames[0].Payload)
	wantRegion := RegionIDForSectors(SectorX(fromRegion)+1, SectorY(fromRegion))
	if region != wantRegion || x != 5 {
		t.Fatalf("canonical goal region=%#04x x=%d, want region=%#04x x=5", region, x, wantRegion)
	}
}
