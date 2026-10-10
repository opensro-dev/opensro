/*
===========================================================================

fortress_item_forge.go - the fortress production rows and their commits

_SiegeFortressItemForge keeps one row per order (layout 8). Starting an
order charges the actor's gold and the holder guild's points with the
row (CSiegeFortress_HandleDatabaseResult case 0xC debits the guild points
before acknowledging); collecting writes the new bag row with the order's
remaining count (_SiegeFortressCollectForgedItem). Cancelling refunds
nothing (QUERY_SIEGE_ITEM_FORGE_REMOVE, 621D40).

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/domain"
)

// fortressItemForgeSchema is layout 8's addition (authority_upgrade.go adds
// it to a layout 5..7 authority).
const fortressItemForgeSchema = `
CREATE TABLE IF NOT EXISTS fortress_item_forges (
  division    TEXT    NOT NULL,
  fortress_id INTEGER NOT NULL,
  item_ref_id INTEGER NOT NULL,
  record      TEXT    NOT NULL,
  PRIMARY KEY (division, fortress_id, item_ref_id)
);
`

/*
================
decodeFortressItemForge

A row's JSON must name the row's own keys and a collectable order.
================
*/
func decodeFortressItemForge(fortressID, itemRefID uint32, raw string) (domain.FortressItemForgeRecord, error) {
	var forge domain.FortressItemForgeRecord
	if err := decodeJSONStrict([]byte(raw), &forge); err != nil {
		return forge, fmt.Errorf("fortress %d item forge %d: %w", fortressID, itemRefID, err)
	}
	if forge.FortressID != fortressID || forge.ItemRefID != itemRefID || forge.Count == 0 ||
		forge.StartedAtMs < 0 || forge.EndsAtMs < forge.StartedAtMs {
		return forge, fmt.Errorf("fortress %d item forge %d: inconsistent record", fortressID, itemRefID)
	}
	return forge, nil
}

/*
================
validateFortressItemForges

Startup validates without creating the table or repairing a row.
================
*/
func validateFortressItemForges(db *sql.DB) error {
	rows, err := db.Query("SELECT fortress_id, item_ref_id, record FROM fortress_item_forges")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var fortressID, itemRefID uint32
		var raw string
		if err := rows.Scan(&fortressID, &itemRefID, &raw); err != nil {
			return err
		}
		if _, err := decodeFortressItemForge(fortressID, itemRefID, raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

/*
================
FortressItemForges
================
*/
func (door storeFortressDoor) FortressItemForges(divisionID string) ([]domain.FortressItemForgeRecord, error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errFortressUnavailable
	}
	rows, err := s.db.Query("SELECT fortress_id, item_ref_id, record FROM fortress_item_forges WHERE division = ? ORDER BY fortress_id, item_ref_id", divisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FortressItemForgeRecord
	for rows.Next() {
		var fortressID, itemRefID uint32
		var raw string
		if err := rows.Scan(&fortressID, &itemRefID, &raw); err != nil {
			return nil, err
		}
		forge, err := decodeFortressItemForge(fortressID, itemRefID, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, forge)
	}
	return out, rows.Err()
}

/*
================
saveFortressItemForgeTx
================
*/
func saveFortressItemForgeTx(tx *sql.Tx, division string, forge domain.FortressItemForgeRecord, present bool) error {
	if !present {
		_, err := tx.Exec("DELETE FROM fortress_item_forges WHERE division = ? AND fortress_id = ? AND item_ref_id = ?",
			division, forge.FortressID, forge.ItemRefID)
		return err
	}
	raw, err := json.Marshal(forge)
	if err != nil {
		return err
	}
	if _, err := decodeFortressItemForge(forge.FortressID, forge.ItemRefID, string(raw)); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO fortress_item_forges (division, fortress_id, item_ref_id, record) VALUES (?, ?, ?, ?) ON CONFLICT(division, fortress_id, item_ref_id) DO UPDATE SET record = excluded.record",
		division, forge.FortressID, forge.ItemRefID, string(raw))
	return err
}

/*
================
SaveFortressItemForge
================
*/
func (door storeFortressDoor) SaveFortressItemForge(divisionID string, forge domain.FortressItemForgeRecord, present bool) (err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("fortress-item-forge", err)
		}
	}()
	if s.db == nil || divisionID == "" || forge.FortressID == 0 || forge.ItemRefID == 0 {
		return errFortressUnavailable
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = saveFortressItemForgeTx(tx, divisionID, forge, present); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.recordWriteSuccessLocked()
	return nil
}

