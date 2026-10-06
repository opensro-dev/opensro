/*
===========================================================================

monsterstrike_test.go - monster strikes and their publication

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
privateFramesOf

Every recipient's private frames in order, for tests with one victim.
================
*/
func privateFramesOf(result simulation.MonsterAttackResult) []simulation.Frame {
	var out []simulation.Frame
	for _, recipient := range result.Private {
		out = append(out, recipient.Frames...)
	}
	return out
}

/*
================
areaPeer

A copy of the fixture character standing x units along the axis, with
its own world record, id and name.
================
*/
func areaPeer(c *enterworld.Character, id int64, name string, x float64) *enterworld.Character {
	peer := *c
	peer.ID, peer.Name, peer.ActiveCOS = id, name, nil
	hp, region, y, z, angle := *c.CurrentHP, *c.World.Spawn.RegionID, *c.World.Spawn.Y, *c.World.Spawn.Z, *c.World.Spawn.Angle
	peer.CurrentHP = &hp
	peer.World = &enterworld.CharacterWorld{Spawn: &enterworld.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle}, SpawnSet: true}
	return &peer
}

/*
================
TestMonsterAreaStrikesEveryVictimAtItsShare

A monster skill with an action area (efr kind 1, shape 1 around the
monster) strikes its target and every player inside the radius under one
area result; the second victim takes the reduced share, and a player
outside the area is untouched.
================
*/
func TestMonsterAreaStrikesEveryVictimAtItsShare(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent, skill.Attack.ImpactCount = 30, 30, 100, 1
	skill.ActionArea = enterworld.SkillOffensiveArea{Shape: 1, Radius: 20, MaxTargets: 3, ReductionPercent: 50, Select: 24}
	skills[2] = skill
	m.Ref.DefaultSkillIDs[0] = 2
	near := areaPeer(c, c.ID+1, "AreaNear", *c.World.Spawn.X+12)
	far := areaPeer(c, c.ID+2, "AreaFar", *c.World.Spawn.X+200)
	characters := fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)
	characters[testDivision] = append(characters[testDivision], near, far)
	before := enterworld.CurrentHP(c)
	result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted || len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("area attack refused: %+v", result)
	}
	primaryLoss, nearLoss := before-enterworld.CurrentHP(c), before-enterworld.CurrentHP(near)
	if primaryLoss == 0 || nearLoss == 0 || nearLoss >= primaryLoss {
		t.Fatalf("losses primary=%d near=%d, want the near victim at a reduced share", primaryLoss, nearLoss)
	}
	if enterworld.CurrentHP(far) != before {
		t.Fatal("a player outside the area was struck")
	}
	// The area result names both victims (target-major records, 8E0190).
	payload := result.Frames[0].Payload
	for _, gid := range []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)} {
		if !bytes.Contains(payload, binary.LittleEndian.AppendUint32(nil, gid)) {
			t.Fatalf("victim %d missing from the area result % X", gid, payload)
		}
	}
	if bytes.Contains(payload, binary.LittleEndian.AppendUint32(nil, enterworld.ObjectIDForCharacter(far))) {
		t.Fatal("the far player is named in the result")
	}
}

/*
================
TestMonsterAttackWithoutAreaStrikesOnlyItsTarget

A skill without an action area keeps the single-target result, however
close another player stands.
================
*/
func TestMonsterAttackWithoutAreaStrikesOnlyItsTarget(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent, skill.Attack.ImpactCount = 30, 30, 100, 1
	skills[2] = skill
	m.Ref.DefaultSkillIDs[0] = 2
	near := areaPeer(c, c.ID+1, "AreaNear", *c.World.Spawn.X+1)
	characters := fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)
	characters[testDivision] = append(characters[testDivision], near)
	before := enterworld.CurrentHP(near)
	if result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs()); !result.Accepted {
		t.Fatalf("attack refused: %+v", result)
	}
	if enterworld.CurrentHP(near) != before {
		t.Fatal("a single-target attack struck a bystander")
	}
}

/*
================
TestMonsterAreaStrikesACompanionInside

A summoned pet beside a player in the area is a victim too, judged on its
own record: its HP falls and the result names it.
================
*/
func TestMonsterAreaStrikesACompanionInside(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent, skill.Attack.ImpactCount = 30, 30, 100, 1
	skill.ActionArea = enterworld.SkillOffensiveArea{Shape: 1, Radius: 40, MaxTargets: 5, Select: 24}
	skills[2] = skill
	m.Ref.DefaultSkillIDs[0] = 2
	near := areaPeer(c, c.ID+1, "AreaOwner", *c.World.Spawn.X+10)
	characters := fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)
	characters[testDivision] = append(characters[testDivision], near)
	deps := rt.deps.(*enterworld.Deps)
	items := deps.Items.(staticItemSource)
	ref := equipCombatTestPet(t, rt, near, 3)
	// Keep the fixture's sword beside the pet reference.
	deps.Items = cosTestItemSource{staticItemSource: items, characters: map[string]*enterworld.CharacterRef{ref.Codename: ref}}
	rt.BindPetSession(testDivision, near, 101)
	rt.advancePets(clock.NowMs())
	before := near.ActiveCOS.CurrentHP
	result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted {
		t.Fatalf("area attack refused: %+v", result)
	}
	if near.ActiveCOS.CurrentHP >= before {
		t.Fatalf("pet hp %d, want below %d", near.ActiveCOS.CurrentHP, before)
	}
	if !bytes.Contains(result.Frames[0].Payload, binary.LittleEndian.AppendUint32(nil, near.ActiveCOS.GID)) {
		t.Fatal("the pet is missing from the area result")
	}
}

/*
================
TestGradedMonsterStrikeTakesTheNativeDamageScale

SkillCombat_GetMonsterDamageScale (5874D0) at 58FD05: a giant's (grade 4)
landed strike deals 1.5 times the damage the same strike from a normal
monster deals, truncated after the multiply.
================
*/
func TestGradedMonsterStrikeTakesTheNativeDamageScale(t *testing.T) {
	loss := func(rarity uint8) int64 {
		rt, clock, c, m := newCombatTestRuntime(t, 100)
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent, skill.Attack.ImpactCount = 20, 20, 100, 1
		skills[2] = skill
		m.Ref.DefaultSkillIDs[0] = 2
		m.Nest.HasRarityOverride, m.Nest.RarityOverride = true, rarity
		before := enterworld.CurrentHP(c)
		result := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
		if !result.Accepted {
			t.Fatalf("rarity %#x: strike refused: %+v", rarity, result)
		}
		return int64(before - enterworld.CurrentHP(c))
	}
	normal, giant := loss(0x00), loss(0x04)
	if normal == 0 {
		t.Fatal("the normal monster dealt no damage")
	}
	if want := normal * 3 / 2; giant != want {
		t.Fatalf("giant loss %d, want %d (1.5 x the normal %d)", giant, want, normal)
	}
}
