/*
===========================================================================

combatcopy_supportcast_test.go - the support-cast walker keeps the combat copy

A support cast pressed out of reach defers behind an intent: every
tick weighs the caster's and the recipient's copies, and the
completion re-detaches the caster for the accept it lands in (a
heal, a guard, a Duplicate, a Discord Wave). All of them take the
narrow combat copy; these runs prove it by walking all four
families in on a loaded bag against the full copy, and by poisoning
one caster fact on the copies the walk and the completion read.
Split from combatcopy_test.go, which owns the shared copy helpers.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
supportCopyKind

Which copies the support-walk runs take: the production combat
copies, the full copies through the seam (armed before the press,
so the whole flow rides them), or the combat copies with one caster
fact poisoned (armed after the press, so only the walk and its
completion can read them).
================
*/
const (
	supportCopyNarrow = iota
	supportCopyFull
	supportCopyBandCorrupt
	supportCopyCasterGone
)

/*
================
supportWalkArmPoison

Arms the poisoned copy seams for one support-walk run: the caster's
copies either wear an equipment band the item source cannot resolve
or read as delete-pending.
================
*/
func supportWalkArmPoison(rt *Runtime, copyKind int, casterName string) {
	switch copyKind {
	case supportCopyBandCorrupt:
		rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			view := character.SnapshotCombat()
			if character.Name == casterName {
				view.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 0x7FFF_FFFF, Codename: "ITEM_MISSING_SUPPORT"}}
			}
			return view
		}
	case supportCopyCasterGone:
		rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			view := character.SnapshotCombat()
			if character.Name == casterName {
				view.DeletePending = true
			}
			return view
		}
	}
}

/*
================
supportWalkMoveFar

Stands other 200 from the caster, beyond every shipped support
row's reach, so the press defers behind a support intent.
================
*/
func supportWalkMoveFar(rt *Runtime, c, other *enterworld.Character, now int64) {
	origin := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, now)
	rt.Worlds.Update(simulation.WorldKey(testDivision, other.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(other) },
		func(w *simulation.WorldState) {
			w.Spawn.X = origin.X + 200
			w.SpawnSet = true
		})
}

/*
================
supportHealWalkRun

One far targeted heal on a loaded bag: the press defers, the tick
walks the caster in and completes the cast. Landed once the mate
is healed and the intent is drained.
================
*/
func supportHealWalkRun(t *testing.T, copyKind int) (string, bool) {
	heal := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_TARGET_A_01")
	affordable(&heal)
	p := newSupportPair(t, heal)
	p.placeMate(200)
	loadCombatBag(p.c)
	if copyKind == supportCopyFull {
		forceFullCombatCopy(p.rt)
	}
	press := p.cast(heal.ID)
	if len(p.rt.combatIntentSnapshot()) != 1 {
		t.Fatal("the far heal armed no support intent; the trace would agree vacuously")
	}
	supportWalkArmPoison(p.rt, copyKind, p.c.Name)
	p.clock.Advance(30 * time.Second)
	bursts := p.rt.TickHook()(p.clock.NowMs())
	p.finishCast(heal)
	trace := fmt.Sprintf("press=%+v bursts=%+v\n", press, bursts) +
		fmt.Sprintf("hp=%d cmp=%d left=%d crows=%s mrows=%s", *p.m.CurrentHP, enterworld.CurrentMP(p.c), len(p.rt.combatIntentSnapshot()), combatRowsView(p.c), combatRowsView(p.m))
	return trace, *p.m.CurrentHP > 1 && len(p.rt.combatIntentSnapshot()) == 0
}

/*
================
supportHealWalkTrace

The table's adapter for supportHealWalkRun.
================
*/
func supportHealWalkTrace(t *testing.T, full bool) string {
	kind := supportCopyNarrow
	if full {
		kind = supportCopyFull
	}
	trace, landed := supportHealWalkRun(t, kind)
	if !landed {
		t.Fatal("the walked heal never landed; the trace would agree vacuously")
	}
	return trace
}

/*
================
supportGuardWalkRun

One far Guard Intercept on a loaded bag: walked in and installed
on the ally (phase 2) by the completion's timed-target arm.
================
*/
func supportGuardWalkRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c := concealmentFixture(t, guardIntercept)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	loadCombatBag(c)
	supportWalkMoveFar(rt, c, ally, clock.NowMs())
	if copyKind == supportCopyFull {
		forceFullCombatCopy(rt)
	}
	press := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: guardIntercept, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode())
	if len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("the far guard armed no support intent; the trace would agree vacuously")
	}
	supportWalkArmPoison(rt, copyKind, c.Name)
	clock.Advance(30 * time.Second)
	bursts := rt.TickHook()(clock.NowMs())
	trace := fmt.Sprintf("press=%+v bursts=%+v\n", press, bursts) +
		fmt.Sprintf("allyfx=%v cmp=%d left=%d crows=%s", hasSkillEffect(rt, ally.Name, guardIntercept), enterworld.CurrentMP(c), len(rt.combatIntentSnapshot()), combatRowsView(c))
	return trace, hasSkillEffect(rt, ally.Name, guardIntercept) && len(rt.combatIntentSnapshot()) == 0
}

/*
================
supportGuardWalkTrace

The table's adapter for supportGuardWalkRun.
================
*/
func supportGuardWalkTrace(t *testing.T, full bool) string {
	kind := supportCopyNarrow
	if full {
		kind = supportCopyFull
	}
	trace, landed := supportGuardWalkRun(t, kind)
	if !landed {
		t.Fatal("the walked guard never landed; the trace would agree vacuously")
	}
	return trace
}

