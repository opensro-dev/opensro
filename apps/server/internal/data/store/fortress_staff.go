/*
===========================================================================

fortress_staff.go - atomic fortress employment and payment

632100 tests gold before guild points. Database completion 623DF9 sets the
staff flags and charges 3000 GP and 30000 gold before acknowledging.

===========================================================================
*/
package store

import (
	"encoding/json"
	"opensro.online/server/internal/domain"
)

/*
================
HireFortressStaff

Write copies first. Failed commits leave all three in-memory owners intact.
The caller holds the fortress authority lock throughout this transaction.
================
*/
func (door storeFortressDoor) HireFortressStaff(division string, record domain.FortressRecord, actorID int64, requested uint8) (code uint8, err error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.recordWriteFailureLocked("fortress-staff", err)
		}
	}()
	if s.db == nil {
		return 2, errFortressUnavailable
	}
	guildID, guild, members, _, actor, refusal := (storeGuildDoor{s: s}).authorizedGuildActorLocked(
		division, actorID, domain.GuildAuthorization{})
	holder := record.GuildID
	if record.TempGuildID != 0 {
		holder = record.TempGuildID
	}
	if refusal.Refused() || holder == 0 || guildID != holder {
		return 6, nil
	}
	master := false
	for _, member := range members {
		if member.CharID == actorID {
			master = member.Grade == 0
		}
	}
	if !master {
		return 7, nil
	}
	if record.StaffFlags&requested != 0 {
		return 9, nil
	}
	record.StaffFlags |= requested
	if actor.Gold == nil || *actor.Gold < domain.FortressStaffGold {
		return 0x0f, nil
	}
	if int32(guild.GP) < int32(domain.FortressStaffGP) {
		return 0x0e, nil
	}
	next := *actor
	gold := *actor.Gold - domain.FortressStaffGold
	next.Gold = &gold
	guild.GP -= domain.FortressStaffGP
	raw, err := json.Marshal(record)
	if err != nil {
		return 2, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 2, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = upsertCharacterTx(tx, division, &next); err != nil {
		return 2, err
	}
	if err = replaceGuildTx(tx, division, guildID, guild, s.guildMembers[division][guildID]); err != nil {
		return 2, err
	}
	if _, err = tx.Exec("INSERT INTO fortresses (division, fortress_id, record) VALUES (?, ?, ?) ON CONFLICT(division, fortress_id) DO UPDATE SET record = excluded.record", division, record.FortressID, string(raw)); err != nil {
		return 2, err
	}
	if err = tx.Commit(); err != nil {
		return 2, err
	}
	actor.Gold = &gold
	s.guilds[division][guildID] = guild
	s.recordWriteSuccessLocked()
	return 0, nil
}
