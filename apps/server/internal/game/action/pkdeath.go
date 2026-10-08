/*
===========================================================================

pkdeath.go - what a player's death costs

CGObjChar_ProcessNormalDeath calls vtable +0x4DC (4E67E0), which resolves
the killer (the recorded killer, else the last attacker) and runs
CGObjPC_ResolveDeathKind (4E6EC0): the death is classified
(pk.DeathKind) and costs EXP and SP (progression), may drop an inventory
item (pk.SelectDropSlot) and, for a murderer, eases the penalty. All of it
commits inside the fatal-HP character transaction.

The kill's other side - the killer's PvP EXP and murder bookkeeping
(CGObjPC_ProcessPvpKillRewards 4E1F60) - belongs to the player hit path.

INFERENCE: a death with no killer object (a lethal zombie potion, or a
status the victim inflicted on itself) costs nothing, as 4E67E0 finds no
killer for 4E6EC0; the port keeps no "last attacker" beyond the hit.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"

	log "github.com/sirupsen/logrus"
)

/*
================
deathKiller

The object that dealt the fatal damage. Zero: no killer.
================
*/
type deathKiller struct {
	monster      *monster.Instance
	player       *enterworld.Character
	siege        bool
	strikerLevel int64
}

/*
================
deathKind

CGObjPC_ResolvePvpKillRelation (4E6590). A monster killer: a trader or
hunter falls to a thief or hunter monster, a thief to a hunter monster,
as a job death; any other monster death is kind 4. A player killer is
classified by playerKillKind.
================
*/
func (rt *Runtime) deathKind(division string, victim *enterworld.Character, killer deathKiller) pk.DeathKind {
	if killer.siege {
		return pk.DeathSpecialWorld
	}
	switch {
	case killer.monster != nil:
		m := killer.monster
		switch enterworld.DressedJob(victim) {
		case domain.JobTrader, domain.JobHunter:
			if m.ThiefMonster() || m.HunterMonster() {
				return pk.DeathJob
			}
		case domain.JobThief:
			if m.HunterMonster() {
				return pk.DeathJob
			}
		}
		return pk.DeathMonster
	case killer.player != nil:
		return rt.playerKillKind(division, victim, killer.player)
	}
	return pk.DeathNone
}

/*
================
deathCost

The private and public frames of a death's cost.
================
*/
type deathCost struct {
	actor, public []wire.Frame
}

/*
================
settleDeathCostInDoor

4E6EC0 inside the fatal transaction: the EXP and SP loss, the item drop,
then a murderer's relief.
================
*/
func (rt *Runtime) settleDeathCostInDoor(division string, c *enterworld.Character, killer deathKiller, now int64) deathCost {
	var cost deathCost
	kind := rt.deathKind(division, c, killer)
	var penalty uint32
	if c.PK != nil {
		penalty = c.PK.Penalty
	}
	level := uint8(min(rewardLevel(c), 0xff))
	kept := float32(paramJobPercent(c, paramDeathExpKept, now))
	switch {
	case kind == pk.DeathSpecialWorld:
		// 4E6D60 has ordinary rates, one-fifth cap, no SP/drop/PK relief.
		rule, ok := pk.DeathLoss(pk.DeathPlayer, level, 0, killer.player != nil, false)
		if ok && rt.ApplyDeathPenalty != nil {
			frames, _ := rt.ApplyDeathPenalty(c, pk.DeathPenalty{Rule: rule, SpecialWorld: true, ReductionPercent: kept})
			cost.actor = append(cost.actor, frames...)
		}
		return cost
	case kind == pk.DeathJob:
		// 4E6820 reads the killer object's level, a monster's or a player's.
		killerLevel := uint8(0)
		if killer.monster != nil {
			killerLevel = killer.monster.Ref.Level
		} else if killer.player != nil {
			killerLevel = levelByte(killer.player)
		}
		if rt.ApplyDeathPenalty != nil {
			frames, _ := rt.ApplyDeathPenalty(c, pk.DeathPenalty{Job: true, KillerLevel: killerLevel, ReductionPercent: kept})
			cost.actor = append(cost.actor, frames...)
		}
		return cost
	case !kind.Penalized():
		return cost
	}
	thief := killer.monster != nil && killer.monster.ThiefMonster()
	if rule, ok := pk.DeathLoss(kind, level, penalty, killer.player != nil, thief); ok && rt.ApplyDeathPenalty != nil {
		frames, _ := rt.ApplyDeathPenalty(c, pk.DeathPenalty{Rule: rule, ReductionPercent: kept})
		cost.actor = append(cost.actor, frames...)
	}
	if level > deathDropProtectedLevel {
		drop := rt.dropOnDeathInDoor(division, c, kind, penalty, now)
		cost.actor = append(cost.actor, drop.actor...)
		cost.public = append(cost.public, drop.public...)
	}
	relief, daily := pk.DeathRelief(kind)
	var changed pk.Changes
	before := c.PVPState()
	if penalty > 0 {
		changed |= pk.AddPenalty(c, relief, rt.Now())
	}
	if c.PK != nil && c.PK.DailyCount > 0 {
		changed |= pk.AddDaily(c, daily, rt.Now())
	}
	cost.actor = append(cost.actor, pkRecordFrames(c, changed)...)
	rt.notePKRecord(division, c)
	if before != c.PVPState() {
		cost.public = append(cost.public, playerPVPStateFrame(c))
	}
	return cost
}

