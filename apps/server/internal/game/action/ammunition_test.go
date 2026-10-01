/*
===========================================================================

ammunition_test.go - tests for ammunition.go and the ammo admission

The equipped arrow or bolt row as the basic attack and ranged skills see
it: the per-shot debit, the wrong-family refusal, and the 0x300E notice at
the press and on a pursuit's arrival.

===========================================================================
*/

package action

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestRangedBasicAttackConsumesRetailAmmunitionSocketAtomically
================
*/
func TestRangedBasicAttackConsumesRetailAmmunitionSocketAtomically(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[character.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	character.MissionInventory[0].TypeFlags = weapon.TypeFlags()

	arrow := &enterworld.ItemRef{
		RefObjID: 62_001,
		Codename: "ITEM_ETC_AMMO_ARROW_01",
		TypeIDs:  [4]int64{3, 3, 4, 1},
	}
	items[arrow.Codename] = arrow
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot:       7,
		RefObjID:   arrow.RefObjID,
		Codename:   arrow.Codename,
		TypeFlags:  arrow.TypeFlags(),
		StackCount: 2,
	})
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_CH_BOW_BASE_01"
	skill.RequiredWeaponKinds = [2]uint8{6, 0xff}
	skills[2] = skill

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if len(result.Frames) != 2 || result.Frames[1].Opcode != wire.OpAvatarInventorySlot7StackCount ||
		!bytes.Equal(result.Frames[1].Payload, []byte{1, 0}) {
		t.Fatalf("ranged actor frames = %+v, want B245 then private 3752 count=1", result.Frames)
	}
	assertSkillDamageOpen(t, result.Frames[:1], 2,
		enterworld.ObjectIDForCharacter(character), target.Gid)
	if len(result.Broadcast) != 1 || result.Broadcast[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("ranged broadcast = %+v, want B245 only (ammo is private)", result.Broadcast)
	}
	if got := character.MissionInventory[1].StackCount; got != 1 {
		t.Fatalf("arrow stack after committed shot = %d, want 1", got)
	}
}

/*
================
TestWrongRangedAmmunitionRefusesBeforeDamageOrDebit
================
*/
func TestWrongRangedAmmunitionRefusesBeforeDamageOrDebit(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[character.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	character.MissionInventory[0].TypeFlags = weapon.TypeFlags()

	bolt := &enterworld.ItemRef{
		RefObjID: 62_002,
		Codename: "ITEM_ETC_AMMO_BOLT_01",
		TypeIDs:  [4]int64{3, 3, 4, 2},
	}
	items[bolt.Codename] = bolt
	character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: bolt.RefObjID, Codename: bolt.Codename,
		TypeFlags: bolt.TypeFlags(), StackCount: 2,
	})
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Codename = "SKILL_CH_BOW_BASE_01"
	skill.RequiredWeaponKinds = [2]uint8{6, 0xff}
	skills[2] = skill

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	// 58E32D: the basic attack's cnsm requirement refuses 0x300E, the
	// out-of-ammunition notice, before any damage or debit.
	assertAmmunitionRefusal(t, result)
	if character.MissionInventory[1].StackCount != 2 {
		t.Fatalf("wrong ammunition debited: %+v", character.MissionInventory)
	}
	after, ok := rt.Monsters.Get(testDivision, target.Gid)
	if !ok || after.CurrentHP != target.CurrentHP {
		t.Fatalf("wrong ammunition changed HP: %+v/%v", after, ok)
	}
}

/*
================
TestBasicAttackWithoutAmmunitionReportsTheNotice

Players saw no notice when a double-click attacked with an empty bow while
a skill did; native refuses both with 0x300E.
================
*/
func TestBasicAttackWithoutAmmunitionReportsTheNotice(t *testing.T) {
	for _, ranged := range []struct {
		name  string
		kind  int64
		skill string
		race  int64
	}{
		{"bow", 6, "SKILL_CH_BOW_BASE_01", enterworld.RaceChina},
		{"crossbow", 12, "SKILL_EU_CROSSBOW_BASE_01", enterworld.RaceEurope},
	} {
		t.Run(ranged.name, func(t *testing.T) {
			rt, _, character, target := newCombatTestRuntime(t, 100)
			// The basic attack is the race's own: bolts are European.
			character.ModelCodename = ""
			character.RaceIndex = testInt64(ranged.race)
			items := rt.deps.ItemReferences().(staticItemSource)
			weapon := items[character.MissionInventory[0].Codename]
			weapon.TypeIDs[3] = ranged.kind
			weapon.Combat.ActionRange = 180
			character.MissionInventory[0].TypeFlags = weapon.TypeFlags()
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.Codename = ranged.skill
			skill.RequiredWeaponKinds = [2]uint8{uint8(ranged.kind), 0xff}
			skills[2] = skill

			result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
				ActionId: 2, HasTarget: true, TargetGid: target.Gid,
			}.Encode())
			assertAmmunitionRefusal(t, result)
			after, ok := rt.Monsters.Get(testDivision, target.Gid)
			if !ok || after.CurrentHP != target.CurrentHP {
				t.Fatalf("an empty %s changed HP: %+v/%v", ranged.name, after, ok)
			}
		})
	}
}

/*
================
assertAmmunitionRefusal

The result carries exactly one B070 refusal with code 0x0E (0x300E).
================
*/
func assertAmmunitionRefusal(t *testing.T, result OpResult) {
	t.Helper()
	var refusals int
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpSkillCastResult && bytes.Equal(frame.Payload, []byte{2, 0x0e}) {
			refusals++
			continue
		}
		if frame.Opcode == wire.OpSkillCastResult {
			t.Fatalf("unexpected cast result %x", frame.Payload)
		}
	}
	if refusals != 1 {
		t.Fatalf("ammunition refusal frames = %+v, want one B070 {2, 0x0E}", result.Frames)
	}
}

