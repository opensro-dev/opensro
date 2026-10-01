/*
===========================================================================

projectilecast_test.go - projectile preparation, commitment and interruption tests

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
arrowFixture
================
*/
func arrowFixture(t *testing.T) (*Runtime, *enterworld.Character, uint32, enterworld.SkillRow, int64) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	items := rt.deps.ItemReferences().(staticItemSource)
	weapon := items[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 6
	weapon.Combat.ActionRange = 180
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	arrow := &enterworld.ItemRef{RefObjID: 62001, Codename: "ITEM_ETC_AMMO_ARROW_01", TypeIDs: [4]int64{3, 3, 4, 1}}
	items[arrow.Codename] = arrow
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 7, RefObjID: arrow.RefObjID, Codename: arrow.Codename, TypeFlags: arrow.TypeFlags(), StackCount: 2})
	skill := shippedOffense(t, "SKILL_CH_BOW_CRITICAL_A_01")
	if !skill.DirectOffensePinned || skill.ProjectileSpeed != 400 || skill.Ammunition != (enterworld.SkillAmmunition{TID3: 4, TID4: 1, Count: 1}) {
		t.Fatalf("authored admission: %+v", skill)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(100)
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	return rt, c, target.Gid, skill, clock.NowMs()
}

/*
================
TestCriticalArrowReleaseChargesOnceAndCarriesRealCritical
================
*/
func TestCriticalArrowReleaseChargesOnceAndCarriesRealCritical(t *testing.T) {
	rt, c, target, skill, now := arrowFixture(t)
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
	before, _ := rt.Monsters.Get(testDivision, target)
	start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now)
	if decision != skillCastAccepted || len(start.Frames) != 1 || len(start.Frames[0].Payload) != 19 || start.Frames[0].Payload[18] != 0 {
		t.Fatalf("start must carry no premature damage: %+v", start)
	}
	token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
	wantCooldown := now + int64(skill.CoolTimeMs)
	if c.OffensiveSkillCooldowns[skill.Group] != wantCooldown {
		t.Fatal("cooldown not registered at acceptance")
	}
	for _, at := range []int64{now, now + 299, now + 300} {
		if got := rt.advanceProjectileCasts(at); len(got) != 0 {
			t.Fatalf("early release at %d: %+v", at-now, got)
		}
	}
	if enterworld.CurrentMP(c) != 100 || c.MissionInventory[1].StackCount != 2 || len(rt.criticals.actors) != 0 {
		t.Fatal("preparation changed cost/history")
	}
	if _, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now+300); decision != skillCastDeferred {
		t.Fatal("duplicate preparation accepted")
	}
	frames := rt.advanceProjectileCasts(now + 301)
	if c.OffensiveSkillCooldowns[skill.Group] != wantCooldown {
		t.Fatal("release restarted cooldown")
	}
	if len(frames) != 2 || len(frames[0].Frames) != 1 || frames[1].OnlyCharacterID != c.ID {
		t.Fatalf("release routing: %+v", frames)
	}
	p := frames[0].Frames[0].Payload
	if frames[0].Frames[0].Opcode != wire.OpSkillEffectControl || p[0] != 1 || binary.LittleEndian.Uint32(p[1:]) != token || len(p) != 25 {
		t.Fatalf("bad release: %x", p)
	}
	packed := binary.LittleEndian.Uint32(p[17:])
	if uint8(packed) != 2 {
		t.Fatalf("authored cr modifier not used: %x", p)
	}
	after, _ := rt.Monsters.Get(testDivision, target)
	if before.CurrentHP-after.CurrentHP != packed>>8 || enterworld.CurrentMP(c) != 79 || c.MissionInventory[1].StackCount != 1 {
		t.Fatal("HP/ammo/MP transaction mismatch")
	}
	if frames[1].Frames[0].Opcode != wire.OpAvatarInventorySlot7StackCount || binary.LittleEndian.Uint16(frames[1].Frames[0].Payload) != 1 {
		t.Fatal("missing private ammo delivery")
	}
	if c.OffensiveSkillCooldowns[skill.Group] != now+4000 {
		t.Fatal("accepted cooldown was changed by release")
	}
	if len(rt.advanceProjectileCasts(now+1000)) != 0 || c.MissionInventory[1].StackCount != 1 {
		t.Fatal("release replayed")
	}
	closes := rt.drainSkillFinalizes(now + 2000)
	if len(closes) != 1 || len(closes[0].Frames) != 1 || binary.LittleEndian.Uint32(closes[0].Frames[0].Payload[2:]) != token {
		t.Fatalf("one close required: %+v", closes)
	}
}

