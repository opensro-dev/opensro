package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/statuseffect"
	"testing"
)

func TestAlchemyBonusTracksActiveEffectAndRemoval(t *testing.T) {
	rt, c := alchemyRuntime()
	deps := rt.deps.(*enterworld.Deps)
	deps.Skills = staticSkillSource{1: {ID: 1, Group: 42, AlchemyStoneBonus: 10, AlchemyReinforceBonus: 5}}
	c.Skills = []uint32{1}
	if a, b := rt.alchemyBonuses(testDivision, c); a != 0 || b != 0 {
		t.Fatal("learned skill activated bonus")
	}
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 1, 1, statuseffect.StateActive, true) {
		t.Fatal("effect admission failed")
	}
	if a, b := rt.alchemyBonuses(testDivision, c); a != 5 || b != 10 {
		t.Fatal(a, b)
	}
	if a, b := rt.alchemyBonuses("other", c); a != 0 || b != 0 {
		t.Fatal("cross-division bonus")
	}
	rt.effects.RequestVoluntaryStop(testDivision, c.Name, 1, 1)
	if a, b := rt.alchemyBonuses(testDivision, c); a != 0 || b != 0 {
		t.Fatal("stopped effect retained bonus")
	}
}

func TestAlchemyAvatarBonusOnlyComesFromWornOptions(t *testing.T) {
	rt, c := alchemyRuntime()
	rt.Alchemy.Magic[50] = alchemy.Magic{ID: 50, Tag: 0x6c756361}
	c.MissionInventory[0].MagicOptions = []uint64{uint64(4)<<32 | 50}
	if a, _ := rt.alchemyBonuses(testDivision, c); a != 0 {
		t.Fatal("bag option applied")
	}
	c.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{Slot: 1, MagicOptions: []uint64{uint64(4)<<32 | 50}}}}
	if a, _ := rt.alchemyBonuses(testDivision, c); a != 4 {
		t.Fatal(a)
	}
	c.AvatarInventory = nil
	if a, _ := rt.alchemyBonuses(testDivision, c); a != 0 {
		t.Fatal("removed avatar retained bonus")
	}
}

func TestAlchemyActiveBonusChangesLiveReinforcementOutcome(t *testing.T) {
	rt, c := alchemyRuntime()
	rt.deps.(*enterworld.Deps).Skills = staticSkillSource{1: {ID: 1, Group: 42, AlchemyReinforceBonus: 5}}
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 1, 1, statuseffect.StateActive, true) {
		t.Fatal("effect admission failed")
	}
	rt.AlchemyRoll = func() (uint32, error) { return 27, nil }
	frames := rt.HandleAlchemyReinforce(testDivision, c, []byte{2, 20, 21})
	last := frames[len(frames)-1]
	if last.Opcode != alchemy.OpReinforceResult || last.Payload[0] != 1 || last.Payload[1] != 1 {
		t.Fatal("active bonus did not reach rule", frames)
	}
}

/*
================
TestAlchemyAvatar4xLuckAddsExactly4

Four stacked LUCK ('luca') options on a worn dress (value 1 each) add +4 to
the reinforce chance (verified on an exact unclamped 50->54 boundary where
roll 53 succeeds and roll 54 fails); a total above 100 still succeeds on
the highest roll.
================
*/
func TestAlchemyAvatar4xLuckAddsExactly4(t *testing.T) {
	setup4xLuck := func(baseChance uint32) (*Runtime, *enterworld.Character) {
		rt, c := alchemyRuntime()
		elixir := rt.Alchemy.Items["elixir"]
		elixir.Params[1] = baseChance << 24
		rt.Alchemy.Items["elixir"] = elixir
		for _, overlay := range enterworld.AvatarV188MagicOptions() {
			id := uint16(overlay.ParamID)
			rt.Alchemy.Magic[id] = alchemy.Magic{ID: id, Name: overlay.OptionName, Degree: int(overlay.Degree), Tag: overlay.Tag}
		}
		c.AvatarInventory = &enterworld.AvatarInventory{Rows: []enterworld.InventoryRow{{
			Slot: 1,
			MagicOptions: []uint64{
				uint64(1)<<32 | 373,
				uint64(1)<<32 | 374,
				uint64(1)<<32 | 375,
				uint64(1)<<32 | 376,
			},
		}}}
		return rt, c
	}

	// Unclamped boundary: base 50 + 4x LUCK 4 = exact 54 (roll 53 succeeds, roll 54 fails).
	rt, c := setup4xLuck(50)
	if bonus, _ := rt.alchemyBonuses(testDivision, c); bonus != 4 {
		t.Fatalf("4x luck alchemyBonuses = %d, want 4", bonus)
	}
	rt.AlchemyRoll = func() (uint32, error) { return 53, nil }
	frames := rt.HandleAlchemyReinforce(testDivision, c, []byte{2, 20, 21})
	last := frames[len(frames)-1]
	if last.Opcode != alchemy.OpReinforceResult || last.Payload[0] != 1 || last.Payload[1] != 1 {
		t.Fatalf("50 + 4 luck bonus did not succeed on roll 53: %+v", frames)
	}

	rt, c = setup4xLuck(50)
	rt.AlchemyRoll = func() (uint32, error) { return 54, nil }
	frames = rt.HandleAlchemyReinforce(testDivision, c, []byte{2, 20, 21})
	last = frames[len(frames)-1]
	if last.Opcode != alchemy.OpReinforceResult || last.Payload[0] != 1 || last.Payload[1] != 0 {
		t.Fatalf("50 + 4 luck bonus unexpectedly succeeded on roll 54: %+v", frames)
	}

	// A total over 100 (base 25 + skill 80 + 4x LUCK 4 = 109) still succeeds on
	// the highest roll, 99. Rolls run 0..99, so the clamp to 100 itself is not
	// observable through the outcome; this only proves the large total is kept.
	rt, c = setup4xLuck(25)
	rt.deps.(*enterworld.Deps).Skills = staticSkillSource{1: {ID: 1, Group: 42, AlchemyReinforceBonus: 80}}
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 1, 1, statuseffect.StateActive, true) {
		t.Fatal("effect admission failed")
	}
	rt.AlchemyRoll = func() (uint32, error) { return 99, nil }
	frames = rt.HandleAlchemyReinforce(testDivision, c, []byte{2, 20, 21})
	last = frames[len(frames)-1]
	if last.Opcode != alchemy.OpReinforceResult || last.Payload[0] != 1 || last.Payload[1] != 1 {
		t.Fatalf("chance 109 did not succeed on roll 99: %+v", frames)
	}
}
