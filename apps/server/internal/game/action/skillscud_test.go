/*
===========================================================================

skillscud_test.go - the Rogue's Scud through the instant self-effect owner

Scud (SKILL_EU_ROG_DAGGERA_SPEED_A) is a dagger-only (13/255) hst2 speed
skill. It runs as an instant self effect like the Chinese GYEONGGONG rows,
but its weapon kinds are checked by the 58D480 phase the instant owner
answers itself, after the cooldown and before MP.

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	scudRank1Codename      = "SKILL_EU_ROG_DAGGERA_SPEED_A_01"
	scudDaggerItem         = "ITEM_EU_DAGGER_01_A_DEF"
	scudCrossbowItem       = "ITEM_EU_CROSSBOW_01_A_DEF"
	scudOpenAttackCodename = "SKILL_EU_ROG_DAGGERA_CHAIN_A_01" // a dagger-only (13/255) attack
	scudRank1Percent       = 48
	scudRank1MP            = 30
	scudTestMP             = 100 // below the level-1 fixture's maximum MP
)

/*
================
newScudTestRuntime

The level-1 combat fixture with shipped Scud rank 1 learned and the named
shipped weapon in the primary slot, plus the fixture's target monster.
================
*/
func newScudTestRuntime(t *testing.T, weaponCodename string) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow, monster.Instance) {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	row, ok := enterworld.NewTextdataSkills(dir).SkillByCodename(scudRank1Codename)
	if !ok || !row.InstantSelfEffectPinned || row.MovementModifier.Kind != statuseffect.MovementOverride {
		t.Fatalf("Scud not admitted as an instant override: instant %v movement %+v", row.InstantSelfEffectPinned, row.MovementModifier)
	}
	weapon, ok := enterworld.NewTextdataItems(dir).ItemRefByCodename(weaponCodename)
	if !ok {
		t.Fatalf("missing %s", weaponCodename)
	}
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	rt.deps.ItemReferences().(staticItemSource)[weapon.Codename] = weapon
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(scudTestMP)
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 6, RefObjID: weapon.RefObjID, Codename: weapon.Codename, TypeFlags: weapon.TypeFlags(),
		VarianceBits: "0", Durability: 50, StackCount: 1,
	}}
	return rt, clock, c, row, target
}

/*
================
requireScudRefusal

The cast answers with one refusal frame carrying the code's low byte, and
leaves MP and the effect registry untouched.
================
*/
func requireScudRefusal(t *testing.T, rt *Runtime, c *enterworld.Character, row enterworld.SkillRow, code byte) {
	t.Helper()
	before := *c.CurrentMP
	got := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(got.Frames) != 1 || !bytes.Equal(got.Frames[0].Payload, []byte{2, code}) {
		t.Fatalf("want refusal 0x30%02x, got %+v", code, got)
	}
	if *c.CurrentMP != before || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("refusal mutated authority")
	}
}

/*
================
TestScudWithDaggerOverridesMovementSpeed
================
*/
func TestScudWithDaggerOverridesMovementSpeed(t *testing.T) {
	rt, clock, c, row, _ := newScudTestRuntime(t, scudDaggerItem)
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	for _, f := range result.Frames {
		if f.Opcode == wire.OpSkillCastResult && f.Payload[0] != 1 {
			t.Fatal("cast refused", result)
		}
	}
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || !effects[0].Movement || effects[0].MovementKind != statuseffect.MovementOverride ||
		effects[0].MovementPercent != scudRank1Percent {
		t.Fatal("override not installed", effects, result)
	}
	if *c.CurrentMP != scudTestMP-scudRank1MP {
		t.Fatal("MP charge", *c.CurrentMP)
	}
	_, run := rt.EntryMovementSpeeds(testDivision, c.Name)
	want := float32(simulation.RunSpeed) * (1 + float32(scudRank1Percent)/100)
	if run != want {
		t.Fatal("run speed", run, want)
	}

	// dura(15000): the override retires on its own and the speed returns.
	clock.Advance(time.Duration(row.EffectDurationMs) * time.Millisecond)
	rt.TickHook()(clock.NowMs() + 1)
	_, run = rt.EntryMovementSpeeds(testDivision, c.Name)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || run != float32(simulation.RunSpeed) {
		t.Fatal("expiry did not restore speed", run)
	}
}

/*
================
TestScudRefusesAWeaponOtherThanADagger

58D480 answers 0x300D for a crossbow. 58D8F0 order: a cooling Scud reports
0x3005 before the weapon, and the weapon answers before MP (0x3004).
================
*/
func TestScudRefusesAWeaponOtherThanADagger(t *testing.T) {
	rt, clock, c, row, _ := newScudTestRuntime(t, scudCrossbowItem)
	requireScudRefusal(t, rt, c, row, 0x0d)

	c.CurrentMP = testInt64(0)
	requireScudRefusal(t, rt, c, row, 0x0d)

	c.CurrentMP = testInt64(scudTestMP)
	registerOffensiveCooldown(c, row, clock.NowMs())
	requireScudRefusal(t, rt, c, row, 0x05)
}

/*
================
TestScudWeaponPhaseSkipsPoisonCoatingReqi

The weapon phase Scud added judges weapon bytes only. A poison coating
carries reqi pairs (6 12 / 6 13) and keeps the admission it had before
Scud: with no weapon equipped the cast is accepted, charged and installed.
================
*/
func TestScudWeaponPhaseSkipsPoisonCoatingReqi(t *testing.T) {
	rt, _, c, row, _ := poisonCoatingFixture(t)
	if !row.Reqi.Present {
		t.Fatalf("%s lost its reqi pairs: %+v", row.Codename, row.Reqi)
	}
	c.MissionInventory = nil
	before := *c.CurrentMP
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpAttachedEffect); !ok {
		t.Fatalf("unarmed coating refused: %+v", out)
	}
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 || *c.CurrentMP >= before {
		t.Fatal("unarmed coating not committed exactly once", *c.CurrentMP)
	}
}

/*
================
TestScudPreservesOpenDaggerAttack

4AD870: the instant owner answers beside an open attack instead of
replacing it. Scud cast while a dagger attack is open installs the override
and leaves the attack and its combat intent in place.
================
*/
func TestScudPreservesOpenDaggerAttack(t *testing.T) {
	rt, _, c, row, target := newScudTestRuntime(t, scudDaggerItem)
	attack, ok := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t)).SkillByCodename(scudOpenAttackCodename)
	if !ok || attack.ActionCastingTimeMs != 0 {
		t.Fatalf("%s missing or no longer released at once", scudOpenAttackCodename)
	}
	// The attack pays its own MP; Scud then draws from the usual test gauge.
	c.CurrentMP = testInt64(int64(attack.Consumption.MP) + scudTestMP)
	rt.deps.SkillData().(staticSkillSource)[attack.ID] = attack
	c.Skills = append(c.Skills, attack.ID)
	open := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: attack.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	assertAndSeparateActionSession(t, open)
	intents := rt.combatIntentSnapshot()
	if len(intents) != 1 || !rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("dagger attack not open", open)
	}
	intent := intents[0]
	mp := *c.CurrentMP

	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if !rt.combatIntentIsCurrent(intent) || !rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("Scud replaced the open attack or its continuation")
	}
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || effects[0].MovementKind != statuseffect.MovementOverride || *c.CurrentMP != mp-scudRank1MP {
		t.Fatal("concurrent Scud not committed exactly once", effects, *c.CurrentMP, result)
	}
}
