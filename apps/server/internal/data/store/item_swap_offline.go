/*
===========================================================================

item_swap_offline.go - an operator's offline item swap, named characters or a whole shard

An operator-only maintenance door, like RenameCharacterOffline: the caller
opens the stopped authority exclusively (store.Open's writer lock refuses a
running GameWorld) and closes it afterwards. It rewrites the rows a swap
function picks in every container the named characters reach: the bag and
equipped sockets, worn avatars, every companion's container (active,
dormant scroll records, mercenaries), the account's storage and the
character's guild storage. One transaction commits every change, so a
failure leaves the store as it was. A row the swap refuses fails the whole
run before anything is written.

SwapItemsOfflineShard covers the whole shard instead: every character of
the division (one pending deletion too, since it can be restored), every
stored account warehouse (accounts without characters too), every
guild's storage, and the division's ground plane: each ground item and a
dropped pet's container (ground_items is restored at boot with its time
to live, so an item left there would come back unswapped). A named run
leaves the ground alone, since no character owns a ground item. A stall
is never stored, and deleted_characters is a raw archive nothing
restores, so neither is a container.

It exists for owner decisions, not a native mechanic: the balance
advisor's own characters trade their degree-9 Seal of Sun gear for the
matching Moon (2026-10-14, cmd/operations/sro-swap-sun), and every plain
elixir becomes the Intensifying one the game now hands out (2026-11-16,
cmd/operations/sro-swap-elixir).

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"math"
	"sort"

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
swapScope

The containers one run covers. Each storage carries the label its rows are
reported under: a named run reports them under the character that reaches
them, a shard run under the account or guild.
================
*/
type swapScope struct {
	characters []*domain.Character
	accounts   []swapStorage
	guilds     []swapStorage
	// ground covers the division's ground plane (a shard run).
	ground bool
}

type swapStorage struct {
	label   string
	account string
	guild   int64
}

/*
================
SwapItemsOffline

Previews (apply false) or commits the swap over the named characters of
division, their account storage and their guild's storage. The result
lists every changed row in container order.
================
*/
func (s *Store) SwapItemsOffline(division string, names []string, swap ItemSwapFunc, apply bool) ([]ItemSwap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var scope swapScope
	accounts := map[string]bool{}
	guilds := map[int64]bool{}
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
		scope.characters = append(scope.characters, selected)
		if selected.AccountID != "" && !accounts[selected.AccountID] {
			accounts[selected.AccountID] = true
			scope.accounts = append(scope.accounts, swapStorage{label: name, account: selected.AccountID})
		}
		if selected.GuildID != nil && !guilds[*selected.GuildID] {
			guilds[*selected.GuildID] = true
			scope.guilds = append(scope.guilds, swapStorage{label: name, guild: *selected.GuildID})
		}
	}
	return s.swapItemsOfflineLocked(division, scope, swap, apply)
}

/*
================
SwapItemsOfflineShard

The same swap over the whole shard: every character of division in name
order, every stored account warehouse in account order, and every guild's
storage in guild order.
================
*/
func (s *Store) SwapItemsOfflineShard(division string, swap ItemSwapFunc, apply bool) ([]ItemSwap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, fmt.Errorf("item swap requires a clean offline authority")
	}
	var scope swapScope
	scope.characters = append(scope.characters, s.characters[division]...)
	sort.SliceStable(scope.characters, func(i, j int) bool { return scope.characters[i].Name < scope.characters[j].Name })
	accounts, err := storedAccounts(s.syncDBLocked())
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		scope.accounts = append(scope.accounts, swapStorage{label: "account " + account, account: account})
	}
	for id := range s.guilds[division] {
		scope.guilds = append(scope.guilds, swapStorage{label: fmt.Sprintf("guild %d", id), guild: id})
	}
	sort.Slice(scope.guilds, func(i, j int) bool { return scope.guilds[i].guild < scope.guilds[j].guild })
	scope.ground = true
	return s.swapItemsOfflineLocked(division, scope, swap, apply)
}

