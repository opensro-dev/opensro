/*
===========================================================================

skillhealingthreat_test.go - recovery threat, squad membership and damage credit

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
engageHealingSquad
================
*/
func engageHealingSquad(t *testing.T, rt *Runtime, gid, target uint32) {
	t.Helper()
	mover, ok := rt.Monsters.Mover(testDivision, gid)
	if !ok {
		t.Fatal("missing mover")
	}
	if mover.Mode() == monster.MoverSpawning {
		if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := mover.Transition(monster.MoverEventAggroAcquired, target); err != nil {
		t.Fatal(err)
	}
	if !rt.Monsters.CommitMover(testDivision, gid, mover) {
		t.Fatal("squad admission failed")
	}
}

/*
================
TestHealingThreatStructureRepairIncludesOverheal
================
*/
func TestHealingThreatStructureRepairIncludesOverheal(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	structure := monster.MonsterRef{RefObjID: 19536, TidWord: 0x2246, Structure: true, MaxHP: 500, ScaleDenom: 100}
	mobNest := monster.NestRow{SpawnPoint: mob.Spawn, MaxCount: 1, RetailEvidence: true}
	nest := mobNest
	nest.RefObjID = structure.RefObjID
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{mob.Ref.RefObjID: mob.Ref, structure.RefObjID: structure}, []monster.NestRow{mobNest, nest}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(clock.NowMs())
	var target monster.Instance
	for _, actor := range rt.Monsters.MaterializedInstances(testDivision) {
		if actor.Ref.Structure {
			target = actor
		} else {
			mob = actor
		}
	}
	if target.Gid == 0 {
		t.Fatal("missing repair target")
	}
	engageHealingSquad(t, rt, mob.Gid, target.Gid)
	skill := shippedOffense(t, "SKILL_FORT_REPAIR_KIT_01")
	if skill.Category != skillCategoryHealing {
		t.Fatal("repair category lost")
	}
	// A full structure is valid for a later pulse of an already accepted kit.
	out := rt.pulseStructureRepair(linkedpulse.Effect{Division: testDivision, SourceGID: enterworld.ObjectIDForCharacter(c)}, skill, target, clock.NowMs())
	if len(out.Broadcast) != 0 {
		t.Fatal("overheal published unchanged vitals")
	}
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	want := int32(target.EffectiveMaxHP()*skill.StructureRepair.HPPercent/100) / 2
	if after.Opponents[1].GID != enterworld.ObjectIDForCharacter(c) || after.Opponents[1].Aggression != want || after.Opponents[1].Damage != 0 {
		t.Fatalf("repair threat %+v want %d", after.Opponents, want)
	}
}

/*
================
TestHealingThreatPreservesZeroHalfAndLinkedSource
================
*/
func TestHealingThreatPreservesZeroHalfAndLinkedSource(t *testing.T) {
	for _, amount := range []int64{1, 201} {
		rt, clock, c, mob := newCombatTestRuntime(t, 100000)
		friend := *c
		friend.ID, friend.Name = 4, "healing-link-source"
		rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {c, &friend}}
		caster, source := enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(&friend)
		link := statuseffect.Link{DivisionID: testDivision, SourceName: friend.Name, TargetName: c.Name,
			SourceGID: source, TargetGID: caster, SourceToken: 100, TargetToken: 101, SkillID: 7246, SkillGroup: 418,
			Group: 3, MaxOutgoing: 2, MaxDistance: 1500, ThreatPercent: 36, ExpiresAtMs: clock.NowMs() + 10000, ClientCancelable: true}
		if code := rt.effects.ApplyLink(link); code != 0 {
			t.Fatal(code)
		}
		engageHealingSquad(t, rt, mob.Gid, caster)
		rt.publishSkillHealingThreat(skillHealingThreat{division: testDivision, caster: caster,
			recipient: caster, category: skillCategoryHealing, amount: amount}, clock.NowMs())
		after, _ := rt.Monsters.Get(testDivision, mob.Gid)
		want := map[uint32]int32{caster: 0, source: 0}
		if amount == 201 {
			want[caster], want[source] = 64, 36
		}
		for _, opponent := range after.Opponents {
			value, found := want[opponent.GID]
			if !found || opponent.Aggression != value || opponent.Damage != 0 || opponent.HitCount != 1 {
				t.Fatalf("amount %d: linked healing event %+v", amount, after.Opponents)
			}
			delete(want, opponent.GID)
		}
		if len(want) != 0 {
			t.Fatal("missing source or caster event")
		}
	}
}

