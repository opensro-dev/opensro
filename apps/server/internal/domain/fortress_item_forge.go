/*
===========================================================================

fortress_item_forge.go - the fortress smith's and trainer's production

One row per running production: the fortress, the
item, how many remain to collect, whether the order is done and when it
started and ends. A fortress holds at most one order per staff member
(at most 2 per kind). Start,
cancel and collect are the shard's forge add, remove and collect queries;
the siege tick's update marks it done.

===========================================================================
*/
package domain

/*
================
FortressItemForgeRecord

One fortress item-forge row. Count is what remains to collect; a
collection lowers it and the last one removes the row.
================
*/
type FortressItemForgeRecord struct {
	FortressID  uint32 `json:"fortressId"`
	ItemRefID   uint32 `json:"itemRefId"`
	Count       uint16 `json:"count"`
	Done        bool   `json:"done,omitempty"`
	StartedAtMs int64  `json:"startedAtMs"`
	EndsAtMs    int64  `json:"endsAtMs"`
}

/*
================
FortressItemForgeStart

The admitted order and its price. The commit rechecks the holder, the
actor's authority, the gold and the guild points under the store lock.
================
*/
type FortressItemForgeStart struct {
	Forge   FortressItemForgeRecord
	Holder  int64
	ActorID int64
	// Role is the fortress-role bit that grants production besides the
	// master: 0x08 for the smith, 0x10 for the trainer.
	Role uint8
	Gold int64
	GP   uint32
}

/*
================
FortressItemForgeCollect

The order after the collection (Count lowered; zero removes the row) and
the new bag row that receives the collected stack.
================
*/
type FortressItemForgeCollect struct {
	Forge   FortressItemForgeRecord
	Holder  int64
	ActorID int64
	Role    uint8
	Item    InventoryRow
}

/*
================
FortressItemForgeStore

The production rows and their two compound commits. Refusals are the
v1.150 fortress notice's low byte, as FortressStaffStore's are.
================
*/
type FortressItemForgeStore interface {
	// FortressItemForges returns a division's stored orders in fortress
	// and item order.
	FortressItemForges(divisionID string) ([]FortressItemForgeRecord, error)
	// StartFortressItemForge charges the actor's gold and the holder's
	// guild points and inserts the order in one transaction.
	StartFortressItemForge(divisionID string, start FortressItemForgeStart) (uint8, error)
	// SaveFortressItemForge rewrites (present) or removes one order.
	SaveFortressItemForge(divisionID string, forge FortressItemForgeRecord, present bool) error
	// CollectFortressItemForge writes the collected bag row and the
	// remaining order in one transaction.
	CollectFortressItemForge(divisionID string, collect FortressItemForgeCollect) (uint8, error)
}

// The client reads the low byte of the native production refusals
// (the fortress smith/trainer handlers).
const (
	FortressForgeErrFailure  uint8 = 0x02
	FortressForgeErrUnknown  uint8 = 0x03
	FortressForgeErrOwner    uint8 = 0x06
	FortressForgeErrBusy     uint8 = 0x0a
	FortressForgeErrGP       uint8 = 0x0e
	FortressForgeErrGold     uint8 = 0x0f
	FortressForgeErrCount    uint8 = 0x16
	FortressForgeErrRole     uint8 = 0x17
	FortressForgeErrWar      uint8 = 0x18
	FortressForgeErrNone     uint8 = 0x19
	FortressForgeErrNotDone  uint8 = 0x1a
	FortressForgeErrQuantity uint8 = 0x1b
	FortressForgeErrBag      uint8 = 0x1c
)
