/*
===========================================================================

hwan.go - the Blue Berserk (port-only, not native)

The cap-110 Blue Berserk quests (quest/hwan_*.go) raise a character's Blue
Berserk level from 0 to 4 (the level table holds 6). This file is what that
level does, from the 1.188-era game data (hwanleveldata, _RefHWANLevel):

	SPDU  the Berserk body's speed, 200 at every level: the base Berserk's
	      doubling in movementeffect.go already is it
	DABR  while Berserk, 10 + 5 x (level - 1) percent of all incoming damage is
	      absorbed (the four incoming-damage keepers 0xAE..0xB1)
	0x3FBF the level, told to its owner (below)

and what the Energy of Life scrolls (ITEM_ETC_SECOND_HWAN_SCROLL_01..04) do:

  - a scroll needs Blue Berserk level n (itemdata requirement type 769,
    refusal 0x18F7) as well as its character level
  - using it puts the 20 minute buff (a limited use of ITEM_ETC_SECOND_HWAN)
    and the extra Berserk time (a thp param job: 5, 8, 11 or 14 seconds)
    on the bar; it starts no cooldown
  - the buff's charge fills the Berserk gauge (the old Berserk potion's rule:
    refused while Berserk), takes both rows off the bar and starts the shared
    20 minute cooldown (group 0xC9)
  - port-only decision (owner, 2026-11-08): a second scroll is refused while
    one's rows are on the bar, because the client keeps a single extension value
  - a Berserk that starts adds the thp seconds to its 60

0x3FBF is a port-only opcode: no v1.150 client or v1.188 server uses it. Body,
little-endian: u32 object id, u8 level. The server tells the owner at world
entry and whenever the level rises.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	// OpHwanLevel is the port-only S->C Blue Berserk level (above).
	OpHwanLevel uint16 = 0x3FBF
	// hwanRequirementType is the itemdata requirement type that names the Blue
	// Berserk level (columns 34 and 35 of a scroll's row).
	hwanRequirementType = 769
	// errCodeHwanLevel is category-1 code 0x18F7: UIIT_MSG_HWANITEM_CANNOT_USE.
	errCodeHwanLevel uint8 = 0xf7
	// hwanChargeItem is the buff item the scrolls put on the bar.
	hwanChargeItem = "ITEM_ETC_SECOND_HWAN"
	// hwanAbsorbBase and hwanAbsorbStep are DABR: 10, 15, 20, 25, 30, 35 percent.
	hwanAbsorbBase = 10
	hwanAbsorbStep = 5
	// hwanModifierSource keys the absorption writes apart from every other owner
	// (most wanted is 0x7fffff01).
	hwanModifierSource uint32 = 0x7fffff02
	// hwanChargeGroup and hwanChargeCooldownMs are the buff item's shared cooldown
	// (COOLTIME:0xC9, usage time 1,200,000 ms) should its row not carry them.
	hwanChargeGroup      uint32 = 0xc9
	hwanChargeCooldownMs int64  = 1_200_000
	// berserkBaseMs is the Berserk body's length before the extra seconds.
	berserkBaseMs int64 = 60_000
	// hwanBerserkFailure is the refusal of a charge in Berserk (as the potion).
	hwanBerserkFailure uint8 = 0xce
)

/*
================
hwanLevel

The character's Blue Berserk level, 0 without the quest owner's door.
================
*/
func (rt *Runtime) hwanLevel(c *enterworld.Character) uint8 {
	if rt.HwanLevel == nil || c == nil {
		return 0
	}
	return rt.HwanLevel(c)
}

/*
================
HwanLevelFrame

The level as one port-only frame for its owner (OpHwanLevel).
================
*/
func HwanLevelFrame(objectID uint32, level uint8) wire.Frame {
	return wire.Frame{Opcode: OpHwanLevel, Payload: wire.NewWriter(5).U32(objectID).U8(level).Payload()}
}

