/*
===========================================================================

fortress_apply.go - a guild applying to, or withdrawing from, a fortress war

The fortress official takes applications through 0x71E1 (v1.150
CGInterface_SendFortressInteraction71E1 703130; v1.188's 0x705E):
[u32 npc gid][u8 subtype][u32 fortress][u8 kind], subtype 7 to apply and
8 to withdraw, kind 0 to attack and 1 to stand with the owner. The answer
is 0xB1E1 [u8 subtype][u8 1][u32 fortress][u8 kind], or [u8 subtype][u8 2]
[u8 code] with the low byte of v1.188's 0x28xx error, shown as notice
category 0x1E (CPSMission_OnFortressManagerResponse0xB1E1 754A40). Every
player then hears 0x3887 [0x0C or 0x0D][u32 fortress][u8 kind].

Admission is v1.188 633910 (apply) and 633C40 (withdraw), in their order.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
)

const (
	opFortressInteraction       uint16 = 0x71e1
	opFortressInteractionResult uint16 = 0xb1e1
	opFortressWarState          uint16 = 0x3887

	fortressApply    uint8 = 7
	fortressWithdraw uint8 = 8

	// 0x3887 subtypes announcing a registration change.
	fortressWarApplied   uint8 = 0x0c
	fortressWarWithdrawn uint8 = 0x0d

	// Low bytes of the 0x28xx refusals (notice category 0x1E).
	fortressErrUnknown       uint8 = 0x02 // the request could not be queued
	fortressErrInvalid       uint8 = 0x03 // no such fortress
	fortressErrNotMaster     uint8 = 0x07 // UIIT_MSG_FORT_ETC_ERROR_ONLYGUILDMASTER_ACTION
	fortressErrNoGuild       uint8 = 0x0b // UIIT_MSG_FORT_ETC_ERROR_NOTJOINGUILD
	fortressErrGuildLevel    uint8 = 0x0c // UIIT_MSG_FORT_ETC_ERROR_TOOLESSGUILDLV
	fortressErrGuildMembers  uint8 = 0x0d // UIIT_MSG_FORT_ETC_ERROR_TOOLESSGUILDMB
	fortressErrGold          uint8 = 0x0f // UIIT_MSG_INTERACTION_FAIL_NOT_ENOUGH_MONEY
	fortressErrApplied       uint8 = 0x10 // UIIT_MSG_FORT_OFFICIAL_FORTWAR_APPLY_ERROR_03
	fortressErrOwns          uint8 = 0x11 // UIIT_MSG_FORT_ETC_ERROR_ALREADYPOSSESSFORT
	fortressErrAlliance      uint8 = 0x12 // UIIT_MSG_FORT_ETC_ERROR_WRONGALLIANCE
	fortressErrNotApplied    uint8 = 0x13 // UIIT_MSG_FORT_OFFICIAL_FORTWAR_APPLY_ERROR_01
	fortressErrPeriod        uint8 = 0x14 // UIIT_MSG_FORT_OFFICIAL_FORTWAR_APPLY_ERROR_02
	fortressErrWrongOfficial uint8 = 0x3a // the NPC is another fortress's official

	// 633910: a guild of level 3 with 7 members may apply.
	fortressApplyMinGuildLevel   = 3
	fortressApplyMinGuildMembers = 7
	// The guild master is the grade-0 member.
	guildMasterGrade = 0
)

/*
================
fortressRefusal
================
*/
func fortressRefusal(subtype, code uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: []byte{subtype, 2, code}}}}
}