/*
================
supportDuplicateWalkRun

One far Duplicate on a loaded bag: walked in, and the caster takes
the ally's look through the completion's duplicate arm.
================
*/
func supportDuplicateWalkRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c, ally := duplicateFixture(t)
	loadCombatBag(c)
	supportWalkMoveFar(rt, c, ally, clock.NowMs())
	if copyKind == supportCopyFull {
		forceFullCombatCopy(rt)
	}
	press := castDuplicate(rt, c, ally)
	if len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("the far duplicate armed no support intent; the trace would agree vacuously")
	}
	supportWalkArmPoison(rt, copyKind, c.Name)
	clock.Advance(30 * time.Second)
	bursts := rt.TickHook()(clock.NowMs())
	trace := fmt.Sprintf("press=%+v bursts=%+v\n", press, bursts) +
		fmt.Sprintf("casterfx=%v cmp=%d left=%d crows=%s", hasSkillEffect(rt, c.Name, duplicateA1), enterworld.CurrentMP(c), len(rt.combatIntentSnapshot()), combatRowsView(c))
	return trace, hasSkillEffect(rt, c.Name, duplicateA1) && len(rt.combatIntentSnapshot()) == 0
}

/*
================
supportDuplicateWalkTrace

The table's adapter for supportDuplicateWalkRun.
================
*/
func supportDuplicateWalkTrace(t *testing.T, full bool) string {
	kind := supportCopyNarrow
	if full {
		kind = supportCopyFull
	}
	trace, landed := supportDuplicateWalkRun(t, kind)
	if !landed {
		t.Fatal("the walked duplicate never landed; the trace would agree vacuously")
	}
	return trace
}

/*
================
supportDiscordWalkRun

One far Discord Wave aimed at an ally on a loaded bag: walked in,
and the caster pays through the completion's discord arm. The wave
itself cuts no hostility here - no monster chases the ally - so the
charge is the landing witness.
================
*/
func supportDiscordWalkRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000)
	skill := shippedOffense(t, discordCode)
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = bardHarpKind
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	loadCombatBag(c)
	supportWalkMoveFar(rt, c, ally, clock.NowMs())
	mp := enterworld.CurrentMP(c)
	if copyKind == supportCopyFull {
		forceFullCombatCopy(rt)
	}
	press := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(ally)}.Encode())
	if len(rt.combatIntentSnapshot()) != 1 {
		t.Fatal("the far discord wave armed no support intent; the trace would agree vacuously")
	}
	supportWalkArmPoison(rt, copyKind, c.Name)
	clock.Advance(30 * time.Second)
	bursts := rt.TickHook()(clock.NowMs())
	trace := fmt.Sprintf("press=%+v bursts=%+v\n", press, bursts) +
		fmt.Sprintf("cmp=%d->%d left=%d crows=%s", mp, enterworld.CurrentMP(c), len(rt.combatIntentSnapshot()), combatRowsView(c))
	return trace, enterworld.CurrentMP(c) < mp && len(rt.combatIntentSnapshot()) == 0
}

/*
================
supportDiscordWalkTrace

The table's adapter for supportDiscordWalkRun.
================
*/
func supportDiscordWalkTrace(t *testing.T, full bool) string {
	kind := supportCopyNarrow
	if full {
		kind = supportCopyFull
	}
	trace, landed := supportDiscordWalkRun(t, kind)
	if !landed {
		t.Fatal("the walked discord wave never landed; the trace would agree vacuously")
	}
	return trace
}

/*
================
TestSupportWalkerCopiesReadTheCastFacts

The seeing-eye for the walker's caster copies, shipped: with the
armed copies all four walked casts land; hand the tick a caster
copy that reads as gone and the intent dies quietly instead - no
effect, no charge. A walker that weighed the live record or the
live caster's standing instead of its copy fails here even though
the differential above still agrees.
================
*/
func TestSupportWalkerCopiesReadTheCastFacts(t *testing.T) {
	for _, family := range []struct {
		name string
		run  func(*testing.T, int) (string, bool)
	}{{"heal", supportHealWalkRun}, {"guard", supportGuardWalkRun}, {"duplicate", supportDuplicateWalkRun}, {"discord", supportDiscordWalkRun}} {
		if _, landed := family.run(t, supportCopyNarrow); !landed {
			t.Fatalf("%s: the walked cast never landed off the armed copies", family.name)
		}
		if _, landed := family.run(t, supportCopyCasterGone); landed {
			t.Fatalf("%s: the cast landed off a caster copy that reads as gone", family.name)
		}
	}
}

/*
================
TestSupportWalkerCompletionReadsTheEquipmentBand

The seeing-eye for the completion's caster copies, shipped: with
the armed copies all four walked casts land; hand the completion
a caster copy whose only equipment row the item source cannot
resolve and every family refuses - no effect, no charge. The
accepts size their admission, weapon and stats reads off that
copy's worn band; a completion that read the live record instead
fails here even though the differential above still agrees.
================
*/
func TestSupportWalkerCompletionReadsTheEquipmentBand(t *testing.T) {
	for _, family := range []struct {
		name string
		run  func(*testing.T, int) (string, bool)
	}{{"heal", supportHealWalkRun}, {"guard", supportGuardWalkRun}, {"duplicate", supportDuplicateWalkRun}, {"discord", supportDiscordWalkRun}} {
		if _, landed := family.run(t, supportCopyNarrow); !landed {
			t.Fatalf("%s: the walked cast never landed off the armed copies", family.name)
		}
		if _, landed := family.run(t, supportCopyBandCorrupt); landed {
			t.Fatalf("%s: the cast landed off a caster copy with an unreadable equipment band", family.name)
		}
	}
}
