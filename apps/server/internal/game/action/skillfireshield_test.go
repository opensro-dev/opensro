/*
===========================================================================

skillfireshield_test.go - Fire Shield resistance through the gameplay owner

Cast shipped rows with their shield requirement intact, then probe incoming
statuses through player stats and the monster-hit abnormal roller. Expiry,
cancellation and rank replacement must release the same contributions.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
fireShieldFixture
================
*/
func fireShieldFixture(t *testing.T, code string) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c, shield, _ := shieldFixture(t)
	row := shippedOffense(t, code)
	if !row.TimedEffect.Pinned || row.TimedEffect.Bgra.Mask == 0 {
		t.Fatalf("Fire Shield not admitted: %+v", row)
	}
	// Keep the level-1 stat row while affording every book's native MP cost.
	c.Intellect = testInt64(2000)
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(10000)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: shield.RefObjID, Codename: shield.Codename, TypeFlags: shield.TypeFlags(),
		VarianceBits: "0", Durability: 1, StackCount: 1,
	})
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	return rt, clock, c, row
}

/*
================
fireShieldProbe

The six elemental statuses at power 100, plus Sleep outside bgra's mask.
================
*/
func fireShieldProbe(t *testing.T, rt *Runtime, c *enterworld.Character) map[abnormal.Status]abnormal.Record {
	t.Helper()
	var params abnormal.SkillParams
	for status := abnormal.Freeze; status <= abnormal.Zombie; status++ {
		params.Params[status] = abnormal.Param{Present: true, Args: [6]uint32{100, 100, 1}}
	}
	params.Params[6] = abnormal.Param{Present: true, Args: [6]uint32{10000, 100, 1}}
	defender, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	records, err := rt.rollCreatureOnPlayer(testDivision, 1, 1, &params, c, defender, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[abnormal.Status]abnormal.Record)
	for _, record := range records {
		out[record.Status] = record
	}
	return out
}

/*
================
TestFireShieldReducesElementalStatusesUntilRetirement
================
*/
func TestFireShieldReducesElementalStatusesUntilRetirement(t *testing.T) {
	for _, retire := range []string{"expiry", "cancel"} {
		t.Run(retire, func(t *testing.T) {
			rt, clock, c, row := fireShieldFixture(t, fireShieldA1)
			base := fireShieldProbe(t, rt, c)
			beforeMP := enterworld.CurrentMP(c)
			if out := castSelf(rt, c, row.ID); out.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, row.ID) {
				t.Fatalf("cast: %+v", out)
			}
			if enterworld.CurrentMP(c) >= beforeMP {
				t.Fatal("Fire Shield did not charge MP")
			}
			buffed := fireShieldProbe(t, rt, c)
			for status := abnormal.Freeze; status <= abnormal.Zombie; status++ {
				before, after := base[status], buffed[status]
				if before.Level != 100 || after.Level != 82 || after.DurationMs >= before.DurationMs || after.DurationMs == 0 {
					t.Fatalf("status %v: before %+v, buffed %+v", status, before, after)
				}
			}
			if buffed[abnormal.Sleep] != base[abnormal.Sleep] {
				t.Fatal("Fire Shield changed Sleep")
			}
			if retire == "expiry" {
				clock.Advance(time.Duration(row.EffectDurationMs+1) * time.Millisecond)
			} else {
				clock.Advance(3 * time.Second)
				rt.drainSkillFinalizes(clock.NowMs())
				rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: row.ID}).Encode())
			}
			rt.TickHook()(clock.NowMs())
			if hasSkillEffect(rt, c.Name, row.ID) {
				t.Fatal("retired Fire Shield still installed")
			}
			after := fireShieldProbe(t, rt, c)
			for status, record := range base {
				if after[status] != record {
					t.Fatalf("status %v after %s: %+v, want %+v", status, retire, after[status], record)
				}
			}
		})
	}
}

/*
================
TestFireShieldHigherRankReplacesResistance
================
*/
func TestFireShieldHigherRankReplacesResistance(t *testing.T) {
	rt, clock, c, first := fireShieldFixture(t, fireShieldA1)
	if out := castSelf(rt, c, first.ID); !hasSkillEffect(rt, c.Name, first.ID) {
		t.Fatalf("first cast: %+v", out)
	}
	clock.Advance(10 * time.Second)
	rt.drainSkillFinalizes(clock.NowMs())
	second := shippedOffense(t, "SKILL_CH_FIRE_SHIELD_A_02")
	rt.deps.SkillData().(staticSkillSource)[second.ID] = second
	c.Skills = append(c.Skills, second.ID)
	if out := castSelf(rt, c, second.ID); out.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, second.ID) {
		t.Fatalf("second cast: %+v", out)
	}
	for status, record := range fireShieldProbe(t, rt, c) {
		if status <= abnormal.Zombie && record.Level != 79 {
			t.Fatalf("status %v power %d, want 79; ranks stacked", status, record.Level)
		}
	}
	for _, effect := range rt.effects.Snapshot(testDivision, c.Name) {
		if effect.SkillID == first.ID && !effect.StopRequested {
			t.Fatal("previous rank remains active")
		}
	}
}

/*
================
TestFireShieldAddsToEquipmentResistance

Emperor's 78 plus 22 frostbite resistance reaches immunity for freezing and
frostbite. Other elements retain their own 78 points, and Sleep still lands.
================
*/
func TestFireShieldAddsToEquipmentResistance(t *testing.T) {
	rt, _, c, row := fireShieldFixture(t, "SKILL_CH_FIRE_SHIELD_D_01")
	dir := gamedatatest.TextdataDir(t)
	ring, ok := enterworld.NewTextdataItems(dir).ItemRefByCodename("ITEM_CH_RING_01_A")
	if !ok {
		t.Fatal("shipped ring missing")
	}
	rt.deps.ItemReferences().(staticItemSource)[ring.Codename] = ring
	rt.deps.(*enterworld.Deps).MagicOptions = gaugeOptions{
		17: {ParamID: 17, OptionName: "MATTR_RESIST_FROSTBITE", Tag: 0x667a},
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 11, RefObjID: ring.RefObjID, Codename: ring.Codename, TypeFlags: ring.TypeFlags(),
		VarianceBits: "0", Durability: 1, StackCount: 1, MagicOptions: []uint64{uint64(22)<<32 | 17},
	})
	base := fireShieldProbe(t, rt, c)
	if base[abnormal.Freeze].Level != 78 || base[abnormal.Frostbite].Level != 78 {
		t.Fatal("ring did not supply resistance", base)
	}
	if out := castSelf(rt, c, row.ID); !hasSkillEffect(rt, c.Name, row.ID) {
		t.Fatalf("cast: %+v", out)
	}
	results := fireShieldProbe(t, rt, c)
	if _, ok := results[abnormal.Freeze]; ok {
		t.Fatal("freezing landed through 100% resistance")
	}
	if _, ok := results[abnormal.Frostbite]; ok {
		t.Fatal("frostbite landed through 100% resistance")
	}
	for status := abnormal.ElectricShock; status <= abnormal.Zombie; status++ {
		if results[status].Level != 22 {
			t.Fatalf("status %v power %d, want 22", status, results[status].Level)
		}
	}
	if results[abnormal.Sleep] != base[abnormal.Sleep] {
		t.Fatal("Fire Shield changed Sleep")
	}
}