/*
================
fortressProducerLocked

The holder-guild and authority checks every production action repeats
(6327xx: 0x2806, then 0x2817): the actor belongs to the holder and is its
master or carries the staff's fortress role.
================
*/
func (door storeFortressDoor) fortressProducerLocked(division string, holder, actorID int64, role uint8) (int64, domain.GuildRecord, *domain.Character, uint8) {
	guildID, guild, members, _, actor, refusal := storeGuildDoor(door).authorizedGuildActorLocked(
		division, actorID, domain.GuildAuthorization{})
	if refusal.Refused() || holder == 0 || guildID != holder {
		return 0, guild, nil, domain.FortressForgeErrOwner
	}
	for _, member := range members {
		if member.CharID == actorID && (member.Grade == 0 || member.FortressRole&role != 0) {
			return guildID, guild, actor, 0
		}
	}
	return 0, guild, nil, domain.FortressForgeErrRole
}

/*
================
StartFortressItemForge

Gold before guild points, as 632660 tests them. Write copies first, so a
failed commit leaves the actor and the guild as they were.
================
*/
func (door storeFortressDoor) StartFortressItemForge(division string, start domain.FortressItemForgeStart) (code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("fortress-item-forge", err)
		}
	}()
	if s.db == nil {
		return domain.FortressForgeErrFailure, errFortressUnavailable
	}
	guildID, guild, actor, refusal := door.fortressProducerLocked(division, start.Holder, start.ActorID, start.Role)
	if refusal != 0 {
		return refusal, nil
	}
	// An absent balance is zero gold.
	gold := int64(0)
	if actor.Gold != nil {
		gold = *actor.Gold
	}
	if gold < start.Gold {
		return domain.FortressForgeErrGold, nil
	}
	if int64(guild.GP) < int64(start.GP) {
		return domain.FortressForgeErrGP, nil
	}
	next := *actor
	gold -= start.Gold
	next.Gold = &gold
	guild.GP -= start.GP
	tx, err := s.db.Begin()
	if err != nil {
		return domain.FortressForgeErrFailure, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = upsertCharacterTx(tx, division, &next); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	if err = replaceGuildTx(tx, division, guildID, guild, s.guildMembers[division][guildID]); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	if err = saveFortressItemForgeTx(tx, division, start.Forge, true); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	if err = tx.Commit(); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	actor.Gold = &gold
	s.guilds[division][guildID] = guild
	s.recordWriteSuccessLocked()
	return 0, nil
}

/*
================
CollectFortressItemForge

The bag row lands in a slot the actor's bag still has free; the order's
remaining count is written with it, or the row removed at zero.
================
*/
func (door storeFortressDoor) CollectFortressItemForge(division string, collect domain.FortressItemForgeCollect) (code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("fortress-item-forge", err)
		}
	}()
	if s.db == nil {
		return domain.FortressForgeErrFailure, errFortressUnavailable
	}
	_, _, actor, refusal := door.fortressProducerLocked(division, collect.Holder, collect.ActorID, collect.Role)
	if refusal != 0 {
		return refusal, nil
	}
	for _, row := range actor.MissionInventory {
		if row.Slot == collect.Item.Slot {
			return domain.FortressForgeErrBag, nil
		}
	}
	next := *actor
	next.MissionInventory = append(append([]domain.InventoryRow(nil), actor.MissionInventory...), collect.Item)
	tx, err := s.db.Begin()
	if err != nil {
		return domain.FortressForgeErrFailure, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = upsertCharacterTx(tx, division, &next); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	if err = saveFortressItemForgeTx(tx, division, collect.Forge, collect.Forge.Count != 0); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	if err = tx.Commit(); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	actor.MissionInventory = next.MissionInventory
	s.recordWriteSuccessLocked()
	return 0, nil
}
