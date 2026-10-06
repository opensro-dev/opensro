/*
===========================================================================

grant.go - package expansion through the shared inventory placement rules

Callers supply a detached inventory and publish it only after the complete
package and its currency transaction succeed.

===========================================================================
*/
package commerce

import (
	"fmt"
	"math"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const maxDeliverySlots = 255

/*
================
GrantPackage

Bound expanded quantities before iterating. Wide multiplication prevents a
malformed authored template from wrapping into a cheap or partial delivery.
================
*/
func GrantPackage(inv *inventory.Inventory, contents []Content, quantity uint16, capacity uint16) ([]uint8, error) {
	if inv == nil || quantity == 0 || capacity == 0 || len(contents) == 0 || len(contents) > maxPackageContents {
		return nil, fmt.Errorf("commerce: invalid grant")
	}
	destinations := []uint8{}
	for _, template := range contents {
		if template.Ref == nil || template.Stack == 0 {
			return nil, fmt.Errorf("commerce: invalid grant template")
		}
		units := uint64(quantity)
		stackable := inventory.IsEtcStackableTypeFlags(template.Ref.TypeFlags())
		if stackable && template.Data > 0 {
			units *= uint64(template.Data)
		}
		maximum := uint64(capacity)
		if stackable {
			maximum *= uint64(template.Stack)
		}
		if units > maximum {
			return nil, inventory.NewFault(wire.ErrCodeStorageFull, "packageExceedsContainerCapacity")
		}
		for units > 0 {
			amount := uint16(1)
			if stackable {
				amount = uint16(min(units, math.MaxUint16))
			}
			item := inventory.Item{TradeOwner: template.TradeOwner, RefObjID: template.Ref.RefObjID, Codename: template.Ref.Codename, TypeFlags: template.Ref.TypeFlags(), Plus: template.Plus, VarianceBits: template.Variance, Durability: template.Data, MagicOptions: append([]uint64(nil), template.Magic...), Quantity: amount}
			var slot uint8
			if stackable {
				grant, fault := inv.GrantStack(item, template.Stack)
				if fault != nil {
					return nil, fault
				}
				accepted := amount - grant.GroundRemainder
				if accepted == 0 {
					return nil, fmt.Errorf("commerce: package grant made no progress")
				}
				slot = grant.DestSlot
				units -= uint64(accepted)
			} else {
				var fault *inventory.Fault
				slot, fault = inv.Grant(item)
				if fault != nil {
					return nil, fault
				}
				units--
			}
			destinations = append(destinations, slot)
			if len(destinations) > maxDeliverySlots {
				return nil, fmt.Errorf("commerce: package exceeds native destination count")
			}
		}
	}
	return destinations, nil
}
