/*
===========================================================================

combatcopy_screammask_test.go - the Scream Mask roots keep the combat copy

The Scream Mask answer detaches twice: the masked member's copy (the
monster and player branches) and the attacker's admitted view (the
player branch). Both take the narrow combat copy; these runs prove it
by corrupting one named character's equipment band on its copy, which
must refuse the roll, and by a differential against the full copy.
Split from combatcopy_test.go, which owns the shared copy helpers.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
screamCopyKind

Which copy the scream-mask runs take: the production combat copy,
the full copy through the seam, or the combat copy with one named
character's equipment band replaced by a row the item source cannot
resolve.
================
*/
const (
	screamCopyNarrow = iota
	screamCopyFull
	screamCopyMemberCorrupt
	screamCopyAttackerCorrupt
)

/*
================
screamMaskInstallLink

Puts the warlock's shipped Scream Mask link on the member, as the
buff install does in production.
================
*/
func screamMaskInstallLink(t *testing.T, rt *Runtime, skill enterworld.SkillRow, warlock, member *enterworld.Character, now int64) {
	t.Helper()
	l := statuseffect.Link{DivisionID: testDivision, SourceName: warlock.Name, TargetName: member.Name,
		SourceGID: enterworld.ObjectIDForCharacter(warlock), TargetGID: enterworld.ObjectIDForCharacter(member),
		SourceToken: 100, TargetToken: 101, SkillID: skill.ID, SkillGroup: skill.Group, MaxOutgoing: 2, MaxDistance: 1500,
		ScreamRange: skill.TimedEffect.Link.ScreamRange, ExpiresAtMs: now + 60000}
	if code := rt.effects.ApplyLink(l); code != 0 {
		t.Fatal(code)
	}
}

/*
================
screamMaskArmCopy

Arms the copy seam for one scream-mask run. The corrupt legs hand
the named character a copy whose only equipment row the item source
cannot resolve, so the stats funnel fails on that copy and the roll
must refuse: the band, not the bag, is what these roots size off.
================
*/
func screamMaskArmCopy(rt *Runtime, copyKind int, memberName, attackerName string) {
	switch copyKind {
	case screamCopyFull:
		forceFullCombatCopy(rt)
	case screamCopyMemberCorrupt:
		screamMaskCorruptCopy(rt, memberName)
	case screamCopyAttackerCorrupt:
		screamMaskCorruptCopy(rt, attackerName)
	}
}

/*
================
screamMaskCorruptCopy
================
*/
func screamMaskCorruptCopy(rt *Runtime, name string) {
	rt.combatCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
		view := character.SnapshotCombat()
		if character.Name == name {
			view.MissionInventory = []enterworld.InventoryRow{{Slot: 6, RefObjID: 0x7FFF_FFFF, Codename: "ITEM_MISSING_SCREAM"}}
		}
		return view
	}
}

/*
================
screamMaskMonsterRun

One Scream Mask answer to a monster striking the masked member from
the member's own pose, and its end state: the published frames, the
attacker's stun and HP, the member's rows. The member's copy feeds
the roll's caster side (its level and getv modifiers through the
stats funnel) and the record's source identity.
================
*/
func screamMaskMonsterRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_STUNLINK_A_07")
	if !skill.TimedEffect.Pinned || !skill.TimedEffect.Link.Scream || skill.TimedEffect.Link.ScreamRange == 0 {
		t.Fatalf("Scream Mask not admitted: %+v", skill.TimedEffect.Link)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	warlock := nearbyCharacter(rt, c, 20, "warlock", 30)
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("fixture monster has no mover")
	}
	pose := mover.LivePoseAt(clock.NowMs(), nil)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})
	screamMaskInstallLink(t, rt, skill, warlock, c, clock.NowMs())
	loadCombatBag(c)
	screamMaskArmCopy(rt, copyKind, c.Name, "")
	out := rt.screamMaskMonster(testDivision, c, target, pose, clock.NowMs())
	live, _ := rt.Monsters.Get(testDivision, target.Gid)
	stunned := live.Abnormal != nil && live.Abnormal.Slots[abnormal.Stun].Active
	return fmt.Sprintf("%+v\n", out) + fmt.Sprintf("stunned=%v hp=%d rows=%s", stunned, live.CurrentHP, combatRowsView(c)), stunned
}