/*
================
HandleFortressInteraction

0x71E1. Subtypes other than the war application belong to the fortress
manager's other functions and answer as an unknown operation.
================
*/
func (rt *Runtime) HandleFortressInteraction(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if err != nil || c == nil {
		return OpResult{}
	}
	subtype, err := r.U8()
	if err != nil {
		return OpResult{}
	}
	if subtype != fortressApply && subtype != fortressWithdraw {
		return fortressRefusal(subtype, fortressErrUnknown)
	}
	fortressID, err := r.U32()
	if err != nil {
		return fortressRefusal(subtype, fortressErrInvalid)
	}
	kindByte, err := r.U8()
	if err != nil || r.Done() != nil || kindByte > uint8(fortress.RequestAlly) {
		return fortressRefusal(subtype, fortressErrInvalid)
	}
	kind := fortress.RequestKind(kindByte)
	unlock := rt.lockDivision(division)
	defer unlock()
	if rt.Fortresses == nil || rt.Guilds == nil {
		return fortressRefusal(subtype, fortressErrUnknown)
	}
	if code := rt.fortressApplicationRefusal(division, c, gid, subtype, fortressID, kind); code != 0 {
		return fortressRefusal(subtype, code)
	}
	guildID := *c.GuildID
	if subtype == fortressApply && kind == fortress.RequestAttack {
		record, _ := rt.Fortresses.Get(division, fortressID)
		charged := rt.deps.Update(c, "fortress-war-apply", func() bool {
			if c.Gold == nil || uint64(*c.Gold) < record.RequestFee {
				return false
			}
			gold := *c.Gold - int64(record.RequestFee)
			c.Gold = &gold
			return true
		})
		if !charged {
			return fortressRefusal(subtype, fortressErrGold)
		}
	}
	if !rt.Fortresses.SetApplication(division, fortressID, guildID, kind, subtype == fortressApply) {
		return fortressRefusal(subtype, fortressErrUnknown)
	}
	announce := fortressWarApplied
	if subtype == fortressWithdraw {
		announce = fortressWarWithdrawn
	}
	state := wire.Frame{Opcode: opFortressWarState, Payload: wire.NewWriter(6).U8(announce).U32(fortressID).U8(kindByte).Payload()}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, []wire.Frame{state})
	}
	result := wire.Frame{Opcode: opFortressInteractionResult, Payload: wire.NewWriter(7).U8(subtype).U8(1).U32(fortressID).U8(kindByte).Payload()}
	frames := []wire.Frame{result, state}
	if subtype == fortressApply && kind == fortress.RequestAttack {
		frames = append(frames, goldFrame(c))
	}
	return OpResult{Frames: frames}
}

/*
================
fortressApplicationRefusal

The refusal byte for an application or withdrawal, zero to admit. The
caller holds the division lock.
================
*/
func (rt *Runtime) fortressApplicationRefusal(division string, c *enterworld.Character, gid uint32, subtype uint8, fortressID uint32, kind fortress.RequestKind) uint8 {
	if rt.Fortresses.Periods(division)&fortress.PeriodRequest == 0 {
		return fortressErrPeriod
	}
	record, ok := rt.Fortresses.Get(division, fortressID)
	if !ok {
		return fortressErrInvalid
	}
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return fortressErrWrongOfficial
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || npc.Codename != record.OfficialNpc {
		return fortressErrWrongOfficial
	}
	if c.GuildID == nil || *c.GuildID == 0 {
		return fortressErrNoGuild
	}
	guildID := *c.GuildID
	guild, members, ok := rt.Guilds.Guild(division, guildID)
	if !ok {
		return fortressErrNoGuild
	}
	if !guildMaster(members, c.ID) {
		return fortressErrNotMaster
	}
	if subtype == fortressWithdraw {
		if record.GuildID == guildID {
			return fortressErrOwns
		}
		if applied, ok := record.Applicants[guildID]; !ok || applied != kind {
			return fortressErrNotApplied
		}
		return 0
	}
	if _, owns := rt.Fortresses.OwnedFortress(division, guildID); owns {
		return fortressErrOwns
	}
	// 61D300: an ally of the owning guild must apply as one. Guild unions
	// are not part of this server, so every guild stands alone.
	if kind != fortress.RequestAttack {
		return fortressErrAlliance
	}
	if guild.Level < fortressApplyMinGuildLevel {
		return fortressErrGuildLevel
	}
	if len(members) < fortressApplyMinGuildMembers {
		return fortressErrGuildMembers
	}
	if c.Gold == nil || uint64(*c.Gold) < record.RequestFee {
		return fortressErrGold
	}
	if _, applied := rt.Fortresses.AppliedFortress(division, guildID); applied {
		return fortressErrApplied
	}
	return 0
}

/*
================
guildMaster
================
*/
func guildMaster(members []domain.GuildMemberRecord, characterID int64) bool {
	for _, member := range members {
		if member.CharID == characterID {
			return member.Grade == guildMasterGrade
		}
	}
	return false
}
