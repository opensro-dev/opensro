/*
===========================================================================

combatcopy_test.go - the combat roots read nothing from the bag (#1943)

The basic attack (engage and every continuation tick), the projectile
release, the periodic pulse, the pulse area and the skill-object owner
pass take the narrow combat copy: equipment rows and the avatar
inventory, no bag rows. The skill press takes the cast copy: the
combat rows plus the companion-carrying bag rows, because its flow
resolves companions through the copy. Each scenario here runs twice on a bag loaded
with everything a root could trip on - spare ammunition, an attack pet's
summon row, a quest item - once on the narrow copy and once forced onto
the full copy, and every frame and the end state must agree. A root that
read a bag row through its copy would diverge.

===========================================================================
*/
package action

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
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
forceFullCastCopy
================
*/
func forceFullCastCopy(rt *Runtime) {
	rt.castCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
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
	start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now, rt.handlerDoors())
	trace := fmt.Sprintf("%+v %v\n", start, decision)
	for _, at := range []int64{now + 300, now + 971, now + 2000} {
		trace += fmt.Sprintf("%+v\n", rt.advanceProjectileCasts(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target)
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
periodicTrace

A persistent attack's cast and two pulses on a loaded bag. The pulse
root takes the combat copy of the caster; the bag carries an attack
pet's summon row, so a pulse that resolved the caster's companions
through its copy would find none on the narrow root and diverge.
================
*/
func periodicTrace(t *testing.T, full bool) string {
	rt, clock, c, target := periodicFixture(t, 10000)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	trace := fmt.Sprintf("%+v\n", rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode()))
	for _, at := range []int64{clock.NowMs() + 2000, clock.NowMs() + 4000} {
		trace += fmt.Sprintf("%+v\n", rt.advancePeriodicEffects(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= 10000 {
		t.Fatal("periodic pulses never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
skillPressTrace

A persistent attack pressed through the real 0x72CD door and two
pulses, on a loaded bag. The press takes the cast copy; the bag
carries an attack pet's summon row, so a press reader that needed a
plain bag row would diverge, and one that resolved the caster's
companions through its copy still finds the raven on both copies.
================
*/
func skillPressTrace(t *testing.T, full bool) string {
	rt, clock, c, target := periodicFixture(t, 10000)
	loadCombatBag(c)
	if full {
		forceFullCastCopy(rt)
	}
	trace := fmt.Sprintf("%+v\n", rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode()))
	for _, at := range []int64{clock.NowMs() + 2000, clock.NowMs() + 4000} {
		trace += fmt.Sprintf("%+v\n", rt.advancePeriodicEffects(at))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP >= 10000 {
		t.Fatal("periodic pulses never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
TestCastPressReadsNoPlainBagRows
================
*/
func TestCastPressReadsNoPlainBagRows(t *testing.T) {
	narrow, full := skillPressTrace(t, false), skillPressTrace(t, true)
	a, b := strings.Split(narrow, "\n"), strings.Split(full, "\n")
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			t.Errorf("skill press: line %d diverged\nnarrow: %.900s\nfull:   %.900s", i, a[i], b[min(i, len(b)-1)])
			break
		}
	}
}

/*
================
pulseAreaTrace

A Soul Chaos instance installed directly, then two due pulses, on a
loaded bag. The pulse root takes the combat copy of the caster; the
bag carries an attack pet's summon row, so a pulse that resolved the
caster's companions through its copy would diverge on the narrow root.
================
*/
func pulseAreaTrace(t *testing.T, full bool) string {
	rt, _, c, row, mob := warlockFixture(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01")
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	now := rt.Now().UnixMilli()
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
		SkillGroup: row.Group, InstanceToken: 9, State: statuseffect.StateActive,
		StartedAtMs: now - 2000, DurationPresent: true, ExpiresAtMs: now + 14000}) {
		t.Fatal("Soul Chaos did not install")
	}
	rt.trackPulseArea(testDivision, c.Name)
	trace := fmt.Sprintf("%+v\n", rt.advancePulseAreas(now))
	trace += fmt.Sprintf("%+v\n", rt.advancePulseAreas(now+2000))
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.CurrentHP >= 100000 {
		t.Fatal("pulse-area pulses never landed; the trace would agree vacuously")
	}
	return trace + fmt.Sprintf("hp=%d rows=%s", after.CurrentHP, combatRowsView(c))
}

/*
================
TestPulseAreaReadsTheCombatCopy

Guard for the #1943 pulse-area slice: the pulse reads the caster's
combat copy, not the whole record. On this rig (bag bulked to 44 plain
rows past the combat fixture's own) one due pulse measures 59,288
allocated bytes through the combat copy and 64,383 through the former
full copy - the bag rows are the difference - so the bound sits
between them while leaving the narrow root headroom.
================
*/
func TestPulseAreaReadsTheCombatCopy(t *testing.T) {
	rt, _, c, row, _ := warlockFixture(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01")
	loadCombatBag(c)
	for slot := int64(20); slot < 60; slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 9999, Codename: "ITEM_QSP_TEST_01", StackCount: 1})
	}
	now := rt.Now().UnixMilli()
	if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
		SkillGroup: row.Group, InstanceToken: 9, State: statuseffect.StateActive,
		StartedAtMs: now - 2000, DurationPresent: true, ExpiresAtMs: now + 1000000}) {
		t.Fatal("Soul Chaos did not install")
	}
	rt.trackPulseArea(testDivision, c.Name)
	at := now
	pulse := func() {
		at += 2000
		if len(rt.advancePulseAreas(at)) == 0 {
			t.Fatal("pulse produced no frames")
		}
	}
	for i := 0; i < 10; i++ {
		pulse()
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 100; i++ {
		pulse()
	}
	runtime.ReadMemStats(&after)
	if got := (after.TotalAlloc - before.TotalAlloc) / 100; got > 62000 {
		t.Fatalf("pulse allocated %d bytes, want <= 62000 (whole-record copy back?)", got)
	}
}

/*
================
skillObjectTrace

A planted fire trap scanned until it explodes on the monster beside
its planter, on a loaded bag. The skill-object pass takes the combat
copy of the owner once per object per tick; the bag carries an attack
pet's summon row, so an object tick that resolved the owner's
companions through its copy would diverge on the narrow root.
================
*/
func skillObjectTrace(t *testing.T, full bool) string {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	loadCombatBag(c)
	if full {
		forceFullCombatCopy(rt)
	}
	before, _ := rt.Monsters.Get(testDivision, gid)
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	trace := fmt.Sprintf("%+v\n", rt.drainSkillFinalizes(release+int64(skill.ActionDurationMs)))
	trace += fmt.Sprintf("%+v\n", rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil))
	after, _ := rt.Monsters.Get(testDivision, gid)
	if after.CurrentHP >= before.CurrentHP || len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("trap never exploded; the trace would agree vacuously")
	}
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
	}{{"basic attack", basicAttackTrace}, {"projectile release", projectileTrace}, {"periodic pulse", periodicTrace}, {"pulse area", pulseAreaTrace}, {"skill object explosion", skillObjectTrace}} {
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