/*
================
TestCriticalArrowRefusalAndCancellationHaveNoCost
================
*/
func TestCriticalArrowRefusalAndCancellationHaveNoCost(t *testing.T) {
	for _, branch := range []string{"missing-ammo", "wrong-ammo", "cancel", "target-death", "caster-death", "forget", "ammo-removed", "mp-removed", "weapon-swap"} {
		t.Run(branch, func(t *testing.T) {
			rt, c, target, skill, now := arrowFixture(t)
			cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
			if branch == "missing-ammo" {
				c.MissionInventory = c.MissionInventory[:1]
			}
			if branch == "wrong-ammo" {
				rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[1].Codename].TypeIDs[3] = 2
			}
			start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now)
			if branch == "missing-ammo" || branch == "wrong-ammo" {
				if decision != skillCastRefused || len(rt.pendingProjectileCasts) != 0 {
					t.Fatal("bad ammunition admitted")
				}
				return
			}
			if decision != skillCastAccepted {
				t.Fatalf("prepare: %+v", start)
			}
			switch branch {
			case "cancel":
				cancel := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Cancel: true}.Encode())
				assertOpcodes(t, cancel.Frames, wire.OpActionState)
				if cancel.Frames[0].Payload[0] != wire.ActionStateKindNotice || cancel.Frames[0].Payload[1] != 1 {
					t.Fatal("committed skill did not refuse voluntary cancellation", cancel)
				}
				// 4ACC40 protects the committed skill. Forced interruption is a
				// different owner and remains covered by the other branches.
				if len(rt.advanceProjectileCasts(now+1000)) == 0 {
					t.Fatal("refused cancellation prevented the committed release")
				}
				return
			case "forget":
				rt.ForgetCharacter(testDivision, c.Name)
			case "target-death":
				rt.Monsters.ApplyDamage(testDivision, target, 100000)
			case "caster-death":
				c.CurrentHP = testInt64(0)
			case "ammo-removed":
				c.MissionInventory = c.MissionInventory[:1]
			case "mp-removed":
				c.CurrentMP = testInt64(0)
			case "weapon-swap":
				rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename].TypeIDs[3] = 3
			}
			out := rt.advanceProjectileCasts(now + 1000)
			for _, batch := range out {
				for _, f := range batch.Frames {
					if f.Opcode == wire.OpSkillEffectControl && f.Payload[0] == 1 {
						t.Fatalf("cancelled cast released: %+v", out)
					}
				}
			}
			if len(rt.criticals.actors) != 0 || c.OffensiveSkillCooldowns[skill.Group] != now+int64(skill.CoolTimeMs) {
				t.Fatal("cancelled cast changed history or accepted cooldown")
			}
			if branch != "mp-removed" && enterworld.CurrentMP(c) != 100 {
				t.Fatal("cancelled cast charged MP")
			}
			if len(c.MissionInventory) > 1 && c.MissionInventory[1].StackCount != 2 {
				t.Fatal("cancelled cast charged ammo")
			}
			if len(rt.pendingProjectileCasts) != 0 {
				t.Fatal("cancelled owner leaked")
			}
			if _, current := rt.currentSkillCommandFor(testDivision, c); current {
				t.Fatal("invalidated cast retained current-command ownership")
			}
		})
	}
}

/*
================
TestProjectileFlightUsesThreeDimensionalReleaseSample
================
*/
func TestProjectileFlightUsesThreeDimensionalReleaseSample(t *testing.T) {
	a := simulation.Spawn{RegionID: 0x60a5, X: 1900, Y: 100, Z: 100}
	b := simulation.Spawn{RegionID: 0x60a6, X: 280, Y: 500, Z: 100}
	if got := projectileFlightMs(a, b, 400); got != 1250 {
		t.Fatalf("3D region distance duration=%d", got)
	}
	if got := projectileFlightMs(a, a, 400); got != 0 {
		t.Fatal(got)
	}
}

/*
================
TestCriticalArrowRepeatedCastConsumesLastArrowOnlyOnce
================
*/
func TestCriticalArrowRepeatedCastConsumesLastArrowOnlyOnce(t *testing.T) {
	rt, c, target, skill, now := arrowFixture(t)
	rt.CombatRoll = func() (uint32, error) { return 100, nil }
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
	for shot := int64(0); shot < 2; shot++ {
		at := now + shot*5000
		_, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, at)
		if decision != skillCastAccepted {
			t.Fatal("repeat refused", shot)
		}
		out := rt.advanceProjectileCasts(at + 301)
		if len(out) != 2 || out[0].Frames[0].Payload[17] != 1 {
			t.Fatal("normal result not serialized", out)
		}
		// Cancel after release stops no pending preparation and refunds nothing.
		if len(rt.cancelPreparingProjectile(testDivision, c.Name)) != 0 {
			t.Fatal("released cast was rolled back")
		}
		if enterworld.CurrentMP(c) != 100-(shot+1)*21 {
			t.Fatal("duplicate or missing MP charge")
		}
		rt.drainSkillFinalizes(at + 2000)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 7 {
			t.Fatal("empty ammunition socket retained")
		}
	}
	_, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now+10000)
	if decision != skillCastRefused || enterworld.CurrentMP(c) != 58 {
		t.Fatal("empty quiver became a free cast")
	}
}

/*
================
TestPreparedOffenseRejectedCooldownCommitPublishesNothing
================
*/
func TestPreparedOffenseRejectedCooldownCommitPublishesNothing(t *testing.T) {
	rt, c, target, skill, now := arrowFixture(t)
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(*enterworld.Character, string, func() bool) bool { return false }
	before, _ := rt.Monsters.Get(testDivision, target)
	r, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}, now)
	after, _ := rt.Monsters.Get(testDivision, target)
	if decision != skillCastRefused || len(r.Broadcast) != 0 || rt.castTokenCounter != 0 || len(rt.pendingProjectileCasts) != 0 || len(c.OffensiveSkillCooldowns) != 0 || enterworld.CurrentMP(c) != 100 || c.MissionInventory[1].StackCount != 2 || after.CurrentHP != before.CurrentHP {
		t.Fatal("failed cooldown commit published or changed combat", r)
	}
}
