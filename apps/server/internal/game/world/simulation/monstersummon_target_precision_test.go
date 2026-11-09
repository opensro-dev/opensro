package simulation

import "testing"

func TestSummonRememberedTargetFloat32TiePrecedesSnapshotOrder(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(children) == 0 {
		t.Fatal("summon refused")
	}
	child := children[0]
	child.SummonSightRange = 0
	mover, _ := s.Mover("summon", child.Gid)
	mover.Pose.X = 1000
	first := playerPose{Gid: PlayerObjectID(1), Pose: poseToSpawn(mover.Pose)}
	second := playerPose{Gid: PlayerObjectID(2), Pose: first.Pose}
	// Both coordinates become 1001 in the native float position domain.
	// Go interpolation may retain these extra bits; they must not override
	// the first remembered slot on a native-distance tie.
	first.Pose.X, second.Pose.X = 1001.00003, 1001.00002
	for _, gid := range []uint32{first.Gid, second.Gid} {
		if !s.ArmRetaliation("summon", parent.Gid, gid) {
			t.Fatal("history not recorded")
		}
	}
	ops := &MonsterMoverOps{Monsters: s}
	for _, players := range [][]playerPose{{first, second}, {second, first}} {
		got, found := ops.summonedTarget("summon", &child, mover, players, *now)
		if !found || got.Gid != first.Gid {
			t.Fatalf("native tie selected %d/%v", got.Gid, found)
		}
	}
	// Filtering precedes comparison, even when the rejected slot would tie.
	first.NativeBodyStatus = 2
	got, found := ops.summonedTarget("summon", &child, mover, []playerPose{first, second}, *now)
	if !found || got.Gid != second.Gid {
		t.Fatal("invalid first slot defeated eligible second slot")
	}
}