// deathDropProtectedLevel: the drop roll sits inside 4E6980's level > 10.
const deathDropProtectedLevel = 10

/*
================
dropOnDeathInDoor

4E6BF6..4E6CB0: rand() % 101 against the penalty's chance, then the
inventory's slot (a guild-war death passes no penalty), dropped where the
corpse lies. A worn item leaves its socket for every observer and the
victim's stats follow.
================
*/
func (rt *Runtime) dropOnDeathInDoor(division string, c *enterworld.Character, kind pk.DeathKind, penalty uint32, now int64) deathCost {
	var cost deathCost
	if rt.CombatRoll == nil {
		return cost
	}
	roll := pk.DropRoll(rt.CombatRoll)
	drops, err := pk.RollsDrop(penalty, roll)
	if err != nil || !drops {
		return cost
	}
	if kind == pk.DeathGuildWar {
		penalty = 0
	}
	slot, ok, err := pk.SelectDropSlot(rt.deathDropSlots(c), penalty, roll)
	if err != nil {
		log.Warnf("action: death drop for %s refused - %v", c.Name, err)
		return cost
	}
	if !ok {
		return cost
	}
	inv := bagOf(c)
	dropped, fault := inv.DeathDrop(uint8(slot))
	if fault != nil {
		return cost
	}
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	added := rt.addCharacterGround(division, c, PlanItemDrop(dropped, dropped.Quantity, at, c.Name, rt.Now()))
	if added.Gid == 0 {
		return cost
	}
	c.MissionInventory = rowsFromInvItems(inv.Items())
	spawn := added.SpawnRow(true)
	cost.actor = append(cost.actor, wire.Frame{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodeGroundDropResult(uint8(slot))})
	cost.public = append(cost.public, rt.groundReferences([]grounditem.Item{added})...)
	cost.public = append(cost.public, wire.DropBroadcastFrames(spawn)...)
	if inventory.IsEquipmentSlot(uint8(slot)) {
		cost.public = append(cost.public, FramesFromSocketVisuals(enterworld.ObjectIDForCharacter(c),
			[]inventory.SocketVisual{{Socket: uint8(slot)}})...)
		if display, err := rt.PlayerBaseStats(division, c); err == nil {
			cost.actor = append(cost.actor, wire.Frame{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(c, display)})
		}
	}
	cost.actor = append(cost.actor, rt.updateQuestInventory(c)...)
	return cost
}

/*
================
deathDropSlots

The inventory as 4BB460 sees it. CGItem_CanDropOnDeathPenalty (4B7CF0):
not an event item (TID 3/3/9), not a cash item, RefObj CanDrop bit 1, not
a COS summoner (TID 3/2), not equipment of rarity 2 or 6. The in-use
latch (+0x15C) never overlaps a death: item use here is synchronous.
================
*/
func (rt *Runtime) deathDropSlots(c *enterworld.Character) []pk.DropSlot {
	slots := make([]pk.DropSlot, inventory.BagEnd(c))
	refs := rt.deps.ItemReferences()
	for _, row := range c.MissionInventory {
		if row.Slot < 0 || row.Slot >= int64(len(slots)) {
			continue
		}
		s := &slots[row.Slot]
		s.Occupied = true
		if refs == nil {
			continue
		}
		ref, ok := refs.ItemRefByCodename(row.Codename)
		if !ok {
			continue
		}
		t := ref.TypeIDs
		s.Ammunition = t[0] == 3 && t[1] == 3 && t[2] == 4
		event := t[0] == 3 && t[1] == 3 && t[2] == 9
		summoner := t[0] == 3 && t[1] == 2
		sealed := t[0] == 3 && t[1] == 1 && (ref.Rarity == 2 || ref.Rarity == 6)
		s.Droppable = ref.CanDropOnDeath && !ref.CashItem && !event && !summoner && !sealed
	}
	return slots
}

/*
================
pkRecordFrames

The live updates of the record fields a change moved (v1.150 0x33C4,
0x3647, 0x30F2).
================
*/
func pkRecordFrames(c *enterworld.Character, changed pk.Changes) []wire.Frame {
	if c.PK == nil || changed == 0 {
		return nil
	}
	var frames []wire.Frame
	if changed&pk.ChangedDaily != 0 {
		frames = append(frames, wire.PKDailyFrame(c.PK.DailyCount))
	}
	if changed&pk.ChangedTotal != 0 {
		frames = append(frames, wire.PKTotalFrame(c.PK.TotalCount))
	}
	if changed&pk.ChangedPenalty != 0 {
		frames = append(frames, wire.PKPenaltyFrame(c.PK.Penalty))
	}
	return frames
}
