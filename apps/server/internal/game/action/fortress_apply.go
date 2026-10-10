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
member of the guild then hears 0x3887 [0x0C or 0x0D][u32 fortress][u8 kind]
(v1.188 61E620 / 61E7E0 send it to the guild through 5C4260; the client
stores the fortress as its guild's and shows the notice).

Admission is v1.188 633910 (apply) and 633C40 (withdraw), in their order.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/fortress"
)

const (
	opFortressInteraction       uint16 = 0x71e1
	opFortressInteractionResult uint16 = 0xb1e1
	opFortressWarState          uint16 = 0x3887

	fortressWarStatus uint8 = 6
	fortressApply     uint8 = 7
	fortressWithdraw  uint8 = 8

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

0x71E1. Decoding is shared with the manager services; each authority owns
the corresponding state and admission.
================
*/
func (rt *Runtime) HandleFortressInteraction(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) < 5 {
		return OpResult{}
	}
	subtype := payload[4]
	request, err := siege.DecodeInteraction(payload)
	if err != nil {
		if subtype == fortressApply || subtype == fortressWithdraw {
			return fortressRefusal(subtype, fortressErrInvalid)
		}
		return OpResult{}
	}
	gid := request.Target
	if subtype == siege.ActionSchedule || subtype == siege.ActionAide || subtype == siege.ActionTaxQuery || subtype == siege.ActionTaxRate || subtype == siege.ActionTaxCollect || subtype == siege.ActionStaffQuery || subtype == siege.ActionStaffHire ||
		subtype == siege.ActionStructureQuery || subtype == siege.ActionRepair {
		unlock := rt.lockDivision(division)
		defer unlock()
		return rt.fortressServiceQuery(division, c, request)
	}
	// 519E60 skips the NPC range check for these two: the target is the
	// summoned object or structure itself.
	if subtype == siege.ActionDismiss || subtype == siege.ActionDemolish {
		unlock := rt.lockDivision(division)
		defer unlock()
		if subtype == siege.ActionDismiss {
			return rt.fortressDismissObject(division, c, request)
		}
		return rt.fortressDemolishStructure(division, c, request)
	}
	if subtype == fortressWarStatus {
		unlock := rt.lockDivision(division)
		defer unlock()
		return rt.fortressWarStatus(division, c, gid)
	}
	if subtype != fortressApply && subtype != fortressWithdraw {
		return fortressRefusal(subtype, fortressErrUnknown)
	}
	fortressID, kindByte := request.Fortress, request.Value8
	if kindByte > uint8(fortress.RequestAlly) {
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
	if _, members, ok := rt.Guilds.Guild(division, guildID); ok && rt.PushCharacterFrames != nil {
		for _, member := range members {
			if member.CharID != c.ID {
				rt.PushCharacterFrames(division, member.Name, []wire.Frame{state})
			}
		}
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
fortressWarStatus

Subtype 6, the official's "fortress war" row (CIFNpcTalk action 0x34 row
1): [6][1], the next war's start as a 16-byte SYSTEMTIME, then whether the
guild has applied and, if so, the fortress and kind. The v1.150 client
opens the application window on it (754A40 -> 69EA10, 663200) and derives
every date it shows from that one time (660C70: the war until two hours
later, applications from three days before to the day before). v1.188's
633610 writes four month/day/hour/minute quadruples instead; the v1.150
reader is the contract here.
================
*/
func (rt *Runtime) fortressWarStatus(division string, c *enterworld.Character, gid uint32) OpResult {
	if rt.Fortresses == nil || rt.FortressWindows == nil {
		return fortressRefusal(fortressWarStatus, fortressErrUnknown)
	}
	if !rt.selectedOfficial(division, c, gid) {
		return fortressRefusal(fortressWarStatus, fortressErrInvalid)
	}
	if c.GuildID == nil || *c.GuildID == 0 {
		return fortressRefusal(fortressWarStatus, fortressErrNoGuild)
	}
	w := wire.NewWriter(24).U8(fortressWarStatus).U8(1)
	writeSystemTime(w, rt.FortressWindows(rt.Now().UnixMilli()))
	if fortressID, applied := rt.Fortresses.AppliedFortress(division, *c.GuildID); applied {
		record, _ := rt.Fortresses.Get(division, fortressID)
		w.U8(1).U32(fortressID).U8(uint8(record.Applicants[*c.GuildID]))
	} else {
		w.U8(0)
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: w.Payload()}}}
}

/*
================
writeSystemTime

A Win32 SYSTEMTIME: year, month, weekday (0 Sunday), day, hour, minute,
second, milliseconds, each a u16.
================
*/
func writeSystemTime(w *wire.Writer, at time.Time) {
	w.U16(uint16(at.Year())).U16(uint16(at.Month())).U16(uint16(at.Weekday())).U16(uint16(at.Day()))
	w.U16(uint16(at.Hour())).U16(uint16(at.Minute())).U16(uint16(at.Second())).U16(uint16(at.Nanosecond() / 1e6))
}

/*
================
selectedOfficial

The selected, visible NPC is a fortress official (function option 0x18).
================
*/
func (rt *Runtime) selectedOfficial(division string, c *enterworld.Character, gid uint32) bool {
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return false
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok {
		return false
	}
	for _, record := range rt.Fortresses.Records(division) {
		if record.OfficialNpc == npc.Codename {
			return true
		}
	}
	return false
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
	// 61D300: an ally of the owning guild must apply as one, and only an
	// ally may.
	if allied := record.GuildID != 0 && rt.Unions.Allied(division, record.GuildID, guildID); allied != (kind == fortress.RequestAlly) {
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
