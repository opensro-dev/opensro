/*
===========================================================================

combatcopy_test.go - the combat roots read nothing from the bag (#1943)

The basic attack (engage and every continuation tick) and the projectile
release take the narrow combat copy: equipment rows and the avatar
inventory, no bag rows. Each scenario here runs twice on a bag loaded
with everything a root could trip on - spare ammunition, an attack pet's
summon row, a quest item - once on the narrow copy and once forced onto
the full copy, and every frame and the end state must agree. A root that
read a bag row through its copy would diverge.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
loadCombatBag

Fills c's bag with rows a bag reader would see: two spare arrow stacks,
an attack pet's summon row and a quest item.
================
*/
func loadCombatBag(c *enterworld.Character) {
	pet := &domain.CharacterCOS{GID: 0x7F00_0001, RefObjID: 3914, Codename: "COS_P_RAVEN", CurrentHP: 100, Summoned: true}
	c.MissionInventory = append(c.MissionInventory,
		enterworld.InventoryRow{Slot: 13, RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", StackCount: 250},
		enterworld.InventoryRow{Slot: 14, RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", StackCount: 250},
		enterworld.InventoryRow{Slot: 15, RefObjID: 3913, Codename: "ITEM_COS_P_RAVEN", StackCount: 1, Summon: pet},
		enterworld.InventoryRow{Slot: 16, RefObjID: 9999, Codename: "ITEM_QSP_TEST_01", StackCount: 1},
	)
}

/*
================
forceFullCombatCopy
================
*/
func forceFullCombatCopy(rt *Runtime) {
	rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		return rt.characterSnapshot(divisionID, character)
	}
}

/*
================
combatRowsView

The rows by value: a summon row prints its companion, not its pointer.
================
*/
func combatRowsView(c *enterworld.Character) string {
	view := ""
	for _, row := range c.MissionInventory {
		summon := domain.CharacterCOS{}
		if row.Summon != nil {
			summon = *row.Summon
		}
		view += fmt.Sprintf("{%d %s x%d dur=%d plus=%d summon=%+v}", row.Slot, row.Codename, row.StackCount, row.Durability, row.Plus, summon)
	}
	return view
}

/*
================
basicAttackTrace

An engage, its release and the continuation tick that strikes again,
on a loaded bag: every frame, then the target's HP and the attacker's
rows.
================
*/
func basicAttackTrace(t *testing.T, full bool) string {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	loadCombatBag(character)
	if full {
		forceFullCombatCopy(rt)
	}
	trace := fmt.Sprintf("%+v\n", rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()))
	for _, at := range []time.Duration{0, testBasicAttackActionDuration, testBasicAttackActionDuration + time.Millisecond} {
		trace += fmt.Sprintf("%+v\n", rt.TickHook()(clock.At(at).UnixMilli()))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(character))
}

/*
================
projectileTrace

A critical arrow from preparation to release, on a loaded bag.
================
*/
func projectileTrace(t *testing.T, full bool) string {
	rt, c, target, skill, now := arrowFixture(t)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
	start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now)
	trace := fmt.Sprintf("%+v %v\n", start, decision)
	for _, at := range []int64{now + 300, now + 971, now + 2000} {
		trace += fmt.Sprintf("%+v\n", rt.advanceProjectileCasts(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target)
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
TestCombatRootsReadNoBagRowsFromTheirCopy
================
*/
func TestCombatRootsReadNoBagRowsFromTheirCopy(t *testing.T) {
	for _, c := range []struct {
		name  string
		trace func(*testing.T, bool) string
	}{{"basic attack", basicAttackTrace}, {"projectile release", projectileTrace}} {
		narrow, full := c.trace(t, false), c.trace(t, true)
		a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				t.Errorf("%s: line %d diverged\nnarrow: %.900s\nfull:   %.900s", c.name, i, a[i], b[min(i, len(b)-1)])
				break
			}
		}
	}
}

/*
================
TestSnapshotCombatKeepsEquipmentAndAvatarOnly

The narrow copy's shape: the equipment rows and the whole avatar
inventory, no bag row.
================
*/
func TestSnapshotCombatKeepsEquipmentAndAvatarOnly(t *testing.T) {
	_, _, c, _ := newCombatTestRuntime(t, 100)
	loadCombatBag(c)
	c.AvatarInventory = &domain.AvatarInventory{Rows: []domain.InventoryRow{{Slot: 0, RefObjID: 7, Codename: "ITEM_AVATAR_TEST"}}}
	copy := c.SnapshotCombat()
	for _, row := range copy.MissionInventory {
		if row.Slot >= 13 {
			t.Fatalf("bag row %d in the combat copy", row.Slot)
		}
	}
	if copy.AvatarInventory == nil || len(copy.AvatarInventory.Rows) != 1 {
		t.Fatalf("avatar inventory = %+v, want the one row", copy.AvatarInventory)
	}
	equipped := 0
	for _, row := range c.MissionInventory {
		if row.Slot < 13 {
			equipped++
		}
	}
	if len(copy.MissionInventory) != equipped {
		t.Fatalf("combat copy has %d rows, the character wears %d", len(copy.MissionInventory), equipped)
	}
}
