/*
===========================================================================

peeravatar_test.go - other players see a worn avatar dress

A player report: other players could not see a worn avatar dress, and the
reporter could not see theirs. The peer spawn row's avatar loop
(client 86B0C7) went out empty; it now carries the avatar inventory.

===========================================================================
*/
package movement

import (
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// avatarDressTid is an equipment-band item in the avatar group
// ((tid & 0x780) == 0x680), the band whose rows carry the extra byte.
const avatarDressTid uint16 = 0x0c | 0x20 | 0x680

/*
================
TestPeerSpawnRowCarriesTheWornAvatars
================
*/
func TestPeerSpawnRowCarriesTheWornAvatars(t *testing.T) {
	c := &domain.Character{Name: "Dressed", AvatarInventory: &domain.AvatarInventory{Capacity: 4, Rows: []domain.InventoryRow{
		{Slot: 2, RefObjID: 23296, TypeFlags: avatarDressTid, StackCount: 1},
		{Slot: 0, RefObjID: 23295, TypeFlags: avatarDressTid, StackCount: 1},
	}}}
	appearance := peerAppearance(nil, nil, "test", capturePeerAppearance(c, 1907))
	ids := appearance.ItemIDs()
	if !slices.Contains(ids, 23295) || !slices.Contains(ids, 23296) {
		t.Fatalf("the dresses' references would not reach the viewer first: %v", ids)
	}
	payload := simulation.BuildPeerSpawnRow(*appearance, 100144, simulation.Spawn{RegionID: 22387, X: 992, Z: 384})
	row, err := wire.DecodePlayerSpawnRow(payload, map[uint32]uint16{23295: avatarDressTid, 23296: avatarDressTid}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Avatars) != 2 || row.Avatars[0].RefObjID != 23295 || row.Avatars[1].RefObjID != 23296 {
		t.Fatalf("the spawn row carries avatars %+v, want 23295 then 23296 in socket order", row.Avatars)
	}
	if row.Name != "Dressed" || row.Gid != 100144 {
		t.Fatalf("the avatar loop desynchronised the rest of the row: %+v", row)
	}
}
