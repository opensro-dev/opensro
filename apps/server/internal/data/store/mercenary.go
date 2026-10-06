/*
===========================================================================

mercenary.go - atomic guild admission and soldier-scroll consumption

Guild-master transfer, membership changes and scroll use share the store
lock. A cached guild snapshot cannot authorize a former master's summon.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"math/bits"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

/*
================
UpdateMercenaryOwner

The callback owns only this character. Guild/union state is a detached read;
no store door may be re-entered from the callback.
================
*/
func (door storeGuildDoor) UpdateMercenaryOwner(division string, c *domain.Character, update func(domain.MercenaryContext) bool) bool {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if c == nil || update == nil || s.charDivision[c] != division || c.DeletePending {
		return false
	}
	var context domain.MercenaryContext
	id, guild, members, index, actor, refusal := door.authorizedGuildActorLocked(division, c.ID, domain.GuildAuthorization{})
	if !refusal.Refused() && actor == c {
		context.Guild, context.Master = guild, members[index].Grade == 0
		if s.db == nil {
			return false
		}
		var raw string
		err := s.db.QueryRow("SELECT record FROM alliances WHERE division = ? AND alliance_id = ?", division, id).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithError(err).Error("mercenary union lookup failed")
			return false
		}
		if err == nil {
			alliance, err := decodeAllianceRecord(id, raw)
			if err != nil {
				log.WithError(err).Error("mercenary union record is invalid")
				return false
			}
			context.UnionMaster = alliance.Guilds[0] == id
		}
	}
	if !update(context) {
		return false
	}
	s.changes.characters[c] = true
	s.commitLocked("mercenary-summon")
	return true
}

/*
================
PurchaseMercenaryAttribute

5C7900 checks guild, level, master, gold and GP in that order, before testing
the current flags. One store commit owns the guild flag and both debits.
The returned byte is the native guild notice reason (zero means committed).
================
*/
func (door storeGuildDoor) PurchaseMercenaryAttribute(division string, actorID int64, attribute uint8) (domain.GuildSnapshot, uint8) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty domain.GuildSnapshot
	id, guild, members, index, actor, refusal := door.authorizedGuildActorLocked(division, actorID, domain.GuildAuthorization{})
	if refusal.Refused() {
		return empty, 0x0d
	}
	if guild.Level < 4 {
		return empty, 0x52
	}
	if members[index].Grade != 0 {
		return empty, 0x1e
	}
	gold, gp := domain.MercenaryAttributePrice(attribute)
	if actor.Gold == nil || *actor.Gold < gold {
		return empty, 0x0c
	}
	if guild.GP < gp {
		return empty, 0x32
	}
	if guild.Byte10 == attribute {
		return empty, 2
	}
	if attribute != 0 {
		if guild.Byte10&attribute != 0 {
			return empty, 0x54
		}
		if bits.OnesCount8(guild.Byte10&0x1f) >= 2 {
			return empty, 2
		}
		guild.Byte10 |= attribute
	} else {
		guild.Byte10 = 0
	}
	remaining := *actor.Gold - gold
	actor.Gold = &remaining
	guild.GP -= gp
	s.guilds[division][id] = guild
	s.changes.guilds[guildKey{division: division, guildID: id}] = true
	s.changes.characters[actor] = true
	// 5C99A0 refreshes only the requester; 4FDC70 skips dead/unspawned actors.
	for _, pet := range actor.Mercenaries {
		if pet != nil && pet.Summoned && pet.CurrentHP > 0 {
			pet.MercenaryAttributes = guild.Byte10
		}
	}
	s.commitLocked("mercenary-attribute")
	return domain.GuildSnapshot{Guild: guild, Members: members}, 0
}
