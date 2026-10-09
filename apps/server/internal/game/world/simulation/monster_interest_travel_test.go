/*
===========================================================================

monster_interest_travel_test.go - live visibility beyond the spawn region

A pursuing monster remains visible near its victim after leaving the original
region ring. Bootstrap and incremental publication must share that identity.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestMonsterInterestBeyondSpawnRegion
================
*/
func TestMonsterInterestBeyondSpawnRegion(t *testing.T) {
	ops, home, _ := scopeStreamFixture(t)
	actor := scopeStreamInstance(t, ops.Monsters, home, 100)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatal(err)
	}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatal(err)
	}
	far := RegionIDForSectors(SectorX(home)+3, SectorY(home))
	mover.From = monster.Pose{RegionID: home, X: 100, Y: 10, Z: 100}
	mover.Pose = mover.From
	mover.To = monster.Pose{RegionID: far, X: 100, Y: 10, Z: 100}
	mover.DepartMs, mover.ArriveMs = 0, 30000
	if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, mover) {
		t.Fatal("commit rejected")
	}
	if rows := ops.Monsters.InterestInstances(monsterTestDivision, worldgeom.RegionXZ{RegionID: far, X: 100, Z: 100}, 0); len(rows) != 0 {
		t.Fatal("future segment endpoint became visible before arrival")
	}
	for _, now := range []int64{20000, 30000} {
		pose := mover.LivePoseAt(now, nil)
		viewer := worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}
		rows := ops.Monsters.InterestInstances(monsterTestDivision, viewer, now)
		if len(rows) != 1 || rows[0].Gid != actor.Gid {
			t.Fatalf("at %d nearby travelling monster missing: %v", now, rows)
		}
		session := viewerSessionAt(pose.RegionID, pose.X)
		gids, _ := ops.Monsters.populationInterestDelta(monsterTestDivision, session.Population, viewer, now, nil)
		if len(gids) != 1 || gids[0] != actor.Gid {
			t.Fatalf("incremental interest lost traveller: %v", gids)
		}
	}
	session := viewerSessionAt(far, 100)
	viewer := worldgeom.RegionXZ{RegionID: far, X: 100, Z: 100}
	push := &fakePusher{}
	ops.runScopeVisibility(30000, []SessionSnapshot{session}, map[string]worldgeom.RegionXZ{"viewer": viewer}, map[string]bool{"viewer": true}, push)
	frames := scopeDeltaFrames(push, "viewer")
	if len(frames) != 1 || frames[0].Opcode != wire.OpSingleObjectSpawn || frames[0].ScopeGID != actor.Gid {
		t.Fatalf("expected one traveller create, got %v", frames)
	}
	push = &fakePusher{}
	ops.runScopeVisibility(30000, []SessionSnapshot{session}, map[string]worldgeom.RegionXZ{"viewer": viewer}, map[string]bool{"viewer": true}, push)
	if len(scopeDeltaFrames(push, "viewer")) != 0 {
		t.Fatal("unchanged traveller churned")
	}
	mover.Pose = mover.From
	mover.DepartMs, mover.ArriveMs = 0, 0
	if !ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, mover) {
		t.Fatal("return commit rejected")
	}
	push = &fakePusher{}
	ops.runScopeVisibility(30001, []SessionSnapshot{session}, map[string]worldgeom.RegionXZ{"viewer": viewer}, map[string]bool{"viewer": true}, push)
	frames = scopeDeltaFrames(push, "viewer")
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectDespawn || frames[0].ScopeGID != actor.Gid {
		t.Fatalf("returned traveller not removed: %v", frames)
	}
	if rows := ops.Monsters.InterestInstances(monsterTestDivision, viewer, 30001); len(rows) != 0 {
		t.Fatal("return left stale live-interest membership")
	}

}
