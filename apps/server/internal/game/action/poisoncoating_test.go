/*
===========================================================================

poisoncoating_test.go - Rogue coating admission and impact lifecycle

Use shipped ranks through the same command, effect and damage owners used by
the live server. A poison coating must never invent elemental hit damage.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestPoisonCoatingCatalog
================
*/
func TestPoisonCoatingCatalog(t *testing.T) {
	source := shippedSkillSource(t)
	coatings, passives := 0, 0
	for id := uint32(1); id < 65536; id++ {
		row, ok := source.SkillByID(id)
		if !ok {
			continue
		}
		if strings.HasPrefix(row.Codename, "SKILL_EU_ROG_POISONA_BLADE_") {
			coatings++
			if !row.Imbue.Pinned || !row.Imbue.Poison || row.Imbue.Attack.Present || !row.EffectRider || !row.Abnormal.Present() {
				t.Fatalf("coating %s: %+v", row.Codename, row.Imbue)
			}
		}
		if strings.HasPrefix(row.Codename, "SKILL_EU_ROG_POIS_DAGP_MAINTAIN_") {
			passives++
			if !row.PassiveParameters.Pinned {
				t.Fatalf("duration passive missing: %s", row.Codename)
			}
		}
	}
	if coatings != 8 || passives != 5 {
		t.Fatalf("catalog coatings=%d duration passives=%d", coatings, passives)
	}
}

/*
================
poisonCoatingFixture

Keep the level-one combat data valid while equipping the Rogue weapon kind.
================
*/
func poisonCoatingFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow, monster.Instance) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 13
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	row := shippedOffense(t, "SKILL_EU_ROG_POISONA_BLADE_A_01")
	if !row.Imbue.Pinned {
		t.Fatalf("coating rejected: %+v", row)
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(1000)
	return rt, clock, c, row, target
}

/*
================
TestPoisonCoatingActivationAndImpact

The shared resolver carries the status on eligible hits without adding damage.
Expiry stops future procs without retiring a poison already on the victim.
================
*/
func TestPoisonCoatingActivationAndImpact(t *testing.T) {
	rt, clock, c, row, _ := poisonCoatingFixture(t)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpAttachedEffect); !ok {
		t.Fatalf("activation refused: %+v", out)
	}
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || effects[0].ExpiresAtMs-clock.NowMs() != int64(row.EffectDurationMs) {
		t.Fatalf("effect lifetime: %+v", effects)
	}
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	skill := rt.deps.SkillData().(staticSkillSource)[2]
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	active, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, stats, stats, clock.NowMs(), false)
	if err != nil {
		t.Fatal(err)
	}
	index, _ := abnormal.SourceIndex(0x7073)
	if active.Imbue.Params[index].Args != row.Abnormal.Params[index].Args {
		t.Fatal("poison rider missing")
	}
	expired, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, stats, stats, effects[0].ExpiresAtMs+1, false)
	if err != nil || expired.Imbue.Present() || active.Damage != expired.Damage {
		t.Fatalf("damage/lifetime: active=%+v expired=%+v err=%v", active, expired, err)
	}
	skill.Attack.Value5 = 0
	ineligible, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, stats, stats, clock.NowMs(), false)
	if err != nil || ineligible.Imbue.Present() {
		t.Fatal("ineligible attack applied coating", err)
	}
}

/*
================
TestPoisonCoatingDurationModifierAndCancellation

RPBU is added once to the coating timer and serialized to the client. The
poison victim's RPTU modifier is a separate parameter and cannot extend it.
================
*/
func TestPoisonCoatingDurationModifierAndCancellation(t *testing.T) {
	rt, clock, c, row, _ := poisonCoatingFixture(t)
	passive := shippedOffense(t, "SKILL_EU_ROG_POIS_DAGP_MAINTAIN_A_01")
	rt.deps.SkillData().(staticSkillSource)[passive.ID] = passive
	c.Skills = append(c.Skills, passive.ID)
	bonus := passive.PassiveParameters.Values[enterworld.ParameterPoisonCoatingDuration]
	if bonus == 0 {
		t.Fatal("missing authored RPBU bonus")
	}
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	frame, ok := findFrame(out.Frames, wire.OpAttachedEffect)
	if !ok || len(frame.Payload) != 16 {
		t.Fatalf("coating packet: %+v", out)
	}
	if got := binary.LittleEndian.Uint32(frame.Payload[12:]); got != bonus {
		t.Fatalf("wire duration rider=%d want=%d", got, bonus)
	}
	effect := rt.effects.Snapshot(testDivision, c.Name)[0]
	if effect.Rider != bonus || effect.ExpiresAtMs-clock.NowMs() != int64(row.EffectDurationMs+bonus) {
		t.Fatalf("duration modifier: %+v", effect)
	}
	rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: row.ID, InstanceToken: effect.InstanceToken}.Encode())
	if active, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.NowMs()); active.Pinned {
		t.Fatal("canceled coating can still proc")
	}
	rt.drainStoppedCharacterEffects()
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("canceled coating retained")
	}
}

