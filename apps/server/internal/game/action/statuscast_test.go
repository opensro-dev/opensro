/*
===========================================================================

statuscast_test.go - damage-free hostile status casts through the offense owner

The Wizard's Lightning Shock, Root and Mesh Root prepare, release one
zero-damage record and leave only the authored status and aggression.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestStatusCastAppliesStatusWithoutDamage

Force the roll so the status must land and survive the tick that released
it; the monster keeps its HP and records the caster with the authored
aggression and no damage credit.
================
*/
func TestStatusCastAppliesStatusWithoutDamage(t *testing.T) {
	cases := []struct {
		code   string
		tag    uint32
		status abnormal.Status
	}{
		{"SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_A_01", 0x6665, abnormal.Fear},
		{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_A_01", 0x7274, abnormal.Root},
		{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_B_01", 0x7274, abnormal.Root},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 1000000)
			skill := shippedOffense(t, tc.code)
			if !skill.StatusCast || skill.Attack.Present || skill.ActionCastingTimeMs == 0 {
				t.Fatalf("catalog shape: status=%v attack=%v cast=%d refusal=%q", skill.StatusCast, skill.Attack.Present, skill.ActionCastingTimeMs, skill.OffenseRefusal)
			}
			index, _ := abnormal.SourceIndex(tc.tag)
			skill.Abnormal.Params[index].Args[1] = 100
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.RaceIndex = testInt64(enterworld.RaceEurope)
			c.ModelCodename = "CHAR_EU_MAN_NOBLE"
			c.Skills = []uint32{skill.ID}
			c.Intellect = testInt64(2000)
			c.CurrentMP = testInt64(10000)
			// Equip the authored Wizard weapon (staff or wand).
			weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
			c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			rt.CombatRoll = func() (uint32, error) { return 10, nil }
			before, _ := rt.Monsters.Get(testDivision, target.Gid)
			mp := enterworld.CurrentMP(c)

			cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}
			start := rt.HandleTargetInteract(testDivision, c, cast.Encode())
			start = assertAndSeparateActionSession(t, start)
			if len(start.Frames) == 0 || len(rt.pendingProjectileCasts) != 1 {
				t.Fatalf("status cast was not prepared: %+v", start)
			}
			// The world tick samples its instant before the registry clock
			// admits the status, then updates abnormals at that same instant.
			tick := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
			clock.Advance(time.Duration(tick-clock.NowMs()+5) * time.Millisecond)
			released := rt.advanceProjectileCasts(tick)
			if len(released) == 0 {
				t.Fatal("prepared status cast never released")
			}
			rt.advanceMonsterAbnormals(tick)

			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if after.CurrentHP != before.CurrentHP {
				t.Fatalf("status cast dealt damage: HP %d -> %d", before.CurrentHP, after.CurrentHP)
			}
			if after.Abnormal == nil || !after.Abnormal.Slots[tc.status].Active {
				t.Fatalf("status %v did not land", tc.status)
			}
			if len(after.Opponents) == 0 || after.Opponents[0].GID != enterworld.ObjectIDForCharacter(c) ||
				after.Opponents[0].Damage != 0 || after.Opponents[0].Aggression < int32(skill.Threat.Flat) {
				t.Fatalf("aggression ledger %+v, authored %d", after.Opponents, skill.Threat.Flat)
			}
			if enterworld.CurrentMP(c) >= mp {
				t.Fatalf("MP not debited: %d -> %d", mp, enterworld.CurrentMP(c))
			}
		})
	}
}

/*
================
TestStatusCastCatalogShape

Caster-centered rows have no release owner without a primary and stay
refused; programs that also carry att remain ordinary attacks.
================
*/
func TestStatusCastCatalogShape(t *testing.T) {
	source := shippedSkillSource(t)
	impact, ok := source.SkillByCodename("SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_B_01")
	if !ok || impact.StatusCast || impact.DirectOffensePinned {
		t.Fatalf("untargeted Lightning Impact admitted: %+v", impact.OffenseRefusal)
	}
	bolt, ok := source.SkillByCodename("SKILL_EU_WIZARD_COLDA_POINT_A_01")
	if !ok || bolt.StatusCast || !bolt.Attack.Present || !bolt.DirectOffensePinned {
		t.Fatalf("ordinary attack reclassified: status=%v", bolt.StatusCast)
	}
	shock, _ := source.SkillByCodename("SKILL_EU_WIZARD_PSYCHICA_UNTOUCH_A_01")
	if _, executable := enterworld.OffensiveSequence(source, shock.ID); !executable || shock.Threat.Flat != 155 || shock.Threat.Percent != 0 {
		t.Fatalf("Lightning Shock plan: executable=%v threat=%+v", executable, shock.Threat)
	}
	mesh, _ := source.SkillByCodename("SKILL_EU_WIZARD_EARTHA_ABNORMAL_B_01")
	if mesh.OffensiveArea.Shape != 2 || mesh.OffensiveArea.Radius != 50 || mesh.OffensiveArea.MaxTargets != 3 {
		t.Fatalf("Mesh Root area %+v", mesh.OffensiveArea)
	}
}
