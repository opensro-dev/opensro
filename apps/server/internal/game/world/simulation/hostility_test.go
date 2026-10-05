package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

func TestOrdinaryAcquisitionAndRememberedLookupUseDifferentChecks(t *testing.T) {
	// Shared actor getters let a COS-shaped observer bypass 540DE0, but
	// 5299E0 still rejects status 3. This distinguishes the two call paths.
	actor := monster.Instance{Ref: monster.MonsterRef{TidWord: 0x1c6}}
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	players := []playerPose{{Gid: 17, NativeBodyStatus: 3,
		Pose: Spawn{RegionID: monsterTestRegion, X: 1010, Z: 1000}}}
	if _, ok := ordinaryPlayerAcquisition(actor, from, players, 100); ok {
		t.Fatal("acquisition skipped hostility")
	}
	if _, ok := eligiblePlayerByGid(actor, players, 17); !ok {
		t.Fatal("remembered lookup incorrectly acquired a hostility gate")
	}
	actor.Ref.TidWord = 0xc6
	actor.Nest.NativeTacticsFlags = 0x200
	players[0].NativeBodyStatus = 6
	if _, ok := ordinaryPlayerAcquisition(actor, from, players, 100); !ok {
		t.Fatal("ordinary detecting monster rejected status 6")
	}
	actor.Nest.NativeTacticsFlags = 0
	if _, ok := ordinaryPlayerAcquisition(actor, from, players, 100); ok {
		t.Fatal("ordinary monster acquired undetected status 6")
	}
}
