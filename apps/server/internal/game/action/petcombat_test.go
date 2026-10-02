/*
===========================================================================

petcombat_test.go - an attack pet fights the monster its owner orders

The owner's COS attack order (tag 2) puts an attack pet into BATTLE: it
closes in, strikes through the shared monster HP door under its own GID,
and the kill is the owner's. The follow order leaves BATTLE.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
newPetCombatRuntime

The combat fixture's owner and Mangnyang with a summoned attack pet whose
only authored action is the fixture's instant attack (skill 2).
================
*/
func newPetCombatRuntime(t *testing.T, monsterHP uint32, band uint16) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, m := newCombatTestRuntime(t, monsterHP)
	ref := equipCombatTestPet(t, rt, c, band)
	ref.Parameters.DefaultSkillIDs[0] = 2
	ref.Parameters.HitRate = 10_000
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 40, 40, 100
	skills[2] = skill
	rt.Now = clock.Now
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.BindPetSession(testDivision, c, 1)
	rt.TickHook()(clock.NowMs())
	return rt, clock, c, m
}

/*
================
petAttackOrder
================
*/
func petAttackOrder(cosGID, target uint32) []byte {
	return wire.NewWriter(9).U32(cosGID).U8(wire.CosCommandAttackTag).U32(target).Payload()
}

/*
================
tickPetCombat

Advance the action tick in 100 ms steps until done reports true, collecting
every frame, so approach and strike happen on the pet's own tick.
================
*/
func tickPetCombat(t *testing.T, rt *Runtime, clock *fakeClock, steps int, done func() bool) []simulation.Frame {
	t.Helper()
	var frames []simulation.Frame
	for range steps {
		clock.now = clock.now.Add(100 * time.Millisecond)
		for _, batch := range rt.TickHook()(clock.NowMs()) {
			frames = append(frames, batch.Frames...)
		}
		if done() {
			return frames
		}
	}
	t.Fatal("pet combat did not finish")
	return nil
}

/*
================
TestAttackPetKillsOrderedMonsterForItsOwner
================
*/
func TestAttackPetKillsOrderedMonsterForItsOwner(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	petGID := c.ActiveCOS.GID
	var credited []string
	rt.UpdateExperience = func(recipient *enterworld.Character, exp, sexp int64, source uint32) ([]wire.Frame, bool) {
		credited = append(credited, recipient.Name)
		return nil, true
	}
	if result := rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid)); len(result.Frames) != 0 {
		t.Fatalf("the order itself answers nothing: %+v", result.Frames)
	}
	frames := tickPetCombat(t, rt, clock, 100, func() bool {
		live, ok := rt.Monsters.Get(testDivision, m.Gid)
		return !ok || live.CurrentHP == 0
	})
	struck := 0
	for _, frame := range frames {
		if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 10 && frame.Payload[0] == 1 &&
			binary.LittleEndian.Uint32(frame.Payload[6:]) == petGID {
			struck++
		}
	}
	if struck == 0 {
		t.Fatal("no cast was published under the pet's GID")
	}
	if len(credited) != 1 || credited[0] != c.Name {
		t.Fatalf("the kill credited %v, want the owner %q", credited, c.Name)
	}
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.combat != nil {
		t.Fatal("the pet stayed in BATTLE after its target died")
	}
}

/*
================
TestFollowOrderLeavesBattle
================
*/
func TestFollowOrderLeavesBattle(t *testing.T) {
	rt, _, c, m := newPetCombatRuntime(t, 1_000_000, attackPetBand)
	petGID := c.ActiveCOS.GID
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state := rt.petSessionFor(testDivision, c.Name, petGID); state == nil || state.combat == nil || state.combat.target != m.Gid {
		t.Fatal("the attack order did not enter BATTLE")
	}
	follow := wire.NewWriter(5).U32(petGID).U8(wire.CosCommandFollowTag).Payload()
	rt.HandleCosCommand(testDivision, c, follow)
	if state := rt.petSessionFor(testDivision, c.Name, petGID); state.combat != nil {
		t.Fatal("the follow order did not leave BATTLE")
	}
}

/*
================
TestOnlyAttackPetsTakeTheAttackOrder

A pickup pet (band 4) has no BATTLE; an unknown target is dropped.
================
*/
func TestOnlyAttackPetsTakeTheAttackOrder(t *testing.T) {
	rt, _, c, m := newPetCombatRuntime(t, 1_000_000, 4)
	petGID := c.ActiveCOS.GID
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state := rt.petSessionFor(testDivision, c.Name, petGID); state != nil && state.combat != nil {
		t.Fatal("a pickup pet entered BATTLE")
	}
	rt, _, c, _ = newPetCombatRuntime(t, 1_000_000, attackPetBand)
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, 0x7fff_fff0))
	if state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID); state != nil && state.combat != nil {
		t.Fatal("an order naming no monster entered BATTLE")
	}
}

/*
================
TestUnsummonedPetLeavesBattle
================
*/
func TestUnsummonedPetLeavesBattle(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 1_000_000, attackPetBand)
	petGID := c.ActiveCOS.GID
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	c.ActiveCOS.Summoned = false
	clock.now = clock.now.Add(100 * time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if state := rt.petSessionFor(testDivision, c.Name, petGID); state != nil && state.combat != nil {
		t.Fatal("an unsummoned pet kept its BATTLE target")
	}
}
