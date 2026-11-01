/*
===========================================================================

item_swap_offline.go - an operator's offline item swap for named characters

An operator-only maintenance door, like RenameCharacterOffline: the caller
opens the stopped authority exclusively (store.Open's writer lock refuses a
running GameWorld) and closes it afterwards. It rewrites the rows a swap
function picks in every container the named characters reach: the bag and
equipped sockets, worn avatars, every companion's container (active,
dormant scroll records, mercenaries), the account's storage and the
character's guild storage (items on the ground or in a stall are not
covered). One transaction commits every change, so a
failure leaves the store as it was. A row the swap refuses fails the whole
run before anything is written.

It exists for one owner decision (2026-10-14): the balance advisor's own
characters trade their degree-9 Seal of Sun gear for the matching Moon
(cmd/operations/sro-swap-sun). It is not a native mechanic.

===========================================================================
*/
package store

import (
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
ItemSwap

One row a swap changes: where it sits and what it becomes.
================
*/
type ItemSwap struct {
	Character string
	Container string
	Slot      int64
	From, To  string
}

/*
================
ItemSwapFunc

Returns the row to write and whether it changed; an error refuses the run.
================
*/
type ItemSwapFunc func(row domain.InventoryRow) (domain.InventoryRow, bool, error)

/*
================
swapRows

Applies swap to rows in place, recording each change under container.
================
*/
func swapRows(character, container string, rows []domain.InventoryRow, swap ItemSwapFunc, out *[]ItemSwap) error {
	for i := range rows {
		next, changed, err := swap(rows[i])
		if err != nil {
			return fmt.Errorf("%s %s slot %d (%s): %w", character, container, rows[i].Slot, rows[i].Codename, err)
		}
		if changed {
			*out = append(*out, ItemSwap{Character: character, Container: container, Slot: rows[i].Slot, From: rows[i].Codename, To: next.Codename})
			rows[i] = next
		}
		if pet := rows[i].Summon; pet != nil && pet.Container != nil {
			label := fmt.Sprintf("%s/companion %d", container, pet.RefObjID)
			if err := swapRows(character, label, pet.Container.Rows, swap, out); err != nil {
				return err
			}
		}
	}
	return nil
}

/*
================
SwapItemsOffline

Previews (apply false) or commits the swap over the named characters of
division. The result lists every changed row in container order.
================
*/
func (s *Store) SwapItemsOffline(division string, names []string, swap ItemSwapFunc, apply bool) ([]ItemSwap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ground.source != nil || !s.changes.empty() || s.db == nil {
		return nil, fmt.Errorf("item swap requires a clean offline authority")
	}
	var out []ItemSwap
	characters := map[*domain.Character]*domain.Character{}
	storages := map[string]domain.AccountStorage{}
	guilds := map[int64]domain.GuildRecord{}
	for _, name := range names {
		var selected *domain.Character
		for _, c := range s.characters[division] {
			if c.Name == name && !c.DeletePending {
				selected = c
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("active character %q not found in %s", name, division)
		}
		next := selected.Snapshot()
		before := len(out)
		// Equipped sockets and the bag share one row list (slot 0-12 worn).
		if err := swapRows(name, "inventory", next.MissionInventory, swap, &out); err != nil {
			return nil, err
		}
		if next.AvatarInventory != nil {
			if err := swapRows(name, "avatar", next.AvatarInventory.Rows, swap, &out); err != nil {
				return nil, err
			}
		}
		for _, pet := range append([]*domain.CharacterCOS{next.ActiveCOS}, next.Mercenaries...) {
			if pet == nil || pet.Container == nil {
				continue
			}
			if err := swapRows(name, fmt.Sprintf("companion %d", pet.RefObjID), pet.Container.Rows, swap, &out); err != nil {
				return nil, err
			}
		}
		if len(out) > before {
			characters[selected] = next
		}
		if _, seen := storages[selected.AccountID]; !seen && selected.AccountID != "" {
			storage, err := readAccountStorage(s.db, selected.AccountID)
			if err != nil {
				return nil, err
			}
			storage.Rows = append([]domain.InventoryRow(nil), storage.Rows...)
			for i := range storage.Rows {
				storage.Rows[i].Summon = domain.CloneCOS(storage.Rows[i].Summon)
			}
			if err := swapRows(name, "storage", storage.Rows, swap, &out); err != nil {
				return nil, err
			}
			storages[selected.AccountID] = storage
		}
		if selected.GuildID == nil {
			continue
		}
		id := *selected.GuildID
		if _, seen := guilds[id]; seen {
			continue
		}
		guild, exists := s.guilds[division][id]
		if !exists || guild.Storage == nil {
			continue
		}
		storage := *guild.Storage
		storage.Rows = append([]domain.InventoryRow(nil), storage.Rows...)
		for i := range storage.Rows {
			storage.Rows[i].Summon = domain.CloneCOS(storage.Rows[i].Summon)
		}
		if err := swapRows(name, fmt.Sprintf("guild %d storage", id), storage.Rows, swap, &out); err != nil {
			return nil, err
		}
		guild.Storage = &storage
		guilds[id] = guild
	}
	if !apply || len(out) == 0 {
		return out, nil
	}
	if s.commitFail != nil {
		return nil, s.commitFail
	}
	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, next := range characters {
		if err := tx.upsertCharacter(division, next); err != nil {
			return nil, err
		}
	}
	for account, storage := range storages {
		if err := validateAccountStorage(storage); err != nil {
			return nil, fmt.Errorf("storage %s: %w", account, err)
		}
		if err := writeAccountStorageTx(tx.Tx, account, storage); err != nil {
			return nil, err
		}
	}
	for id, guild := range guilds {
		if err := replaceGuildTx(tx.Tx, division, id, guild, s.guildMembers[division][id]); err != nil {
			return nil, err
		}
	}
	if err := upsertMetaTx(tx.Tx, metaKeyUpdatedAtMs, fmt.Sprint(s.now().UnixMilli())); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for c, next := range characters {
		c.MissionInventory = next.MissionInventory
		c.AvatarInventory = next.AvatarInventory
		c.ActiveCOS = next.ActiveCOS
		c.Mercenaries = next.Mercenaries
	}
	for id, guild := range guilds {
		s.guilds[division][id] = guild
	}
	s.recordWriteSuccessLocked()
	return out, nil
}