/*
================
HwanLevelFrames

What the quest owner sends when a level is raised: the level, and the title's
name as a notice (UIO_SECOND_HWAN_{CH,EU}_NAME_LEVEL_n) so even a client that
only reads notices tells the player. Nothing for level 0.
================
*/
func (rt *Runtime) HwanLevelFrames(c *enterworld.Character) []wire.Frame {
	level := rt.hwanLevel(c)
	if level == 0 {
		return nil
	}
	return []wire.Frame{HwanLevelFrame(enterworld.ObjectIDForCharacter(c), level), HwanTitleNotice(c, level)}
}

/*
================
HwanEntryFrames

The level frame a world entry owes its owner; none while the level is 0 (a
native shard, or a character who never turned blue, sends nothing).
================
*/
func (rt *Runtime) HwanEntryFrames(c *enterworld.Character) []wire.Frame {
	level := rt.hwanLevel(c)
	if level == 0 {
		return nil
	}
	return []wire.Frame{HwanLevelFrame(enterworld.ObjectIDForCharacter(c), level)}
}

/*
================
HwanTitleNotice

The title of a level as a 0x36BF notice: the Chinese or the European name by the
character's race.
================
*/
func HwanTitleNotice(c *enterworld.Character, level uint8) wire.Frame {
	race := "CH"
	if enterworld.NativeCountryByte9C(c) == 1 {
		race = "EU"
	}
	symbol := "UIO_SECOND_HWAN_" + race + "_NAME_LEVEL_" + string(rune('0'+level))
	return wire.Frame{Opcode: 0x36bf, Payload: wire.NewWriter(len(symbol) + 2).U16(uint16(len(symbol))).Bytes([]byte(symbol)).Payload()}
}

/*
================
hwanItemRequirement

The Blue Berserk level a scroll names (requirement type 769); 0 when it names
none, errCodeHwanLevel when the character is below it. As the other typed
requirements, one satisfied slot passes the item.
================
*/
func (rt *Runtime) hwanItemRequirement(character *enterworld.Character, ref *enterworld.ItemRef) uint8 {
	named := false
	for slot := 0; slot < 4; slot++ {
		if ref.ReqQuadTypes[slot] == hwanRequirementType {
			named = true
		}
	}
	if !named {
		return 0
	}
	level := int64(rt.hwanLevel(character))
	if quadWalk(ref, func(kind int64) (int64, bool) { return level, kind == hwanRequirementType }) {
		return errCodeHwanLevel
	}
	return 0
}

/*
================
hwanWrites

The Blue Berserk's absorption: while the body is Berserk, 10 + 5 x (level - 1)
percent off every incoming-damage keeper. Nil without a level or Berserk.
================
*/
func (rt *Runtime) hwanWrites(c *enterworld.Character) []paramkeeper.Write {
	level := rt.hwanLevel(c)
	if level == 0 || c.NativeBodyStatus != 1 {
		return nil
	}
	percent := uint32(hwanAbsorbBase + hwanAbsorbStep*(int(level)-1))
	return combat.IncomingReductionWrites(4|8|1|2, percent, hwanModifierSource)
}

/*
================
hwanBerserkExtraMs

The extra Berserk time the thp rows on the bar give a Berserk that starts now.
================
*/
func hwanBerserkExtraMs(c *enterworld.Character, nowMs int64) int64 {
	return paramJobPercent(c, paramBerserkTime, nowMs) * 1000
}

/*
================
hwanEffectOnBar

Whether an Energy of Life scroll's rows are still on the bar: its limited use of
the buff item, or its thp row.
================
*/
func hwanEffectOnBar(c *enterworld.Character, nowMs int64) bool {
	for _, work := range c.CompositeJobs {
		if work.TargetCodename == hwanChargeItem && work.EndUnixMs > nowMs {
			return true
		}
	}
	return paramJobPercent(c, paramBerserkTime, nowMs) != 0
}

