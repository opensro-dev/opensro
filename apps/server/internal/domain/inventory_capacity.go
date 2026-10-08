/*
===========================================================================

inventory_capacity.go - the character's inventory capacity and its growth

The capacity byte counts the 13 equipment sockets and the bag together, as
the 0x32B3 entry block ships it. A character is created with 45 (a 32-slot
bag) and the six QSP_*_EXINVENTORY quests add to it.

Native v1.188 pays the slots from the quest reward row (CBasicQuest_PayRewardRow
924CF0, byte +0x3db) through CGObjPC_ExpandInventoryByQuest (4E19D0), which
refuses silently past 112, and then announces the new size with 0x3092. The
v1.150 client has no such message: CICPlayer+0x1848, the bag size, is
written by the entry parser (8675F0), the sole direct caller of the inventory
rebuild (59DF10), which holds at most two 32-slot tabs. The port therefore
stops at 77. Inference for the v1.150 wire contract: a paid expansion waits
for the next world entry (login, teleport or resurrection), where the client
learns it; the v1.188 immediate-capacity message is not added to this client.

===========================================================================
*/

package domain

const (
	// DefaultInventorySize is the creation capacity: 13 sockets, 32 bag slots.
	DefaultInventorySize uint8 = 45
	// MaxInventorySize is two CIFInventory tabs of 32 behind the 13 sockets.
	MaxInventorySize uint8 = 77
)

/*
================
InventoryCapacity

The capacity byte in force: one past the last usable bag slot.
================
*/
func (character *Character) InventoryCapacity() uint8 {
	if character == nil || character.InventorySize == 0 {
		return DefaultInventorySize
	}
	return character.InventorySize
}

/*
================
InventoryCapacityValid

The persisted pair is in range: a capacity of 45..77 (zero reads as 45)
and waiting slots that still fit under 77.
================
*/
func (character *Character) InventoryCapacityValid() bool {
	size := character.InventoryCapacity()
	return size >= DefaultInventorySize && size <= MaxInventorySize &&
		int(size)+int(character.InventoryExpansion) <= int(MaxInventorySize)
}

/*
================
GrantInventoryExpansion

A quest's slot reward. It waits in InventoryExpansion until the next world
entry presents it. Past MaxInventorySize the grant is refused and the quest
still completes, as 4E19D0's refusal is ignored by its caller.
================
*/
func (character *Character) GrantInventoryExpansion(slots uint8) bool {
	if character == nil || slots == 0 {
		return false
	}
	total := int(character.InventoryCapacity()) + int(character.InventoryExpansion) + int(slots)
	if total > int(MaxInventorySize) {
		return false
	}
	character.InventoryExpansion += slots
	return true
}

/*
================
PresentInventoryExpansion

Moves the waiting slots into the capacity in force. A path that builds a
world entry calls it on the detached record it encodes; once the entry is
built, the live record adopts the encoded capacity (AdoptInventorySize).
================
*/
func (character *Character) PresentInventoryExpansion() bool {
	if character == nil || character.InventoryExpansion == 0 {
		return false
	}
	character.InventorySize = character.InventoryCapacity() + character.InventoryExpansion
	character.InventoryExpansion = 0
	return true
}

/*
================
AdoptInventorySize

The live record takes the capacity a world entry presented, moving exactly
that many waiting slots: a reward paid while the entry was being built
stays waiting for the next one.
================
*/
func (character *Character) AdoptInventorySize(presented uint8) {
	if character == nil {
		return
	}
	current := character.InventoryCapacity()
	if presented <= current || presented-current > character.InventoryExpansion {
		return
	}
	character.InventoryExpansion -= presented - current
	character.InventorySize = presented
}
