package simulation

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestAIActorDistanceAdaptersUseNativePlanes(t *testing.T) {
	from := monster.Pose{RegionID: 0x8001, X: 1000, Y: 20, Z: 1000}
	to := monster.Pose{RegionID: 0x81ff, X: 1003, Y: 24, Z: 1012}
	if got := tacticsDistance3D(from, to); got != 13 {
		t.Fatalf("pursuit/help/approach distance=%g", got)
	}
	if got := acquisitionDistance(from, poseToSpawn(to)); got != 13 {
		t.Fatalf("acquisition distance=%g", got)
	}
	to.RegionID = 0x6262
	if !math.IsInf(float64(tacticsDistance3D(from, to)), 1) || !math.IsInf(float64(acquisitionDistance(from, poseToSpawn(to))), 1) {
		t.Fatal("incompatible actor planes became finite targeting distance")
	}
}

func TestSummonRememberedDungeonTargetsUseLocalCoordinates(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(children) == 0 {
		t.Fatal("summon refused")
	}
	child := children[0]
	child.SummonSightRange = 0
	mover, _ := s.Mover("summon", child.Gid)
	mover.Pose = monster.Pose{RegionID: 0x8001, X: 1000, Y: 20, Z: 1000}
	first := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: 0x8002, X: 1001.00003, Y: 20, Z: 1000}}
	second := playerPose{Gid: PlayerObjectID(2), Pose: Spawn{RegionID: 0x8001, X: 1001.00002, Y: 20, Z: 1000}}
	for _, gid := range []uint32{first.Gid, second.Gid} {
		if !s.ArmRetaliation("summon", parent.Gid, gid) {
			t.Fatal("retaliation refused")
		}
	}
	ops := &MonsterMoverOps{Monsters: s}
	for _, players := range [][]playerPose{{first, second}, {second, first}} {
		got, found := ops.summonedTarget("summon", &child, mover, players, *now)
		if !found || got.Gid != first.Gid {
			t.Fatalf("native dungeon tie lost: %d/%v", got.Gid, found)
		}
	}
	first.Pose.Y += 10
	got, found := ops.summonedTarget("summon", &child, mover, []playerPose{first, second}, *now)
	if !found || got.Gid != second.Gid {
		t.Fatal("dungeon target height ignored")
	}
}

func TestFollowAdmissionNativeCoordinatePrecisionAndDungeonWords(t *testing.T) {
	for _, tc := range []struct {
		name      string
		from, to  monster.Pose
		threshold float64
		admitted  bool
	}{
		{"dungeon near across region words", monster.Pose{RegionID: 0x8001, X: 1000, Y: 20, Z: 1000}, monster.Pose{RegionID: 0x81ff, X: 1001, Y: 20, Z: 1000}, 300, false},
		{"rounded equality", monster.Pose{RegionID: 0x6262, X: 1000.00003, Y: 20, Z: 1000}, monster.Pose{RegionID: 0x6262, X: 1300, Y: 20, Z: 1000}, 300, true},
		{"vertical equality", monster.Pose{RegionID: 0x8001, X: 1000, Y: 20, Z: 1000}, monster.Pose{RegionID: 0x81ff, X: 1000, Y: 320, Z: 1000}, 300, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, parent, child, now := followFixture(t)
			mover, _ := ops.Monsters.Mover("summon", child.Gid)
			leader, _ := ops.Monsters.Mover("summon", parent.Gid)
			mover.Pose, leader.Pose = tc.from, tc.to
			ops.Monsters.CommitMover("summon", child.Gid, mover)
			ops.Monsters.CommitMover("summon", parent.Gid, leader)
			child.SummonerFollowRange = tc.threshold
			frames, _ := ops.followSummoner("summon", &child, mover, now)
			current, _ := ops.Monsters.Mover("summon", child.Gid)
			if (current.Mode() == monster.MoverFollowing) != tc.admitted || len(frames) != 0 {
				t.Fatalf("FOLLOW admission=%v; packets=%d", current.Mode(), len(frames))
			}
		})
	}
}
