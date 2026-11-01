package action

import (
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// FramesFromSocketVisuals is the M1 emit glue: it turns the post-move socket
// occupancy (inventory.EquipVisualChanges) into the visual pushes that ride
// behind the 0xB06D move result.
//
//   - a WORN socket pushes 0x3314 with the occupant's identity: RefObjID,
//     the type word (it gates the opt tail), and OptLevel = the item's Plus -
//     exactly the fixture's buildV150EquipVisualPayload(objectId,
//     worn.refObjId, worn.typeFlags, worn.plus);
//   - a VACATED socket pushes the 0x377C clear with RefObjID 0, which takes
//     the non-avatar arm.
//   - a socket that ends up holding AMMUNITION also pushes the 0x377C
//     clear: ammo has no worn visual (the native equipment visual test
//     is false for it and the client maps it to no equipment slot),
//     so the equip arm never fires for it, and the clear is what detaches
//     a shield the pair-swap displaced from socket 7 (the unequip arm of
//     the native move contribution slots for the item leaving
//     the socket). Without it the shield stayed drawn over the bow.
//
// Handlers must run this for EVERY transfer that touched a socket < 13; a
// bag-only move produces no changes and therefore no frames. Without the
// pushes the containers update but the doll and the world model keep wearing
// the old item.
func FramesFromSocketVisuals(objectID uint32, changes []inventory.SocketVisual) []wire.Frame {
	frames := make([]wire.Frame, 0, len(changes))
	for _, change := range changes {
		if change.Worn && ammoFamily(change.Item) == 0 {
			frames = append(frames, wire.EquipVisualFrame(wire.EquipVisual{
				Gid:       objectID,
				RefObjID:  change.Item.RefObjID,
				TypeFlags: change.Item.TypeFlags,
				OptLevel:  change.Item.Plus,
			}))
		} else {
			frames = append(frames, wire.UnequipVisualFrame(wire.UnequipVisual{
				Gid:      objectID,
				Slot:     change.Socket,
				RefObjID: 0,
			}))
		}
	}
	return frames
}
