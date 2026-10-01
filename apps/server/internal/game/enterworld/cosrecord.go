/*
===========================================================================

cosrecord.go - owner-private companion records

Character references select the native wire grammar. Riding horses omit the
persistent death word; pet records additionally carry progression and slot.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"opensro.online/server/internal/game/item/wire"
	"strconv"
)

/*
================
BuildCOSRecord

830EC0 selects the grammar from characterdata. The record and world actor
must describe the same canonical companion before either is published.
================
*/
func BuildCOSRecord(cos *CharacterCOS, ref *CharacterRef, items ItemRefSource) ([]byte, error) {
	if cos == nil || ref == nil || cos.RefObjID != ref.RefObjID || cos.GID == 0 || ref.TidWord&0x7fe != 0x1c6 {
		return nil, fmt.Errorf("invalid COS record identity")
	}
	band := ref.TidWord >> 11
	if band < 1 || band > 4 || len([]byte(cos.Name)) > 65535 {
		return nil, fmt.Errorf("unsupported COS record family or name")
	}
	w := wire.NewWriter(64).U32(cos.GID).U32(cos.RefObjID).U32(cos.CurrentHP).U32(cos.CurrentMP)
	if band == 3 {
		w.U64(cos.Experience).U8(cos.Level).U16(cos.Satiety)
	}
	if band == 3 || band == 4 {
		w.U32(cos.CommandMode).U16(uint16(len([]byte(cos.Name)))).Bytes([]byte(cos.Name))
	}
	if cos.Container == nil {
		w.U8(0)
	} else {
		bag := cos.Container
		if bag.Capacity == 0 || bag.Capacity > 140 || len(bag.Rows) > int(bag.Capacity) {
			return nil, fmt.Errorf("invalid COS container capacity")
		}
		w.U8(bag.Capacity).U8(uint8(len(bag.Rows)))
		seen := map[int64]bool{}
		for _, row := range bag.Rows {
			if items == nil {
				return nil, fmt.Errorf("missing COS item references")
			}
			r, ok := items.ItemRefByCodename(row.Codename)
			_, varianceErr := strconv.ParseUint(row.VarianceBits, 10, 64)
			if !ok || r == nil || r.RefObjID != row.RefObjID || r.TypeFlags() != row.TypeFlags || row.Slot < 0 || row.Slot >= int64(bag.Capacity) || seen[row.Slot] || row.StackCount < 1 || row.StackCount > 65535 || len(row.MagicOptions) > 12 || row.Plus < 0 || row.Plus > 255 || row.Durability < 0 || row.Durability > 0xffffffff || varianceErr != nil && row.VarianceBits != "" {
				return nil, fmt.Errorf("invalid COS container row")
			}
			// The current durable InventoryRow represents equipment and plain
			// expendables; do not silently encode a summon record as equipment.
			if row.TypeFlags&0x60 != 0x20 && row.TypeFlags&0x60 != 0x60 {
				return nil, fmt.Errorf("unsupported COS item body")
			}
			group := row.TypeFlags & 0x780
			if row.TypeFlags&0x60 == 0x60 && (group == 0x280 || group == 0x400) {
				return nil, fmt.Errorf("COS item requires a wider or labeled durable body")
			}
			seen[row.Slot] = true
			w.U8(uint8(row.Slot)).Bytes(BuildItemBody(InventoryWireItems([]InventoryRow{row})[0]))
		}
	}
	dead := uint32(0)
	if cos.CurrentHP == 0 {
		dead = 1
	}
	if band != 1 {
		w.U32(dead)
	}
	if band == 3 || band == 4 {
		w.U8(cos.InventorySlot)
	}
	return w.Payload(), nil
}
