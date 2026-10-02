/*
===========================================================================

fear_acquisition_test.go - native detection priority over Fear exclusion

529A60 returns success for detected body states before the source exclusion.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestFearPreservesDetectionBranch
================
*/
func TestFearPreservesDetectionBranch(t *testing.T) {
	a := monster.Instance{Abnormal: &abnormal.Block{Mask: abnormal.Fear.Bit()}}
	a.Ref.TidWord = 0x8c6
	a.Nest.NativeTacticsFlags = 0x200
	a.Abnormal.Slots[abnormal.Fear].Active = true
	a.Abnormal.Slots[abnormal.Fear].SourceGID = 7
	from := monster.Pose{RegionID: 25000, X: 1000, Z: 1000}
	p := playerPose{Gid: 7, NativeBodyStatus: 6, Pose: Spawn{RegionID: 25000, X: 1005, Z: 1000}}
	if !ordinaryPlayerHostility(a, p) {
		t.Fatal("fixture did not admit native detection")
	}
	if got, ok := ordinaryPlayerAcquisition(a, from, []playerPose{p}, 100); !ok || got.Gid != 7 {
		t.Fatalf("Fear bypassed native detection branch: gid=%d found=%v", got.Gid, ok)
	}
}

/*
================
TestFearAcquisitionAndRememberedOpponent
================
*/
func TestFearAcquisitionAndRememberedOpponent(t *testing.T) {
	actor := monster.Instance{Abnormal: &abnormal.Block{Mask: abnormal.Fear.Bit()}}
	actor.Ref.TidWord = 0x8c6
	actor.Abnormal.Slots[abnormal.Fear].Active = true
	actor.Abnormal.Slots[abnormal.Fear].SourceGID = 7
	from := monster.Pose{RegionID: 25000, X: 1000, Z: 1000}
	players := []playerPose{
		{Gid: 7, Pose: Spawn{RegionID: 25000, X: 1005, Z: 1000}},
		{Gid: 8, Pose: Spawn{RegionID: 25000, X: 1010, Z: 1000}},
	}
	if got, ok := ordinaryPlayerAcquisition(actor, from, players, 100); !ok || got.Gid != 8 {
		t.Fatalf("acquired feared source: %+v, %v", got, ok)
	}
	// Fear's AI event abandons an opponent; the remembered lookup itself
	// must not acquire a hostility check that 547E04 never calls.
	if _, ok := eligiblePlayerByGid(actor, players, 7); !ok {
		t.Fatal("remembered lookup acquired the acquisition-only Fear gate")
	}
	actor.Abnormal.Slots[abnormal.Fear].Active = false
	if got, ok := ordinaryPlayerAcquisition(actor, from, players, 100); !ok || got.Gid != 7 {
		t.Fatalf("expired Fear still excludes source: %+v, %v", got, ok)
	}
}
