/*
===========================================================================

mercenary_attribute.go - guild-manager soldier attribute requests and receipts

The guild store owns purchases. Existing guild metadata carries the flags
through entry; 3B29/5/40 and B322 apply the native add-or-reset delta.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
)

const (
	opMercenaryAttribute      uint16 = 0x7322
	opMercenaryAttributeReply uint16 = 0xb322
)

/*
================
HandleMercenaryAttribute

v1.150 5DB1B8 writes [npc DWORD][attribute BYTE]; v1.188 517D60 validates
range and NPC service 15 before the guild transaction.
================
*/
func (rt *Runtime) HandleMercenaryAttribute(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	npc, err := r.U32()
	attribute, byteErr := r.U8()
	if c == nil || err != nil || byteErr != nil || r.Done() != nil {
		return guildNpcAnswer(opMercenaryAttributeReply, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	store, ok := rt.deps.GuildAuthority().(domain.MercenaryStore)
	if !ok || !rt.guildManagerNpc(division, c, npc) {
		return guildNpcAnswer(opMercenaryAttributeReply, guildNpcRefused)
	}
	snapshot, code := store.PurchaseMercenaryAttribute(division, c.ID, attribute)
	if code != 0 {
		return guildNpcAnswer(opMercenaryAttributeReply, code)
	}
	push := wire.Frame{Opcode: guild.OpGuildUpdatePush,
		Payload: wire.NewWriter(7).U8(5).U8(0x48).U32(snapshot.Guild.GP).U8(attribute).Payload()}
	for _, member := range snapshot.Members {
		if member.CharID != c.ID && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, member.Name, []wire.Frame{push})
		}
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opMercenaryAttributeReply, Payload: []byte{1, attribute}}, push, goldFrame(c)}}
}
