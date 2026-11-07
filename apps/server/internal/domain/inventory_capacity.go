/*
===========================================================================

inventory_capacity.go - the character's inventory capacity and its growth

The capacity byte counts the 13 equipment sockets and the bag together, as
the 0x32B3 entry block ships it. A character is created with 45 (a 32-slot
bag) and the six QSP_*_EXINVENTORY quests add to it.

Native pays the slots from the quest reward row through the player's quest
inventory expansion: it refuses silently past the server's limit, and on
completion the running character's storage grows at once and the server
announces the new size with 0x3092 (research/investigations/items/
native-audit-rows-20261031.md, row 26). The v1.150 client defines the
shipped maximum: its inventory holds at most two 32-slot tabs behind the 13
sockets, so the port stops at 77 where the server's own limit is higher.
Slots granted before this rule was ported may still wait in
InventoryExpansion on a saved record; a world entry presents those
(PresentInventoryExpansion). New grants never wait.

===========================================================================
*/

package domain

const (
	// DefaultInventorySize is the creation capacity: 13 sockets, 32 bag slots.
	DefaultInventorySize uint8 = 45
	// MaxInventorySize is two CIFInventory tabs of 32 behind the 13 sockets.
	MaxInventorySize uint8 = 77
	// EquipmentSlotEnd is one past the last equipment socket: slots
	// 0..12 of MissionInventory are worn equipment, the rest is the bag.
	// It mirrors the item/inventory package's constant of the same name;
	// that package imports domain, so the value is stated here and the
	// two are tied by test.
	EquipmentSlotEnd = 13
	// JobSuitSlot is the job suit's equipment socket, which the
	// free-battle cape shares: the PvP relation rules read it (action
	// playerRelationEquipment), so the pet-owner view keeps it.
	JobSuitSlot = 8
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

A quest's slot reward, applied to the capacity in force at the turn-in:
native grows the running character's storage when the expansion completes.
Past MaxInventorySize the grant is refused and the quest still completes,
as the native expansion's refusal is ignored by its caller. Slots still
waiting from before this rule was ported count against the limit and keep
waiting for their world entry.
================
*/
func (character *Character) GrantInventoryExpansion(slots uint8) bool {
	if character == nil || slots == 0 {
		return false
	}
	size := character.InventoryCapacity()
	total := int(size) + int(character.InventoryExpansion) + int(slots)
	if total > int(MaxInventorySize) {
		return false
	}
	character.InventorySize = size + slots
	return true
}

/*
================
PresentInventoryExpansion

Moves the waiting slots into the capacity in force. Only records saved
before grants applied live can hold waiting slots. A path that builds a
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
