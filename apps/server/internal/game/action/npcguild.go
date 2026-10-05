/*
===========================================================================

npcguild.go - the guild manager NPC's guild services

A guild manager holds service 0xF (CGObjNPC_SpawnAndConfigureServices
4C6350), and its talk rows reach the guild through requests that name the
NPC first. v1.188 opens each with CGObjPC_CheckNpcFunctionTargetInRange
and the 0xF test (answer 3 for either); the guild rule follows:

	0x73F0 [u32 npc]               level up      -> 0xB3F0   (v1.188 0x70FA)
	0x77D4 [u32 npc][u32 member]   master leave  -> 0xB7D4   (v1.188 0x7103)
	0x7140 [u32 npc]               compensation  -> 0xB140   (v1.188 0x7114)
	0x73F7 [u32 npc]               claim it      -> 0xB3F7   (v1.188 0x7113)

each answering [1] or [2][code] in notice category 0x10.

The level-up window (CIFGuildLevelUp 5EF9A0) prices the next level; the
master pays its gold, the guild its GP (domain.GuildLevelUpCostAt), and
every member online learns the new level and GP from 0x3B29 subOp 5.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opGuildLevelUpRequest   uint16 = 0x73f0
	opGuildLevelUpResponse  uint16 = 0xb3f0
	opGuildMasterLeave      uint16 = 0x77d4
	opGuildMasterLeaveDone  uint16 = 0xb7d4
	opGuildCompensation     uint16 = 0x7140
	opGuildCompensationDue  uint16 = 0xb140
	opGuildCompensationPay  uint16 = 0x73f7
	opGuildCompensationPaid uint16 = 0xb3f7

	// guildNpcRefused is the answer to a request whose NPC is missing,
	// out of range or without the guild service.
	guildNpcRefused uint8 = 0x03
)

/*
================
guildNpcAnswer
================
*/
func guildNpcAnswer(opcode uint16, code uint8) OpResult {
	if code == 0 {
		return OpResult{Frames: []wire.Frame{{Opcode: opcode, Payload: []byte{1}}}}
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opcode, Payload: []byte{2, code}}}}
}

/*
================
guildManagerNpc

CGObjPC_CheckNpcFunctionTargetInRange and the 0xF service test: the
selected NPC, in range, keeps a guild. The caller holds the division lock.
================
*/
func (rt *Runtime) guildManagerNpc(division string, c *enterworld.Character, gid uint32) bool {
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return false
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	return ok && rt.npcWithinHitRange(division, c, npc) && npc.Services.Has(simulation.NpcServiceGuild)
}

/*
================
guildRefusalCode

The category 0x10 notice of a guild door refusal.
================
*/
func guildRefusalCode(refusal domain.GuildRefusal) uint8 {
	switch refusal {
	case domain.GuildRefusalNotMember:
		return guild.GuildErrNotMember
	case domain.GuildRefusalLeaderRequired, domain.GuildRefusalPermissionDenied:
		return guild.GuildErrPermissionDenied
	case domain.GuildRefusalMaxLevel:
		return guild.GuildErrLevelUpFull
	case domain.GuildRefusalGPDeficit:
		return guild.GuildErrLevelUpGPDeficit
	case domain.GuildRefusalGoldDeficit:
		return guild.GuildErrLevelUpGoldDeficit
	case domain.GuildRefusalNoCompensation:
		return guild.GuildErrNoCompensation
	}
	return guildNpcRefused
}

/*
================
HandleGuildLevelUp

0x73F0 from the level-up window. v1.188 5C63D0 refuses a player outside a
guild (0x0D), anyone but the master (0x1E) and a level 5 guild (0x21),
then 5C6240 prices the level; the door does all of it under the store
lock.
================
*/
func (rt *Runtime) HandleGuildLevelUp(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if c == nil || err != nil || r.Done() != nil {
		return guildNpcAnswer(opGuildLevelUpResponse, guildNpcRefused)
	}
	store := rt.deps.GuildAuthority()
	if store == nil {
		return guildNpcAnswer(opGuildLevelUpResponse, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildLevelUpResponse, guildNpcRefused)
	}
	snapshot, refusal := store.LevelUpGuildAs(division, c.ID)
	if refusal.Refused() {
		return guildNpcAnswer(opGuildLevelUpResponse, guildRefusalCode(refusal))
	}
	push := wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeGuildLevel3B29(snapshot.Guild.Level, snapshot.Guild.GP)}
	for _, member := range snapshot.Members {
		if member.CharID != c.ID && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, member.Name, []wire.Frame{push})
		}
	}
	result := guildNpcAnswer(opGuildLevelUpResponse, 0)
	result.Frames = append(result.Frames, push, goldFrame(c))
	return result
}