/*
================
TestEmptyBowIsRefusedAtThePressOutOfRange

Command acceptance (phase 0x37, 4ACED4) carries the ammo bit 0x20, so an
empty bow pressed at a monster out of range is refused at once, with the
notice, and never walks: neither the basic attack nor a bow skill.
================
*/
func TestEmptyBowIsRefusedAtThePressOutOfRange(t *testing.T) {
	for _, press := range emptyBowPresses {
		t.Run(press, func(t *testing.T) {
			rt, _, c, target, id, _ := emptyBowFixture(t, press)
			result := rt.HandleTargetInteract(testDivision, c, emptyBowPress(press, id, target.Gid))
			assertAmmunitionRefusal(t, result)
			for _, frame := range result.Frames {
				if frame.Opcode == simulation.OpMovementAck {
					t.Fatal("an empty bow walked toward the target")
				}
			}
			if intents := rt.combatIntentSnapshot(); len(intents) != 0 {
				t.Fatalf("an empty bow kept a pursuit: %+v", intents)
			}
			if after, _ := rt.Monsters.Get(testDivision, target.Gid); after.CurrentHP != target.CurrentHP {
				t.Fatal("an empty bow changed HP")
			}
		})
	}
}

/*
================
TestArrivalRefusalReachesOnlyTheActor

A pursuit's refusal is decided on arrival under the simulation tick (here
the arrows were unequipped during the walk). It is the actor's alone: it
must reach the actor beside the tick's public range-entry correction, and
never ride the public route.
================
*/
func TestArrivalRefusalReachesOnlyTheActor(t *testing.T) {
	for _, press := range emptyBowPresses {
		t.Run(press, func(t *testing.T) {
			rt, clock, c, target, id, arrow := emptyBowFixture(t, press)
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 5})
			result := rt.HandleTargetInteract(testDivision, c, emptyBowPress(press, id, target.Gid))
			walked := false
			for _, frame := range result.Frames {
				walked = walked || frame.Opcode == simulation.OpMovementAck
				if frame.Opcode == wire.OpSkillCastResult {
					t.Fatalf("refused before the walk: %x", frame.Payload)
				}
			}
			if !walked {
				t.Fatalf("the press did not walk: %+v", result)
			}
			c.MissionInventory = c.MissionInventory[:len(c.MissionInventory)-1]
			private := 0
			for tick := 1; tick <= 200 && private == 0; tick++ {
				for _, burst := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
					for _, frame := range burst.Frames {
						if frame.Opcode != wire.OpSkillCastResult {
							continue
						}
						if burst.OnlyCharacterID != c.ID {
							t.Fatalf("cast result %x on the public route", frame.Payload)
						}
						if !bytes.Equal(frame.Payload, []byte{2, 0x0e}) {
							t.Fatalf("cast result %x, want {2, 0x0E}", frame.Payload)
						}
						private++
					}
				}
			}
			if private != 1 {
				t.Fatalf("arrival refusals = %d, want one private 0x300E", private)
			}
		})
	}
}

// emptyBowPresses are each race's ranged basic attack and one ranged skill.
var emptyBowPresses = []string{
	"SKILL_CH_BOW_BASE_01", "SKILL_CH_BOW_CRITICAL_A_01",
	"SKILL_EU_CROSSBOW_BASE_01", "SKILL_EU_ROG_BOWA_POWER_A_01",
}

/*
================
emptyBowFixture

An archer with an unloaded bow (CH) or crossbow (EU), out of range of the
target, and the ammunition row the weapon would take, not yet carried. A
_BASE_ codename renames the fixture's skill 2 to the race's basic attack;
any other codename is the shipped skill, learned.
================
*/
func emptyBowFixture(t *testing.T, codename string) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance, uint32, *enterworld.ItemRef) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	kind, race, ammo := int64(6), enterworld.RaceChina, &enterworld.ItemRef{RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	if strings.HasPrefix(codename, "SKILL_EU_") {
		c.ModelCodename = "CHAR_EU_MAN_NOBLE"
		kind, race, ammo = 12, enterworld.RaceEurope, &enterworld.ItemRef{RefObjID: 62002, Codename: "ITEM_ETC_AMMO_BOLT_01", TypeIDs: [4]int64{3, 3, 4, 2}}
	}
	c.RaceIndex = testInt64(race)
	items := rt.deps.ItemReferences().(staticItemSource)
	items[ammo.Codename] = ammo
	weapon := items[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = kind
	weapon.Combat.ActionRange = 180
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	skills := rt.deps.SkillData().(staticSkillSource)
	id := uint32(2)
	if strings.Contains(codename, "_BASE_") {
		skill := skills[2]
		skill.Codename = codename
		skill.RequiredWeaponKinds = [2]uint8{uint8(kind), 0xff}
		skills[2] = skill
	} else {
		skill := shippedOffense(t, codename)
		skills[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.CurrentMP = testInt64(100)
		id = skill.ID
	}
	*c.World.Spawn.X = 500
	return rt, clock, c, target, id, ammo
}

/*
================
emptyBowPress

The client's request for a press: the basic attack is an engage (the
double-click), any other skill a cast.
================
*/
func emptyBowPress(codename string, id, target uint32) []byte {
	if strings.Contains(codename, "_BASE_") {
		return wire.BasicAttackEngage{TargetGid: target}.Encode()
	}
	return wire.SkillAction{ActionId: id, HasTarget: true, TargetGid: target}.Encode()
}
