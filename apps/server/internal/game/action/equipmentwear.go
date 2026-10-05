/*
===========================================================================

equipmentwear.go - equipment loses durability in combat

v1.188 SkillCombat_CalculateHitOutcome (58ED80..) counts, per execution:
the attacker's attempts that were not blocked (result header +0x06,
58F743), each defender's landed hits (+0x15, 58F79F) and its blocks
(+0x14, 58F0F3). SkillCombat_ApplyResultRecipients (593800) then rolls each
count once through CGObjPC_RollEquipmentWear (4A9A90): a chance of count
x 2 percent (capped at 100, abs(rand()) % 100 + 1 <= chance) to take one
point from the weapon (slot 6), from the first of slots 0..7 but 6 whose own
roll succeeds (armour, shield included), or from the shield (slot 7). Only
players wear (the actor's +0x1C virtual).

CGObjPC_OffsetItemDurability (4E7100) applies the point: nothing below
zero, and an equipped item that reaches zero stops contributing
(RemoveEquipmentContributions, ReevaluateEquipmentRequirements) before the
durability packet (v1.188 0x3052, v1.150 0x31E8) goes out.

This port rolls no evasion yet, so every unblocked attempt lands: the
armour count equals the weapon count.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/durability"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const (
	// The RollEquipmentWear modes.
	wearWeapon uint8 = 0
	wearArmour uint8 = 1
	wearShield uint8 = 2

	wearWeaponSlot = 6
	wearShieldSlot = 7
	// Slots 0..7 are the armour sockets, the weapon and the shield.
	wearSockets = 8
	// Each counted event adds this many percent to its roll.
	wearChancePerEvent = 2
)

/*
================
wearTally

One execution's counts for one actor: the attacker's unblocked attempts,
or a defender's landed hits and blocks.
================
*/
type wearTally struct {
	weapon, armour, shield uint8
}

/*
================
note

Counts one impact's outcome the way 58F0F3/58F743/58F79F do.
================
*/
func (w *wearTally) note(blocked bool, attacker bool) {
	add := func(count *uint8) {
		if *count < 0xff {
			*count++
		}
	}
	switch {
	case attacker && !blocked:
		add(&w.weapon)
	case !attacker && blocked:
		add(&w.shield)
	case !attacker:
		add(&w.armour)
	}
}

/*
================
wearRoll

abs(rand()) % 100 + 1 <= chance.
================
*/
func (rt *Runtime) wearRoll(chance int) bool {
	roll := rt.WearRoll
	if roll == nil {
		roll = combat.SecureRoll32767
	}
	value, err := roll()
	return err == nil && int(value%100)+1 <= chance
}

/*
================
wearFrames

What a point of wear publishes: the owner's packets and the observers'
(effects a broken item ends).
================
*/
type wearFrames struct {
	actor, public []wire.Frame
}

/*
================
rollEquipmentWear

CGObjPC_RollEquipmentWear for one counter. Runs inside the character's
Update; returns the frames of any point taken.
================
*/
func (rt *Runtime) rollEquipmentWear(division string, c *enterworld.Character, mode, count uint8) wearFrames {
	if count == 0 {
		return wearFrames{}
	}
	chance := min(int(count)*wearChancePerEvent, 100)
	switch mode {
	case wearWeapon:
		if rt.wearRoll(chance) {
			return rt.offsetItemDurability(division, c, wearWeaponSlot, -1)
		}
	case wearShield:
		if rt.wearRoll(chance) {
			return rt.offsetItemDurability(division, c, wearShieldSlot, -1)
		}
	case wearArmour:
		for slot := uint8(0); slot < wearSockets; slot++ {
			if slot != wearWeaponSlot && rt.wearRoll(chance) {
				return rt.offsetItemDurability(division, c, slot, -1)
			}
		}
	}
	return wearFrames{}
}

/*
================
offsetItemDurability

CGObjPC_OffsetItemDurability (4E7100) for an equipment item in any
inventory slot. A broken item loses no further points; an equipped item's
zero crossing in either direction re-derives the stats and ends effects
whose equipment requirement it no longer meets.
================
*/
func (rt *Runtime) offsetItemDurability(division string, c *enterworld.Character, slot uint8, delta int32) wearFrames {
	index := -1
	for i, row := range c.MissionInventory {
		if row.Slot == int64(slot) {
			index = i
			break
		}
	}
	if index < 0 || !isEquipmentItem(c.MissionInventory[index].TypeFlags) {
		return wearFrames{}
	}
	row := &c.MissionInventory[index]
	if row.Durability < 0 || row.Durability > 0xffffffff || row.Durability == 0 && delta <= 0 {
		return wearFrames{}
	}
	before := uint32(row.Durability)
	state := durability.State{Current: before, Broken: before == 0}
	// For a loss the maximum never clamps; a repair passes the real maximum.
	maximum := before
	if delta > 0 {
		maximum = rt.equipmentMaxDurability(row)
	}
	value, err := state.Offset(row.TypeFlags, maximum, delta, true)
	if err != nil {
		return wearFrames{}
	}
	row.Durability = int64(state.Current)
	out := wearFrames{actor: []wire.Frame{wire.ItemDurabilityFrame(slot, value)}}
	if inventory.IsEquipmentSlot(slot) && (before == 0) != (state.Current == 0) {
		if ended := rt.retireUnmetEquipmentEffects(division, c); len(ended) != 0 {
			public, actor := rt.finishEndedEffects(division, c, ended, rt.Now().UnixMilli())
			out.actor = append(out.actor, actor...)
			out.public = append(out.public, public...)
		}
		if block, err := rt.PlayerBaseStats(division, c); err == nil {
			out.actor = append(out.actor, wire.Frame{Opcode: wire.OpBaseStats, Payload: block.Encode()})
		}
	}
	return out
}

/*
================
equipmentMaxDurability

The most a repair restores (combat.EquipmentMaxDurability); never below
the current value an item already carries.
================
*/
func (rt *Runtime) equipmentMaxDurability(row *enterworld.InventoryRow) uint32 {
	refs := rt.deps.ItemReferences()
	if refs == nil {
		return uint32(row.Durability)
	}
	ref, ok := refs.ItemRefByCodename(row.Codename)
	if !ok || ref == nil {
		return uint32(row.Durability)
	}
	maximum, err := combat.EquipmentMaxDurability(ref, *row, rt.deps.MagicOptionDefinitions())
	if err != nil {
		return uint32(row.Durability)
	}
	return max(maximum, uint32(row.Durability))
}

/*
================
applyEquipmentWear

Rolls one actor's tally after its execution, in its own character
transaction. Monsters never wear.
================
*/
func (rt *Runtime) applyEquipmentWear(division string, c *enterworld.Character, tally wearTally) wearFrames {
	var out wearFrames
	if c == nil || tally == (wearTally{}) {
		return out
	}
	rt.deps.Update(c, "equipment-wear", func() bool {
		if c.DeletePending {
			return false
		}
		for _, roll := range [...]struct{ mode, count uint8 }{
			{wearWeapon, tally.weapon}, {wearArmour, tally.armour}, {wearShield, tally.shield},
		} {
			taken := rt.rollEquipmentWear(division, c, roll.mode, roll.count)
			out.actor = append(out.actor, taken.actor...)
			out.public = append(out.public, taken.public...)
		}
		return len(out.actor) != 0
	})
	return out
}

/*
================
isEquipmentItem

483210: the type word names equipment.
================
*/
func isEquipmentItem(tid uint16) bool {
	return tid&2 == 0 && tid&0x1c == 0xc && tid&0x60 == 0x20
}
