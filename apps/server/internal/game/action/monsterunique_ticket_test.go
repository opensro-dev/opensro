/*
===========================================================================

monsterunique_ticket_test.go - the unique reward switches on real kill plans

Port-only, not native (owner 2026-10-20): SRO_UNIQUE_GAP_EXEMPT admits a
unique's drops past the 4C1840 level gap, SRO_UNIQUE_POP_TICKET leaves
exactly one Magic Pop ticket per unique kill. Both off is byte-native:
a level-90 hunter loots nothing from level-20 Tiger Girl, and the Bone
Lord's native two probability-1.0 cards stay two.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
uniqueKillFixture

One unique of the given codename, level and spawn grade facing a hunter
of playerLevel, with every roll at 0 and only the gold heap, the return
scroll and the Magic Pop card resolvable: everything else the planner
picks fails its reference lookup and drops nothing, so what remains is
the prepass scrolls, the gold, the assigned rows and the ticket rule.
================
*/
func uniqueKillFixture(t *testing.T, code string, monsterLevel, rarity uint8, playerLevel int64) (*Runtime, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, monsterLevel)
	installSmallGoldRef(rt)
	items := rt.deps.ItemReferences().(staticItemSource)
	items[gacha.CardCodename] = &enterworld.ItemRef{RefObjID: 92000, Codename: gacha.CardCodename, TypeIDs: [4]int64{3, 3, 21, 1}}
	items["ITEM_ETC_SCROLL_RETURN_02"] = &enterworld.ItemRef{RefObjID: 92001, Codename: "ITEM_ETC_SCROLL_RETURN_02", TypeIDs: [4]int64{3, 3, 1, 1}}
	target.Ref.Codename = code
	target.Ref.MonsterType = rarity
	target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, rarity
	c.Level = &playerLevel
	rt.DropRoll = constantDropRoll(0)
	return rt, c, target
}

/*
================
planUniqueKill
================
*/
func planUniqueKill(rt *Runtime, c *enterworld.Character, target monster.Instance) []grounditem.Item {
	drops, _ := rt.planMonsterKillLoot(c, target, monster.Pose{
		RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z,
	}, rt.Now().UnixMilli())
	return drops
}

/*
================
countDrops

The ground items of one codename and their total stack count.
================
*/
func countDrops(drops []grounditem.Item, codename string) (items, total int) {
	for _, drop := range drops {
		if drop.Codename == codename {
			items++
			total += int(drop.StackCount)
		}
	}
	return items, total
}

/*
================
TestUniqueGapExemptAdmitsFullKillAndOneTicket

Level 90 against level-20 Tiger Girl: native 4C1840's threshold is -180
and admits nothing. With both switches on the whole kill is admitted
(the prepass scrolls land) and the native probability-1.0 card is
replaced by the one guaranteed ticket. One plan is one kill
(reward_group.go plans once per kill), so a party kill leaves the same
single ticket.
================
*/
func TestUniqueGapExemptAdmitsFullKillAndOneTicket(t *testing.T) {
	rt, c, target := uniqueKillFixture(t, "MOB_CH_TIGERWOMAN", 20, 3, 90)
	rt.UniqueRules = progression.UniqueRules{GapExempt: true, PopTicket: true}
	drops := planUniqueKill(rt, c, target)
	if items, total := countDrops(drops, gacha.CardCodename); items != 1 || total != 1 {
		t.Fatalf("unique kill left %d tickets (total %d), want exactly 1", items, total)
	}
	if scrolls, _ := countDrops(drops, "ITEM_ETC_SCROLL_RETURN_02"); scrolls == 0 {
		t.Fatal("gap exemption admitted nothing: the unique prepass scrolls are missing")
	}
}

/*
================
TestUniqueRulesOffIsByteNative

Both switches off is the native rule: at seventy levels over Tiger Girl
the threshold is -180, every prepared drop is refused, no ticket.
================
*/
func TestUniqueRulesOffIsByteNative(t *testing.T) {
	rt, c, target := uniqueKillFixture(t, "MOB_CH_TIGERWOMAN", 20, 3, 90)
	rt.UniqueRules = progression.UniqueRules{}
	if drops := planUniqueKill(rt, c, target); len(drops) != 0 {
		t.Fatalf("native level-gap kill left %d drops, want 0: %+v", len(drops), drops)
	}
}

