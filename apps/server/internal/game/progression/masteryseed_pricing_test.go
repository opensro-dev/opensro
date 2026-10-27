package progression

// The level-0 mastery seed and its pricing model (levelup wave, LANE-2).
//
// Seed: creation rows sit at level 0, retail's _AddNewChar value - the
// v1.150 client pin lives at enterworld.MasterySeedLevel. Pricing: the
// L -> L+1 train costs the leveldata row of the CURRENT level L, and the
// 0 -> 1 first train is FREE (client enable gate sub_5841d0 @0x0058435b
// unconditional-enables level-0 rows; tooltip sub_55ab40 @0x0055b21e
// hardcodes the displayed cost to 0; the cross-version reference server's
// mastery raise takes a level-0 branch that bypasses pricing).

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// The creation seed is level 0 for every racial row, both races.
func TestDefaultMasteriesSeedAtLevelZero(t *testing.T) {
	for _, raceKey := range []string{enterworld.RaceKeyChina, enterworld.RaceKeyEurope} {
		for _, row := range enterworld.DefaultMasteries(raceKey) {
			if row.Level != 0 {
				t.Fatalf("%s seed row %d = level %d, want the retail 0", raceKey, row.ID, row.Level)
			}
		}
	}
}

// The first train is free: a FRESH level-1 character with ZERO skill
// points trains a seeded level-0 mastery to 1 - the retail creation
// experience (the client enables the button unconditionally at level 0
// and displays 0 SP; a charge here would refuse a request the retail UI
// invites).
func TestMasteryFirstTrainIsFreeForAFreshCharacter(t *testing.T) {
	character := testCharacter()
	character.Level = int64Ptr(1)
	character.SkillPoints = int64Ptr(0)
	rt := newTestRuntime(character)

	result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))

	if len(result.Frames) != 2 {
		t.Fatalf("frames = %d, want the ack + the SP refresh", len(result.Frames))
	}
	ack := result.Frames[0].Payload
	if len(ack) != 6 || ack[0] != wire.ResultSuccess || ack[5] != 1 {
		t.Fatalf("free-train ack = %v, want [01 id.. 01]", ack)
	}
	if level, ok := enterworld.MasteryLevel(character, chMastery); !ok || level != 1 {
		t.Fatalf("mastery level = %d/%v, want 1/true", level, ok)
	}
	if got := *character.SkillPoints; got != 0 {
		t.Fatalf("skill points = %d, want the untouched 0 (the 0->1 train is free)", got)
	}
}

// The free first train needs no leveldata row (there IS no row 0): it
// stays trainable even on a degraded boot without the cost table, while
// priced trains keep refusing (guarded by the runtime_test no-table
// case). The pinned behaviour is "free", not "missing data defaults to
// zero".
func TestMasteryFirstTrainIsFreeEvenWithoutACostTable(t *testing.T) {
	character := testCharacter()
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
	}
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, fn func()) {
		if fn != nil {
			fn()
		}
	}
	rt := NewRuntime(deps)

	result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))

	if got := result.Frames[0].Payload[0]; got != wire.ResultSuccess {
		t.Fatalf("table-less 0->1 train refused: %v", result.Frames[0].Payload)
	}
	if level, _ := enterworld.MasteryLevel(character, chMastery); level != 1 {
		t.Fatalf("mastery level = %d, want 1", level)
	}
}

// A priced train charges the CURRENT level's row - chosen at a boundary
// where the two candidate rows differ (row 5 = 2 SP, row 6 = 4 SP in the
// shipped table and the fake), so the old SkillPointCost(newLevel)
// off-by-one cannot pass by coincidence.
func TestMasteryTrainingChargesTheCurrentLevelsRow(t *testing.T) {
	character := testCharacter()
	for i := range character.Masteries {
		if character.Masteries[i].ID == chMastery {
			character.Masteries[i].Level = 5
		}
	}
	rt := newTestRuntime(character)

	result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))

	ack := result.Frames[0].Payload
	if len(ack) != 6 || ack[0] != wire.ResultSuccess || ack[5] != 6 {
		t.Fatalf("train ack = %v, want [01 id.. 06]", ack)
	}
	// Row 5 (the level being left) is 2 SP; row 6 would be 4.
	if got := *character.SkillPoints; got != 98 {
		t.Fatalf("skill points = %d, want 98 (100 - row-5 cost 2; 96 means the newLevel row was charged)", got)
	}
}

// A priced train whose CURRENT level has no leveldata row refuses 07:02
// (fail-closed pricing is unchanged for L >= 1; only the pinned-free 0->1
// is exempt). testLevels carries no row 7.
func TestMasteryTrainingRefusesAnUnpricedCurrentLevel(t *testing.T) {
	character := testCharacter()
	for i := range character.Masteries {
		if character.Masteries[i].ID == chMastery {
			character.Masteries[i].Level = 7
		}
	}
	rt := newTestRuntime(character)

	result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))

	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.ResultError || payload[1] != wire.ErrCodeMasterySkillPoints {
		t.Fatalf("unpriced-level train = %v, want the 07:02 refusal", payload)
	}
	if level, _ := enterworld.MasteryLevel(character, chMastery); level != 7 {
		t.Fatalf("refused training moved the level to %d", level)
	}
}

// The retail-correct fresh-EU experience the old level-1 seed broke: at
// character level 1 the budget is min(2 x 1, 240) = 2, so exactly TWO
// free 0->1 trains fit and the third refuses 07:05. With the old seed
// (sum 6) the budget was overdrawn from creation and refused everything;
// with a wrong budget quantity (count instead of sum, or the CH flat cap)
// the third train would pass.
func TestEuFreshCharacterTrainsExactlyTwoMasteriesAtLevelOne(t *testing.T) {
	eu := testEuCharacter(1)
	eu.SkillPoints = int64Ptr(0)
	rt := newTestRuntime(eu)

	ids := []uint32{513, 514, 518}
	for i, id := range ids[:2] {
		result := rt.HandleMasteryLevelUp(testDivision, eu, masteryPayload(id, 1))
		if got := result.Frames[0].Payload[0]; got != wire.ResultSuccess {
			t.Fatalf("free train %d (mastery %d) refused: %v", i, id, result.Frames[0].Payload)
		}
	}
	if got := masteryLevelSum(eu); got != 2 {
		t.Fatalf("mastery sum after two trains = %d, want 2", got)
	}

	result := rt.HandleMasteryLevelUp(testDivision, eu, masteryPayload(ids[2], 1))
	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.ResultError || payload[1] != wire.ErrCodeMasteryTotalLimit {
		t.Fatalf("third train at the level-1 EU budget = %v, want the 07:05 refusal", payload)
	}
	if got := *eu.SkillPoints; got != 0 {
		t.Fatalf("free trains moved SP to %d", got)
	}
}
