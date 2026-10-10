/*
===========================================================================

screammask_test.go - Scream Mask stuns a monster that strikes the masked
member

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestScreamMaskStunsAnAttackerInsideItsRange

The member stands on the fixture monster and holds the Warlock's shipped
Scream Mask link. A monster striking from inside abnb's range is stunned
(the injected roll always procs) and takes no damage; from the range or
beyond, or once the link has ended, nothing is rolled.
================
*/
func TestScreamMaskStunsAnAttackerInsideItsRange(t *testing.T) {
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
	l := statuseffect.Link{DivisionID: testDivision, SourceName: warlock.Name, TargetName: c.Name,
		SourceGID: enterworld.ObjectIDForCharacter(warlock), TargetGID: enterworld.ObjectIDForCharacter(c),
		SourceToken: 100, TargetToken: 101, SkillID: skill.ID, SkillGroup: skill.Group, MaxOutgoing: 2, MaxDistance: 1500,
		ScreamRange: skill.TimedEffect.Link.ScreamRange, ExpiresAtMs: clock.NowMs() + 60000}
	if code := rt.effects.ApplyLink(l); code != 0 {
		t.Fatal(code)
	}
	stunned := func() bool {
		live, _ := rt.Monsters.Get(testDivision, target.Gid)
		return live.Abnormal != nil && live.Abnormal.Slots[abnormal.Stun].Active
	}
	// From exactly the range: strictly inside is required.
	edge := pose
	edge.X += float64(l.ScreamRange)
	if out := rt.screamMaskMonster(testDivision, c, target, edge, clock.NowMs()); len(out.Broadcast) != 0 || stunned() {
		t.Fatal("an attacker at the range was stunned")
	}
	out := rt.screamMaskMonster(testDivision, c, target, pose, clock.NowMs())
	if len(out.Broadcast) == 0 || !stunned() {
		t.Fatalf("the attacker was not stunned: %+v", out)
	}
	if live, _ := rt.Monsters.Get(testDivision, target.Gid); live.CurrentHP != target.CurrentHP {
		t.Fatalf("the attacker took damage: %d -> %d", target.CurrentHP, live.CurrentHP)
	}
	// An ended link rolls nothing.
	if _, ok := rt.effects.ScreamLink(testDivision, c.Name, l.ExpiresAtMs+1); ok {
		t.Fatal("an expired Scream Mask is still installed")
	}
}

/*
================
TestScreamMaskStunsAPlayerAttacker

The player branch: an attacking player beside the masked member is
stunned through its own door, with no damage.
================
*/
func TestScreamMaskStunsAPlayerAttacker(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skill := shippedOffense(t, "SKILL_EU_WARLOCK_SOULA_STUNLINK_A_07")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	warlock := nearbyCharacter(rt, c, 20, "warlock", 30)
	attacker := nearbyCharacter(rt, c, 21, "attacker", 10)
	hp := *c.CurrentHP
	attacker.CurrentHP = &hp
	l := statuseffect.Link{DivisionID: testDivision, SourceName: warlock.Name, TargetName: c.Name,
		SourceGID: enterworld.ObjectIDForCharacter(warlock), TargetGID: enterworld.ObjectIDForCharacter(c),
		SourceToken: 100, TargetToken: 101, SkillID: skill.ID, SkillGroup: skill.Group, MaxOutgoing: 2, MaxDistance: 1500,
		ScreamRange: skill.TimedEffect.Link.ScreamRange, ExpiresAtMs: clock.NowMs() + 60000}
	if code := rt.effects.ApplyLink(l); code != 0 {
		t.Fatal(code)
	}
	rt.screamMaskPlayer(testDivision, c, attacker, clock.NowMs())
	block := rt.playerAbnormal(testDivision, attacker.Name)
	if block == nil || !block.Slots[abnormal.Stun].Active {
		t.Fatal("the attacking player was not stunned")
	}
	if *attacker.CurrentHP != hp {
		t.Fatalf("the attacker took damage: %d -> %d", hp, *attacker.CurrentHP)
	}
}