/*
================
TestUniquePopTicketWithoutGapExemptLeavesOnlyTicket

The ticket is guaranteed by its own switch: without the gap exemption
the rest of the cross-level kill is still refused natively.
================
*/
func TestUniquePopTicketWithoutGapExemptLeavesOnlyTicket(t *testing.T) {
	rt, c, target := uniqueKillFixture(t, "MOB_CH_TIGERWOMAN", 20, 3, 90)
	rt.UniqueRules = progression.UniqueRules{PopTicket: true}
	drops := planUniqueKill(rt, c, target)
	if len(drops) != 1 || drops[0].Codename != gacha.CardCodename {
		t.Fatalf("ticket-only kill left %+v, want the single guaranteed ticket", drops)
	}
}

/*
================
TestBoneLordTicketCountTogether

The Bone Lord's native assigned row is two probability-1.0 cards. At an
even level both are admitted natively (ticket switch off: two cards);
with the ticket switch on they are retired into exactly one.
================
*/
func TestBoneLordTicketCountTogether(t *testing.T) {
	rt, c, target := uniqueKillFixture(t, "MOB_TK_BONELORD", 90, 3, 90)
	rt.UniqueRules = progression.UniqueRules{}
	if items, total := countDrops(planUniqueKill(rt, c, target), gacha.CardCodename); items != 2 || total != 2 {
		t.Fatalf("native Bone Lord kill left %d cards (total %d), want 2", items, total)
	}
	rt.UniqueRules = progression.UniqueRules{PopTicket: true}
	drops := planUniqueKill(rt, c, target)
	if items, total := countDrops(drops, gacha.CardCodename); items != 1 || total != 1 {
		t.Fatalf("ticket-rule Bone Lord kill left %d cards (total %d), want exactly 1", items, total)
	}
	if scrolls, _ := countDrops(drops, "ITEM_ETC_SCROLL_RETURN_02"); scrolls == 0 {
		t.Fatal("ticket rule disturbed the rest of the even-level kill")
	}
}

/*
================
TestDemonSameLevelKillUnchanged

Regression: a same-level unique (the Demon Shaitan shape, level 90
against 90) already admitted everything natively, which is why only
Demon dropped before the hunting patch. Both-off leaves that kill
exactly as native, and the ticket switch only adds its one card.
================
*/
func TestDemonSameLevelKillUnchanged(t *testing.T) {
	rt, c, target := uniqueKillFixture(t, "MOB_RM_DEMON", 90, 3, 90)
	rt.UniqueRules = progression.UniqueRules{}
	drops := planUniqueKill(rt, c, target)
	if scrolls, _ := countDrops(drops, "ITEM_ETC_SCROLL_RETURN_02"); scrolls == 0 {
		t.Fatal("same-level unique kill lost its native drops")
	}
	if items, _ := countDrops(drops, gacha.CardCodename); items != 0 {
		t.Fatalf("native same-level kill left %d cards, want 0 (no native row, no ticket)", items)
	}
	rt.UniqueRules = progression.UniqueRules{PopTicket: true}
	if items, total := countDrops(planUniqueKill(rt, c, target), gacha.CardCodename); items != 1 || total != 1 {
		t.Fatalf("ticket-rule same-level kill left %d cards (total %d), want exactly 1", items, total)
	}
}

/*
================
TestAdmitMonsterDropUniqueGapExemption

The gate itself: the exemption admits a unique at any gap with any
roll, never touches an ordinary monster, and off is the native -180.
================
*/
func TestAdmitMonsterDropUniqueGapExemption(t *testing.T) {
	rt, _, _, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	rt.DropRoll = constantDropRoll(0)
	target.Ref.MonsterType = 3
	target.Nest.HasRarityOverride, target.Nest.RarityOverride = true, 3
	rt.UniqueRules = progression.UniqueRules{GapExempt: true}
	if !rt.admitMonsterDrop(90, target) {
		t.Fatal("gap-exempt unique refused a level-90 hunter")
	}
	target.Nest.RarityOverride = 0
	if rt.admitMonsterDrop(90, target) {
		t.Fatal("gap exemption admitted an ordinary monster")
	}
	target.Nest.RarityOverride = 3
	rt.UniqueRules = progression.UniqueRules{}
	if rt.admitMonsterDrop(90, target) {
		t.Fatal("native -180 threshold admitted roll 0")
	}
}