/*
================
hwanCharge

The buff's charge (the limited use of ITEM_ETC_SECOND_HWAN): the old Berserk
potion's rule, then the rows leave the bar and the shared cooldown starts. Runs
inside the character door; the caller removes the buff's own row.
================
*/
func (rt *Runtime) hwanCharge(c *enterworld.Character, ref *enterworld.ItemRef, now int64, result *OpResult) bool {
	if c.NativeBodyStatus == 1 {
		*result = itemUseFailure(hwanBerserkFailure)
		return false
	}
	frames := []wire.Frame{}
	if c.ModifyBerserkPoints(5) {
		frames = append(frames, berserkPointsFrame(c, 0))
	}
	owner := enterworld.ObjectIDForCharacter(c)
	kept := c.ParamJobs[:0:0]
	ended := map[uint32]bool{}
	for _, job := range c.ParamJobs {
		if job.Param != paramBerserkTime {
			kept = append(kept, job)
			continue
		}
		if !ended[job.ItemRefObjID] {
			ended[job.ItemRefObjID] = true
			frames = append(frames, wire.Frame{Opcode: wire.OpParamJobEnd, Payload: wire.EncodeParamJobEnd(owner, job.ItemRefObjID)})
		}
	}
	c.ParamJobs = kept
	group, cooldownMs, ok := itemCooltime(ref)
	if !ok {
		group, cooldownMs = hwanChargeGroup, hwanChargeCooldownMs
	}
	if c.ItemGroupCooldowns == nil {
		c.ItemGroupCooldowns = map[uint32]int64{}
	}
	c.ItemGroupCooldowns[group] = now + cooldownMs
	*result = OpResult{Frames: frames}
	return true
}

/*
================
SetGMBerserkLevel

The gmcommand port: the GM's /SETBERSERK name level. Port-only test tooling,
not native: it sets the named character's Blue Berserk level (0 to 6) for the
life of the shard process, tells that character the new level and its title,
and leaves the quests alone. Privilege is rechecked inside the store door, as
SetGMSpeed does.
================
*/
func (rt *Runtime) SetGMBerserkLevel(division, sender, name string, level uint8) bool {
	if rt == nil || rt.deps == nil || rt.SetHwanLevel == nil || level > 6 {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, sender)
	target := rt.findCharacter(division, name)
	if gm == nil || target == nil {
		return false
	}
	var frames []wire.Frame
	if !rt.deps.Update(target, "gm-set-berserk", func() bool {
		if target.DeletePending || !gm.GMPrivilege || !rt.SetHwanLevel(target, level) {
			return false
		}
		frames = []wire.Frame{HwanLevelFrame(enterworld.ObjectIDForCharacter(target), level)}
		if level > 0 {
			frames = append(frames, HwanTitleNotice(target, level))
		}
		return true
	}) {
		return false
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, target.Name, frames)
	}
	return true
}

/*
================
SetGMBerserkGauge

The gmcommand port: the GM's /SETGAUGE name balls. Port-only test tooling, not
native: the named character's Berserk gauge holds exactly balls (0 to 5) from
now on, whatever it held (a gauge cannot be raised while Berserk is active, as
native, so that is refused). The character is told the new count. Privilege is
rechecked inside the store door.
================
*/
func (rt *Runtime) SetGMBerserkGauge(division, sender, name string, balls uint8) bool {
	if rt == nil || rt.deps == nil || balls > 5 {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, sender)
	target := rt.findCharacter(division, name)
	if gm == nil || target == nil {
		return false
	}
	var frame wire.Frame
	if !rt.deps.Update(target, "gm-set-gauge", func() bool {
		if target.DeletePending || !gm.GMPrivilege || target.NativeBodyStatus == 1 && balls > target.BerserkPoints {
			return false
		}
		target.BerserkPoints = balls
		frame = berserkPointsFrame(target, 0)
		return true
	}) {
		return false
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, target.Name, []wire.Frame{frame})
	}
	return true
}
