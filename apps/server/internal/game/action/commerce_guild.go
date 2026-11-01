/*
===========================================================================

commerce_guild.go - guild-point currency admission and debit for NPC shops

Guild shop goods (STORE_*_GUILD_TAB1/3) carry both a gold row (payment 1)
and a guild-point row (payment 8) in refpricepolicyofitem.txt.
Natively the purchase payment check makes sure the buyer belongs to a guild
and is its master (else 0x18C4 -> UIIT_MSG_GUILD_STORE_BUY_LIMIT) and that the
guild holds at least the quoted GP cost (guild+0x3C, else 0x18C5 ->
UIIT_MSG_GUILD_LACK_GP). The payment commit then debits the guild's GP
through the guild manager and broadcasts the new
GP total on 0x3E74 subOp 5 (&0x08).

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
)

/*
================
guildStore

Resolves the guild store from the runtime's dependencies or direct field.
================
*/
func (rt *Runtime) guildStore() enterworld.GuildStore {
	if rt == nil {
		return nil
	}
	if rt.deps != nil && rt.deps.GuildAuthority() != nil {
		return rt.deps.GuildAuthority()
	}
	return rt.Guilds
}

/*
================
guildShopAdmission

Checks whether the buyer is a guild master (as the native purchase payment
check does) and returns the guild's current GP balance. Called outside
SettleTrade so store.mu is never re-entered.
================
*/
func (rt *Runtime) guildShopAdmission(division string, c *enterworld.Character) (bool, uint64) {
	if rt == nil || c == nil {
		return false, 0
	}
	store := rt.guildStore()
	if store == nil {
		return false, 0
	}
	guildID, ok := store.GuildOfCharacter(division, c.ID)
	if !ok && c.GuildID != nil && *c.GuildID != 0 {
		guildID = *c.GuildID
		ok = true
	}
	if !ok || guildID == 0 {
		return false, 0
	}
	record, members, ok := store.Guild(division, guildID)
	if !ok {
		return false, 0
	}
	for _, member := range members {
		if member.CharID == c.ID {
			if member.Grade != domain.GuildLeaderGrade {
				return false, 0
			}
			return true, uint64(record.GP)
		}
	}
	return false, 0
}

/*
================
spendGuildPoints

Debits cost guild points from the buyer's guild after SettleTrade commits
(as the native payment commit does) and
broadcasts the updated guild GP (0x3E74 subOp 5 &0x08) to online guild
members and the buyer.
================
*/
func (rt *Runtime) spendGuildPoints(division string, c *enterworld.Character, cost uint32, out OpResult) OpResult {
	if rt == nil || c == nil || cost == 0 {
		return out
	}
	store := rt.guildStore()
	if store == nil {
		return out
	}
	snapshot, refusal := store.UpdateGuildAs(
		division,
		c.ID,
		"guild-shop-buy",
		domain.GuildAuthorization{LeaderOnly: true},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			if g.GP < cost {
				return g, m, false
			}
			g.GP -= cost
			return g, m, true
		},
	)
	if refusal.Refused() {
		return out
	}
	update := wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeGuildGp3B29(snapshot.Guild.GP)}
	rt.pushGuildMembers(division, snapshot.Guild.ID, snapshot.Members, c.ID, []wire.Frame{update})
	out.Frames = append(out.Frames, update)
	return out
}