/*
================
storedAccounts

Every account that has a warehouse row, in account order.
================
*/
func storedAccounts(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT account_id FROM account_storage ORDER BY account_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []string
	for rows.Next() {
		var account string
		if err := rows.Scan(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

/*
================
cloneStorageRows

A storage whose rows (and their companions) can change without touching
the store's copy.
================
*/
func cloneStorageRows(storage domain.AccountStorage) domain.AccountStorage {
	storage.Rows = append([]domain.InventoryRow(nil), storage.Rows...)
	for i := range storage.Rows {
		storage.Rows[i].Summon = domain.CloneCOS(storage.Rows[i].Summon)
	}
	return storage
}

/*
================
groundRow

A ground item as the inventory row a swap function reads. Its dropped
pet is a clone, so a swap that walks it never touches the store's copy.
================
*/
func groundRow(item domain.GroundItemRecord) domain.InventoryRow {
	return domain.InventoryRow{
		TradeOwner:   item.TradeOwner,
		Summon:       domain.CloneCOS(item.Summon),
		RecordID:     item.RecordID,
		RefObjID:     item.RefObjID,
		Codename:     item.Codename,
		TypeFlags:    item.TypeFlags,
		Plus:         int64(item.Plus),
		VarianceBits: item.VarianceBits,
		Durability:   int64(item.Durability),
		StackCount:   int64(item.StackCount),
		MagicOptions: item.MagicOptions,
		Data:         item.Data,
		ForgeBound:   item.ForgeBound,
	}
}

/*
================
applyGroundRow

Writes a swapped row back onto its ground item. The ground record keeps
narrower fields than a row, so a value that does not fit refuses the
run instead of being truncated.
================
*/
func applyGroundRow(item *domain.GroundItemRecord, row domain.InventoryRow) error {
	if row.Plus < 0 || row.Plus > math.MaxUint8 {
		return fmt.Errorf("plus %d does not fit a ground item", row.Plus)
	}
	if row.Durability < 0 || row.Durability > math.MaxUint32 {
		return fmt.Errorf("durability %d does not fit a ground item", row.Durability)
	}
	if row.StackCount < 0 || row.StackCount > math.MaxUint16 {
		return fmt.Errorf("stack %d does not fit a ground item", row.StackCount)
	}
	item.TradeOwner = row.TradeOwner
	item.Summon = row.Summon
	item.RecordID = row.RecordID
	item.RefObjID = row.RefObjID
	item.Codename = row.Codename
	item.TypeFlags = row.TypeFlags
	item.Plus = uint8(row.Plus)
	item.VarianceBits = row.VarianceBits
	item.Durability = uint32(row.Durability)
	item.StackCount = uint16(row.StackCount)
	item.MagicOptions = row.MagicOptions
	item.Data = row.Data
	item.ForgeBound = row.ForgeBound
	return nil
}

/*
================
swapGround

Runs swap over division's ground plane: each item and its dropped pet's
container. Returns the whole plane, every division, with division's
rows rewritten, and whether any row changed; the store's plane is never
touched.
================
*/
func (s *Store) swapGround(division string, swap ItemSwapFunc, out *[]ItemSwap) (map[string][]domain.GroundItemRecord, bool, error) {
	plane := make(map[string][]domain.GroundItemRecord, len(s.ground.loadedRecords))
	for id, rows := range s.ground.loadedRecords {
		plane[id] = rows
	}
	items := append([]domain.GroundItemRecord(nil), plane[division]...)
	changed := false
	for i := range items {
		before := len(*out)
		rows := []domain.InventoryRow{groundRow(items[i])}
		if err := swapRows("ground", fmt.Sprintf("gid %d", items[i].Gid), rows, swap, out); err != nil {
			return nil, false, err
		}
		if len(*out) == before {
			continue
		}
		if err := applyGroundRow(&items[i], rows[0]); err != nil {
			return nil, false, fmt.Errorf("ground gid %d (%s): %w", items[i].Gid, items[i].Codename, err)
		}
		changed = true
	}
	plane[division] = items
	return plane, changed, nil
}

/*
================
swapItemsOfflineLocked

Runs swap over scope, then, with apply, commits every changed character,
storage and guild in one transaction. A refused row fails the run before
anything is written.
================
*/
func (s *Store) swapItemsOfflineLocked(division string, scope swapScope, swap ItemSwapFunc, apply bool) ([]ItemSwap, error) {
	if s.ground.source != nil || !s.changes.empty() || s.db == nil {
		return nil, fmt.Errorf("item swap requires a clean offline authority")
	}
	var out []ItemSwap
	characters := map[*domain.Character]*domain.Character{}
	storages := map[string]domain.AccountStorage{}
	guilds := map[int64]domain.GuildRecord{}
	for _, selected := range scope.characters {
		name := selected.Name
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
		// Dormant records sit in their summoner rows, which swapRows walks
		// above (and in storage below). The active pet and mercenaries are
		// held outside any row; an active record also seated in its row is
		// already swapped there, so it is not counted again. Captured pets
		// and broods are never stored, so an offline run has none.
		for _, pet := range append([]*domain.CharacterCOS{next.ActiveCOS}, next.Mercenaries...) {
			if pet == nil || pet.Container == nil {
				continue
			}
			if err := swapRows(name, fmt.Sprintf("companion %d", pet.RefObjID), pet.Container.Rows, swap, &out); err != nil {
				return nil, err
			}
		}
		// The survival arena's rolled chest (domain.ArenaChest) keeps its
		// items on the character until they are claimed, across a full bag
		// and a reconnect, so a pending chest can hold a plain elixir. The
		// chest is copied before the swap so the snapshot never shares rows
		// with the live record.
		if next.SookChest != nil {
			chest := *next.SookChest
			chest.Items = append([]domain.InventoryRow(nil), chest.Items...)
			next.SookChest = &chest
			if err := swapRows(name, "sook chest", chest.Items, swap, &out); err != nil {
				return nil, err
			}
		}
		if len(out) > before {
			characters[selected] = next
		}
	}
	for _, owner := range scope.accounts {
		stored, err := readAccountStorage(s.syncDBLocked(), owner.account)
		if err != nil {
			return nil, err
		}
		storage := cloneStorageRows(stored)
		before := len(out)
		if err := swapRows(owner.label, "storage", storage.Rows, swap, &out); err != nil {
			return nil, err
		}
		if len(out) > before {
			storages[owner.account] = storage
		}
	}
	for _, owner := range scope.guilds {
		guild, exists := s.guilds[division][owner.guild]
		if !exists || guild.Storage == nil {
			continue
		}
		storage := cloneStorageRows(*guild.Storage)
		before := len(out)
		if err := swapRows(owner.label, fmt.Sprintf("guild %d storage", owner.guild), storage.Rows, swap, &out); err != nil {
			return nil, err
		}
		if len(out) > before {
			guild.Storage = &storage
			guilds[owner.guild] = guild
		}
	}
	var ground map[string][]domain.GroundItemRecord
	groundChanged := false
	if scope.ground {
		var err error
		if ground, groundChanged, err = s.swapGround(division, swap, &out); err != nil {
			return nil, err
		}
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
		if err := replaceGuildTx(tx.Tx, division, id, guild, s.guildMembers[division][id], &s.residues); err != nil {
			return nil, err
		}
	}
	if groundChanged {
		if err := replaceGroundTx(tx.Tx, &s.encodeScratch, ground, s.residues.ground); err != nil {
			return nil, err
		}
	}
	if err := upsertMetaTx(tx.Tx, metaKeyUpdatedAtMs, fmt.Sprint(s.now().UnixMilli())); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if groundChanged {
		// As the offline rename: the plane was rewritten outside the
		// registry's change stamps, so the next ground write diffs the
		// whole plane against it (#2040).
		s.ground.loadedRecords = ground
		s.ground.revisionRecorded = false
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