/*
================
HandleGuildMasterLeave

0x77D4 from the master-leave window: the master hands the guild to a
member (v1.188 5C6900, guild job 0x13). The member becomes grade 0 with
the master's permissions. INFERENCE: the shard's job is unseen; the old
master keeps the guild as an ordinary joiner (guild.JoinerGrade, no
permissions). v1.188's guild-war and siege-title refusals (0x4C5D,
0x4C55) have no v1.150 notice and no owner in this port.
================
*/
func (rt *Runtime) HandleGuildMasterLeave(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	target, err2 := r.U32()
	if c == nil || err != nil || err2 != nil || r.Done() != nil {
		return guildNpcAnswer(opGuildMasterLeaveDone, guildNpcRefused)
	}
	store := rt.deps.GuildAuthority()
	if store == nil {
		return guildNpcAnswer(opGuildMasterLeaveDone, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildMasterLeaveDone, guildNpcRefused)
	}
	code := uint8(0)
	var master, heir domain.GuildMemberRecord
	snapshot, refusal := store.UpdateGuildAs(division, c.ID, "guild-master-leave", domain.GuildAuthorization{LeaderOnly: true},
		func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			from, to := -1, -1
			for index, member := range members {
				if member.CharID == c.ID {
					from = index
				}
				if member.JID == target {
					to = index
				}
			}
			switch {
			case to < 0:
				code = guild.GuildErrMemberNotFound
				return record, members, false
			case to == from:
				code = guildNpcRefused
				return record, members, false
			}
			next := append([]domain.GuildMemberRecord(nil), members...)
			next[to].Grade, next[to].PermMask = 0, next[from].PermMask
			next[from].Grade, next[from].PermMask = guild.JoinerGrade, guild.JoinerPermMask
			master, heir = next[from], next[to]
			return record, next, true
		})
	if code != 0 {
		return guildNpcAnswer(opGuildMasterLeaveDone, code)
	}
	if refusal.Refused() {
		return guildNpcAnswer(opGuildMasterLeaveDone, guildRefusalCode(refusal))
	}
	// The old master's row first: the heir's grade 0 then names the master.
	pushes := []wire.Frame{
		{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberGrade3B29(master.JID, master.Grade, master.PermMask)},
		{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberGrade3B29(heir.JID, heir.Grade, heir.PermMask)},
	}
	for _, member := range snapshot.Members {
		if member.CharID != c.ID && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, member.Name, pushes)
		}
	}
	result := guildNpcAnswer(opGuildMasterLeaveDone, 0)
	result.Frames = append(result.Frames, pushes...)
	return result
}

/*
================
HandleGuildCompensation

0x7140 from the compensation row (v1.188 5C72A0): a guild member asks
what guild wars owe the guild; [1][u32 gold] opens the claim box (5D4050)
and nothing owed answers 0x45.
================
*/
func (rt *Runtime) HandleGuildCompensation(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	store := rt.deps.GuildAuthority()
	if c == nil || err != nil || r.Done() != nil || store == nil {
		return guildNpcAnswer(opGuildCompensationDue, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildCompensationDue, guildNpcRefused)
	}
	guildID, member := store.GuildOfCharacter(division, c.ID)
	record, _, found := store.Guild(division, guildID)
	if !member || !found {
		return guildNpcAnswer(opGuildCompensationDue, guild.GuildErrNotMember)
	}
	if record.WarCompensation <= 0 {
		return guildNpcAnswer(opGuildCompensationDue, guild.GuildErrNoCompensation)
	}
	owed := uint32(min(record.WarCompensation, int64(^uint32(0))))
	return OpResult{Frames: []wire.Frame{{Opcode: opGuildCompensationDue, Payload: wire.NewWriter(5).U8(1).U32(owed).Payload()}}}
}

/*
================
HandleGuildCompensationClaim

0x73F7 from the claim box: the master collects (v1.188 5C7330, job 0x1E).
================
*/
func (rt *Runtime) HandleGuildCompensationClaim(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	store := rt.deps.GuildAuthority()
	if c == nil || err != nil || r.Done() != nil || store == nil {
		return guildNpcAnswer(opGuildCompensationPaid, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildCompensationPaid, guildNpcRefused)
	}
	if _, refusal := store.ClaimWarCompensationAs(division, c.ID); refusal.Refused() {
		return guildNpcAnswer(opGuildCompensationPaid, guildRefusalCode(refusal))
	}
	result := guildNpcAnswer(opGuildCompensationPaid, 0)
	result.Frames = append(result.Frames, goldFrame(c))
	return result
}

/*
================
GuildManagerInRange

The guild package's create admission (guild.GuildManagers).
================
*/
func (rt *Runtime) GuildManagerInRange(division string, c *enterworld.Character, gid uint32) bool {
	if c == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.guildManagerNpc(division, c, gid)
}