/*
================
TestHealingThreatAuthoredTargetPartyAndPulse
================
*/
func TestHealingThreatAuthoredTargetPartyAndPulse(t *testing.T) {
	for _, code := range []string{
		"SKILL_EU_CLERIC_HEALA_TARGET_A_01",
		"SKILL_EU_CLERIC_HEALA_GROUP_A_01",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01",
		"SKILL_EU_CLERIC_HEALA_CYCLE_B_01",
	} {
		t.Run(code, func(t *testing.T) {
			skill := shippedOffense(t, code)
			affordable(&skill)
			if skill.Category != skillCategoryHealing {
				t.Fatal("authored healing category lost")
			}
			p := newSupportParty(t, skill)
			at := p.m.World.Spawn
			actors := p.rt.Monsters.InstancesInRegions(testDivision, []uint16{uint16(*at.RegionID)})
			if len(actors) == 0 {
				t.Fatal("no fixture monster")
			}
			mob := actors[0]
			recipient := p.m
			if skill.Aura.Eshp {
				recipient = p.c
			}
			engageHealingSquad(t, p.rt, mob.Gid, enterworld.ObjectIDForCharacter(recipient))
			cast := wire.SkillAction{ActionId: skill.ID}
			if skill.TargetRequired {
				cast.HasTarget, cast.TargetGid = true, enterworld.ObjectIDForCharacter(p.m)
			}
			p.castReleased(t, skill, cast)
			if skill.Aura.Eshp {
				p.rt.advancePartyAuras(p.clock.NowMs())
			}
			if skill.Recovery.HealOverTimePinned {
				before, _ := p.rt.Monsters.Get(testDivision, mob.Gid)
				if before.Opponents[0].Aggression != 0 {
					t.Fatal("timed heal generated threat before its pulse")
				}
				p.clock.Advance(time.Duration(int64(skill.ActionCastingTimeMs)+1+int64(skill.Recovery.PulseMs)) * time.Millisecond)
				p.rt.advanceHealsOverTime(p.clock.NowMs())
			}
			after, _ := p.rt.Monsters.Get(testDivision, mob.Gid)
			var aggression int32
			for _, opponent := range after.Opponents {
				if opponent.Damage != 0 {
					t.Fatal("recovery generated damage credit")
				}
				if opponent.GID == enterworld.ObjectIDForCharacter(p.c) {
					aggression += opponent.Aggression
				}
			}
			if aggression == 0 {
				t.Fatalf("recipient healing did not credit caster threat: %+v", after.Opponents)
			}
		})
	}
}

/*
================
TestHealingThreatActorClasses
================
*/
func TestHealingThreatActorClasses(t *testing.T) {
	for _, tc := range []struct {
		tid         uint16
		alive, want bool
	}{
		{0xc6, true, true}, {0xc6, false, true}, {0x1c6, true, false},
		{0xa46, true, true}, {0xa46, false, false}, {0x1a46, false, true},
		{0x2246, false, true}, {0x1246, true, false}, {0x1846, true, false},
	} {
		if got := healingThreatActor(tc.tid, tc.alive); got != tc.want {
			t.Fatalf("type %04x alive %v: %v want %v", tc.tid, tc.alive, got, tc.want)
		}
	}
}

/*
================
TestHealingThreatUsesRecipientSquadAndNeverDamageCredit
================
*/
func TestHealingThreatUsesRecipientSquadAndNeverDamageCredit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		category    uint8
		amount      int64
		engaged     bool
		otherTarget bool
		want        int32
	}{
		{"healing-odd", skillCategoryHealing, 101, true, false, 50},
		{"healing-even", skillCategoryHealing, 100, true, false, 50},
		{"non-healing", 'D', 100, true, false, 0},
		{"zero", skillCategoryHealing, 0, true, false, 0},
		{"idle", skillCategoryHealing, 100, false, false, 0},
		{"different-recipient", skillCategoryHealing, 100, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, mob := newCombatTestRuntime(t, 100000)
			gid := enterworld.ObjectIDForCharacter(c)
			if tc.engaged {
				target := gid
				if tc.otherTarget {
					target++
				}
				engageHealingSquad(t, rt, mob.Gid, target)
			}
			rt.publishSkillHealingThreat(skillHealingThreat{division: testDivision, caster: gid,
				recipient: gid, category: tc.category, amount: tc.amount}, clock.NowMs())
			after, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if after.CurrentHP != mob.CurrentHP {
				t.Fatal("healing threat changed monster HP")
			}
			var aggression int32
			for _, opponent := range after.Opponents {
				if opponent.Damage != 0 {
					t.Fatal("healing granted damage credit", opponent)
				}
				if opponent.GID == gid {
					aggression += int32(opponent.Aggression)
				}
			}
			if aggression != tc.want {
				t.Fatalf("aggression=%d, want %d", aggression, tc.want)
			}
		})
	}
}

/*
================
TestHealingThreatAtReleaseIncludesOverheal
================
*/
func TestHealingThreatAtReleaseIncludesOverheal(t *testing.T) {
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	// This fixture isolates category dispatch on the existing self-recovery
	// route; its authored flat recovery is 89 and the gauge is already full.
	skill.Category = skillCategoryHealing
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentHP = testInt64(enterworld.DerivedMaxHP(c))
	c.CurrentMP = testInt64(int64(skill.Consumption.MP))
	gid := enterworld.ObjectIDForCharacter(c)
	engageHealingSquad(t, rt, mob.Gid, gid)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if before.Opponents[0].Aggression != 0 {
		t.Fatal("preparation generated threat")
	}
	rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.Opponents[0].GID != gid || after.Opponents[0].Aggression != 44 || after.Opponents[0].Damage != 0 {
		t.Fatalf("overheal threat=%+v", after.Opponents)
	}
	if *c.CurrentHP != enterworld.DerivedMaxHP(c) {
		t.Fatal("overheal exceeded gauge")
	}
}