/*
================
TestPoisonCoatingEquipmentAndLearnedAdmission
================
*/
func TestPoisonCoatingEquipmentAndLearnedAdmission(t *testing.T) {
	for _, mode := range []string{"unlearned", "wrong weapon", "dead", "targeted", "no MP"} {
		t.Run(mode, func(t *testing.T) {
			rt, _, c, row, _ := poisonCoatingFixture(t)
			request := wire.SkillAction{ActionId: row.ID}
			switch mode {
			case "unlearned":
				c.Skills = nil
			case "wrong weapon":
				rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename].TypeIDs[3] = 2
			case "dead":
				c.CurrentHP = testInt64(0)
			case "targeted":
				request.HasTarget = true
				request.TargetGid = enterworld.ObjectIDForCharacter(c)
			case "no MP":
				c.CurrentMP = testInt64(0)
			}
			before := enterworld.CurrentMP(c)
			rt.HandleTargetInteract(testDivision, c, request.Encode())
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || enterworld.CurrentMP(c) != before {
				t.Fatal("refused coating changed state")
			}
		})
	}
}

/*
================
TestPoisonCoatingVictimLifetimeAndModifiers

The coating and the poison have separate clocks. Both weapon branches use
the shared strike resolver and the victim's production abnormal-status tick.
================
*/
func TestPoisonCoatingVictimLifetimeAndModifiers(t *testing.T) {
	for _, weaponKind := range []int64{12, 13} {
		t.Run(fmt.Sprint(weaponKind), func(t *testing.T) {
			rt, clock, c, row, target := poisonCoatingFixture(t)
			weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = weaponKind
			c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			code := "SKILL_EU_DAGGER_BASE_01"
			if weaponKind == 12 {
				code = "SKILL_EU_CROSSBOW_BASE_01"
				bolt := &enterworld.ItemRef{RefObjID: 62002, Codename: "ITEM_ETC_AMMO_BOLT_01", TypeIDs: [4]int64{3, 3, 4, 2}}
				rt.deps.ItemReferences().(staticItemSource)[bolt.Codename] = bolt
				c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
					Slot: 7, RefObjID: bolt.RefObjID, Codename: bolt.Codename, TypeFlags: bolt.TypeFlags(), StackCount: 10,
				})
			}
			attack := shippedOffense(t, code)
			rt.deps.SkillData().(staticSkillSource)[attack.ID] = attack
			c.Skills = append(c.Skills, attack.ID)
			passive := shippedOffense(t, "SKILL_EU_ROG_POIS_DAGP_POISON_A_01")
			rt.deps.SkillData().(staticSkillSource)[passive.ID] = passive
			c.Skills = append(c.Skills, passive.ID)
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			hit := rt.beginBasicAttack(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}, clock.NowMs())
			rt.advanceProjectileCasts(clock.NowMs() + int64(attack.ActionCastingTimeMs) + 1)
			poisoned, _ := rt.Monsters.Get(testDivision, target.Gid)
			if poisoned.Abnormal == nil {
				t.Fatalf("weapon impact did not apply poison: %+v", hit)
			}
			slot := poisoned.Abnormal.Slots[abnormal.Poison]
			if !slot.Active || slot.Level != 78 || slot.Param38 != 46 || slot.DurationMs != 78*1000 {
				t.Fatalf("poison modifier roles: %+v", slot)
			}
			rt.ClearCombatIntent(testDivision, c.Name)
			rt.TickHook()(clock.NowMs() + 1)
			ticked, _ := rt.Monsters.Get(testDivision, target.Gid)
			if got := poisoned.CurrentHP - ticked.CurrentHP; got != slot.Param38 {
				t.Fatalf("poison tick=%d want=%d", got, slot.Param38)
			}
			effect := rt.effects.Snapshot(testDivision, c.Name)[0]
			rt.TickHook()(effect.ExpiresAtMs + 1)
			retained, _ := rt.Monsters.Get(testDivision, target.Gid)
			if !retained.Abnormal.Slots[abnormal.Poison].Active {
				t.Fatal("coating expiry removed the victim's poison")
			}
			rt.TickHook()(slot.StartedAt + int64(slot.DurationMs) + 1)
			ended, _ := rt.Monsters.Get(testDivision, target.Gid)
			if ended.Abnormal != nil && ended.Abnormal.Slots[abnormal.Poison].Active {
				t.Fatal("poison outlived its own timer")
			}
		})
	}
}