/*
================
screamMaskMonsterTrace

The table's adapter for screamMaskMonsterRun: the member snapshot
on the narrow copy must answer a monster's hit exactly as the full
copy answers it, frames and end state alike.
================
*/
func screamMaskMonsterTrace(t *testing.T, full bool) string {
	kind := screamCopyNarrow
	if full {
		kind = screamCopyFull
	}
	trace, stunned := screamMaskMonsterRun(t, kind)
	if !stunned {
		t.Fatal("the attacker was never stunned; the trace would agree vacuously")
	}
	return trace
}

/*
================
screamMaskPlayerRun

One Scream Mask answer to a player striking the masked member, and
its end state: the published frames, the attacker's stun and HP,
both characters' rows. This branch detaches twice per hit - the
member's snapshot (caster side) and the attacker's admitted view
(its spawn lookup, defender stats and the roll's victim side).
================
*/
func screamMaskPlayerRun(t *testing.T, copyKind int) (string, bool) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_STUNLINK_A_07")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	warlock := nearbyCharacter(rt, c, 20, "warlock", 30)
	attacker := nearbyCharacter(rt, c, 21, "attacker", 10)
	hp := *c.CurrentHP
	attacker.CurrentHP = &hp
	screamMaskInstallLink(t, rt, skill, warlock, c, clock.NowMs())
	loadCombatBag(c)
	loadCombatBag(attacker)
	screamMaskArmCopy(rt, copyKind, c.Name, attacker.Name)
	out := rt.screamMaskPlayer(testDivision, c, attacker, clock.NowMs(), rt.handlerDoors())
	block := rt.playerAbnormal(testDivision, attacker.Name)
	stunned := block != nil && block.Slots[abnormal.Stun].Active
	return fmt.Sprintf("%+v\n", out) + fmt.Sprintf("stunned=%v ahp=%d mrows=%s arows=%s", stunned, *attacker.CurrentHP, combatRowsView(c), combatRowsView(attacker)), stunned
}

/*
================
screamMaskPlayerTrace

The table's adapter for screamMaskPlayerRun: the member snapshot
and the attacker view on the narrow copy must answer a player's
hit exactly as the full copies answer it.
================
*/
func screamMaskPlayerTrace(t *testing.T, full bool) string {
	kind := screamCopyNarrow
	if full {
		kind = screamCopyFull
	}
	trace, stunned := screamMaskPlayerRun(t, kind)
	if !stunned {
		t.Fatal("the attacker was never stunned; the trace would agree vacuously")
	}
	return trace
}

/*
================
TestScreamMaskCopiesReadTheirEquipmentBand

The seeing-eye for the scream-mask roots, shipped: with the armed
copies the attacker is stunned; hand any one of the three copies -
the member's snapshot in either branch, the attacker's admitted
view - an equipment band the item source cannot resolve and the
stats funnel fails on that copy, so no roll runs and nobody is
stunned. With the injected roll pinned to always proc, the chance
arithmetic cannot move; the funnel over the copy's band is the one
channel that can refuse. A root that took its stats from anything
but its copy - or lost the band on the way - fails here even
though the differential above still agrees.
================
*/
func TestScreamMaskCopiesReadTheirEquipmentBand(t *testing.T) {
	if _, stunned := screamMaskMonsterRun(t, screamCopyNarrow); !stunned {
		t.Fatal("monster branch: the attacker was not stunned off the armed member copy")
	}
	if _, stunned := screamMaskMonsterRun(t, screamCopyMemberCorrupt); stunned {
		t.Fatal("monster branch: the roll ran off a member copy with an unreadable equipment band")
	}
	if _, stunned := screamMaskPlayerRun(t, screamCopyNarrow); !stunned {
		t.Fatal("player branch: the attacker was not stunned off the armed copies")
	}
	if _, stunned := screamMaskPlayerRun(t, screamCopyAttackerCorrupt); stunned {
		t.Fatal("player branch: the roll ran off an attacker view with an unreadable equipment band")
	}
	if _, stunned := screamMaskPlayerRun(t, screamCopyMemberCorrupt); stunned {
		t.Fatal("player branch: the roll ran off a member copy with an unreadable equipment band")
	}
}
