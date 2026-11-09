/*
===========================================================================

chatitemlinks.go - the item a chat line links, read from its sender

Port-only, not native (SRO_CHAT_ITEM_LINKS, owner decision 2026-11-07).
The chat lane sends only where an item sits; this runtime owns the
inventory, the warehouse session and the pet bag, so it reads the item
itself (chat.ItemLinkSource). A client can link only what its character
holds: the bag and worn gear, the costume slots, the pet bag, and the
warehouse while its window is open. Guild storage and the exchange window
are not sources (owner, 2026-11-07).

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
)

// The runtime is the chat lane's item source.
var _ chat.ItemLinkSource = (*Runtime)(nil)

/*
================
Runtime.LinkedItem

The item ref names in the sender's own state, as an item body and the
label text consumers store ("<Name +N>"). False when the slot is empty,
the warehouse is closed, no pet is out, or the source is unknown.
================
*/
func (rt *Runtime) LinkedItem(divisionID string, character *enterworld.Character, ref chat.LinkRef) (chat.LinkedItem, bool) {
	if character == nil {
		return chat.LinkedItem{}, false
	}
	var rows []domain.InventoryRow
	switch ref.Source {
	case chat.LinkSourceInventory:
		rows = character.MissionInventory
	case chat.LinkSourceAvatar:
		if character.AvatarInventory != nil {
			rows = character.AvatarInventory.Rows
		}
	case chat.LinkSourcePet:
		// A pet link names which pet it means (ref.Pet). A grab pet is a
		// persistent companion living on its summon item's row, not
		// ActiveCOS (the transport), so the old ActiveCOS-only read
		// linked the transport's same-slot item or nothing. The bag is
		// read the ownedCOSContainer way: an owned, summoned, alive
		// companion with a bag (CompanionByGID already requires owned
		// and summoned; a gid of 0 names no pet and reads nothing).
		if pet := character.CompanionByGID(ref.Pet); pet != nil && !character.DeletePending && pet.CurrentHP != 0 && pet.Container != nil {
			rows = pet.Container.Rows
		}
	default:
		// The warehouse is read by StorageLinks, outside any door.
		return chat.LinkedItem{}, false
	}
	return rt.linkedRow(rows, ref.Slot)
}

/*
================
Runtime.StorageLinks

The warehouse refs among refs, read while the warehouse is open. The store
takes its own lock and keys its accounts by the live character, so this
runs before any character door (the Global Chatting use's door holds that
lock) and with the live character, never a snapshot.
================
*/
func (rt *Runtime) StorageLinks(divisionID string, live *enterworld.Character, refs []chat.LinkRef) map[chat.LinkRef]chat.LinkedItem {
	wanted := false
	for _, ref := range refs {
		wanted = wanted || ref.Source == chat.LinkSourceStorage
	}
	if !wanted || live == nil || rt.storageAuthority == nil {
		return nil
	}
	if _, open := rt.storageSession(divisionID, live.Name); !open {
		return nil
	}
	storage, err := rt.storageAuthority.AccountStorage(live)
	if err != nil {
		return nil
	}
	read := map[chat.LinkRef]chat.LinkedItem{}
	for _, ref := range refs {
		if ref.Source != chat.LinkSourceStorage {
			continue
		}
		if item, ok := rt.linkedRow(storage.Rows, ref.Slot); ok {
			read[ref] = item
		}
	}
	return read
}

/*
================
Runtime.linkedRow

The row at slot as a linked item.
================
*/
func (rt *Runtime) linkedRow(rows []domain.InventoryRow, slot uint16) (chat.LinkedItem, bool) {
	for _, row := range rows {
		if row.Slot == int64(slot) && row.RefObjID != 0 {
			return chat.LinkedItem{
				Body:  enterworld.BuildItemBody(enterworld.InventoryWireItems([]domain.InventoryRow{row})[0]),
				Label: rt.linkLabel(row),
				// A reader holds only the references it was sent (BUG-084).
				References: rt.itemReferencesByID([]uint32{row.RefObjID}),
			}, true
		}
	}
	return chat.LinkedItem{}, false
}

/*
================
Runtime.linkLabel

"<Name +N>", or "<Name>" at +0; the codename stands in for a reference
without a name.
================
*/
func (rt *Runtime) linkLabel(row domain.InventoryRow) string {
	name := row.Codename
	if items := rt.deps.ItemReferences(); items != nil {
		if ref, found := items.ItemRefByCodename(row.Codename); found && ref.Name != "" {
			name = ref.Name
		}
	}
	if row.Plus > 0 {
		return fmt.Sprintf("<%s +%d>", name, row.Plus)
	}
	return "<" + name + ">"
}
