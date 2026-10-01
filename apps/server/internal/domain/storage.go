/*
===========================================================================

storage.go - the account warehouse (NPC storage)

Retail keeps one warehouse per account (_Chest by user id), shared by every
character of that account on the shard. The client shows it as
CIFStorageRoom: pages of 30 slots, a capacity byte and a gold balance
(0x3126 gold, 0x321A list; CIFStorageRoom_SetSlotItem / _SetGold).

===========================================================================
*/
package domain

// StorageDefaultCapacity is the slot count of a warehouse no item has
// expanded. INFERENCE: retail's default _User storage size; the v1.150 room
// pages it as five pages of 30 (CIFStorageRoom_GetSlotItem, 0x1E per page).
const StorageDefaultCapacity = 150

// StorageMaxCapacity bounds the capacity byte the list carries.
const StorageMaxCapacity = 255

/*
================
AccountStorage

Rows reuse the inventory row body; Slot is the warehouse slot index.
================
*/
type AccountStorage struct {
	Capacity int64          `json:"capacity"`
	Gold     int64          `json:"gold"`
	Rows     []InventoryRow `json:"rows,omitempty"`
}

/*
================
NewAccountStorage

The warehouse an account opens before anything was ever stored.
================
*/
func NewAccountStorage() AccountStorage {
	return AccountStorage{Capacity: StorageDefaultCapacity}
}

/*
================
StorageAuthority

TransactStorage runs mutate with the account and character locked, on
detached copies of the character and the warehouse. Only the character's
inventory and gold may change. A refused mutate or a failed durable commit
changes neither plane.
================
*/
type StorageAuthority interface {
	AccountStorage(character *Character) (AccountStorage, error)
	TransactStorage(character *Character, mutate func(next *Character, storage *AccountStorage) error) (AccountStorage, error)
}
