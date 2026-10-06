/*
===========================================================================

skilllearn_test.go - skill training admission, spending and rank replacement

===========================================================================
*/
package progression

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// staticSkills is a test enterworld.SkillDataSource. The first four rows
// mirror the SHIPPED CH sword head of skilldata_5000.txt exactly (ids,
// groups, requirement pairs and SP costs verified against the extracted
// file), so the gate semantics are exercised on real authority values;
// the higher ids are synthetic rows for the upgrade/edge cases.
/*
================
staticSkills
================
*/
type staticSkills map[uint32]enterworld.SkillRow

/*
================
SkillByID
================
*/
func (s staticSkills) SkillByID(id uint32) (enterworld.SkillRow, bool) {
	row, ok := s[id]
	return row, ok
}

const (
	// Shipped rows (skilldata_5000.txt).
	skillPunch     uint32 = 1 // SKILL_PUNCH_01: grp 172 lvl 1, no reqs, sp 0
	skillSwordBase uint32 = 2 // SKILL_CH_SWORD_BASE_01: grp 173 lvl 1, mastery 257@0, sp 0
	skillSmashA1   uint32 = 3 // SKILL_CH_SWORD_SMASH_A_01: grp 174 lvl 1, mastery 257@5, sp 2
	skillSmashB1   uint32 = 4 // SKILL_CH_SWORD_SMASH_B_01: grp 175 lvl 1, mastery 257@27, prereq grp 174@9, sp 117

	// Shipped chain set (group 177 level 1, SKILL_CH_SWORD_CHAIN_A_*_01):
	// the root pays SP and links the sub-rows; only the root is learnable.
	skillChain1S uint32 = 6 // 1S root: mastery 257@7, sp 5, chain -> 7
	skillChain2S uint32 = 7 // 2S sub-row: linked to by 6, sp 0, chain -> 8
	skillChain3S uint32 = 8 // 3S sub-row: linked to by 7, sp 0, chain end

	// Synthetic rows.
	skillSmashA2     uint32 = 9001 // grp 174 lvl 2 (the upgrade target)
	skillSmashA9     uint32 = 9009 // grp 174 lvl 9 (satisfies SMASH_B's prereq)
	skillStrGated    uint32 = 9101 // ReqStr 200 (custom-data posture)
	skillIntGated    uint32 = 9102 // ReqInt 200
	skillEUReserve   uint32 = 9201 // requires EU mastery 513 (no CH record)
	euMasteryWarlock uint32 = 513
)

/*
================
testSkills
================
*/
func testSkills() staticSkills {
	return staticSkills{
		skillPunch:     {ID: skillPunch, Group: 172, Level: 1},
		skillSwordBase: {ID: skillSwordBase, Group: 173, Level: 1, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 0}}},
		skillSmashA1:   {ID: skillSmashA1, Group: 174, Level: 1, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 5}}, SPCost: 2},
		skillSmashB1: {
			ID: skillSmashB1, Group: 175, Level: 1,
			Masteries:     [2]enterworld.SkillRequirement{{ID: chMastery, Level: 27}},
			Prerequisites: [3]enterworld.SkillRequirement{{ID: 174, Level: 9}},
			SPCost:        117,
		},
		skillChain1S:   {ID: skillChain1S, Group: 177, Level: 1, ChainNext: skillChain2S, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 7}}, SPCost: 5},
		skillChain2S:   {ID: skillChain2S, Group: 177, Level: 1, ChainNext: skillChain3S, ChainSub: true, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 7}}},
		skillChain3S:   {ID: skillChain3S, Group: 177, Level: 1, ChainSub: true, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 7}}},
		skillSmashA2:   {ID: skillSmashA2, Group: 174, Level: 2, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 5}}, SPCost: 5},
		skillSmashA9:   {ID: skillSmashA9, Group: 174, Level: 9, Masteries: [2]enterworld.SkillRequirement{{ID: chMastery, Level: 20}}, SPCost: 10},
		skillStrGated:  {ID: skillStrGated, Group: 9110, Level: 1, SPCost: 1, ReqStr: 200},
		skillIntGated:  {ID: skillIntGated, Group: 9111, Level: 1, SPCost: 1, ReqInt: 200},
		skillEUReserve: {ID: skillEUReserve, Group: 9210, Level: 1, SPCost: 1, Masteries: [2]enterworld.SkillRequirement{{ID: euMasteryWarlock, Level: 0}}},
	}
}

// newSkillTestRuntime wires a runtime with the skill table attached.
/*
================
newSkillTestRuntime
================
*/
func newSkillTestRuntime(character *enterworld.Character) *Runtime {
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Levels:     testLevels(),
		Skills:     testSkills(),
	}
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, fn func()) {
		if fn != nil {
			fn()
		}
	}
	return NewRuntime(deps)
}

// setMastery raises one mastery record on the test character.
/*
================
setMastery
================
*/
func setMastery(character *enterworld.Character, masteryID uint32, level int64) {
	for i := range character.Masteries {
		if character.Masteries[i].ID == masteryID {
			character.Masteries[i].Level = level
			return
		}
	}
}

// skillPayload encodes a 0x72CB body.
/*
================
skillPayload
================
*/
func skillPayload(skillID uint32) []byte {
	return wire.NewWriter(4).U32(skillID).Payload()
}

/*
================
TestSkillLearnGrantsAndChargesSP
================
*/
func TestSkillLearnGrantsAndChargesSP(t *testing.T) {
	character := testCharacter()
	setMastery(character, chMastery, 5)
	rt := newSkillTestRuntime(character)

	result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashA1))

	if len(result.Frames) != 2 {
		t.Fatalf("frames = %d, want the ack + the SP refresh", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpSkillLearnResponse {
		t.Fatalf("ack opcode = 0x%04X, want 0x%04X", result.Frames[0].Opcode, wire.OpSkillLearnResponse)
	}
	// [1][u32 skillId] - the client marks exactly this id learned
	// (sub_75bb20 @0x0075bb48), so the ack must echo the request.
	ack := result.Frames[0].Payload
	if len(ack) != 5 || ack[0] != wire.ResultSuccess ||
		ack[1] != 0x03 || ack[2] != 0 || ack[3] != 0 || ack[4] != 0 {
		t.Fatalf("skill ack = %v, want [01 03 00 00 00]", ack)
	}
	// The SP refresh mirrors the mastery-training posture: absolute,
	// type 2, silent.
	sp := result.Frames[1]
	if sp.Opcode != wire.OpPointsUpdate {
		t.Fatalf("second frame = 0x%04X, want 0x30B3", sp.Opcode)
	}
	if len(sp.Payload) != 6 || sp.Payload[0] != wire.PointsTypeSkill {
		t.Fatalf("SP frame = %v, want a type-2 absolute update", sp.Payload)
	}
	if sp.Payload[5] != 0 {
		t.Fatal("the SP refresh must be silent - a loss toast would read as a penalty for a purchase")
	}

	if len(character.Skills) != 1 || character.Skills[0] != skillSmashA1 {
		t.Fatalf("learned list = %v, want [%d]", character.Skills, skillSmashA1)
	}
	if got := *character.SkillPoints; got != 98 {
		t.Fatalf("skill points = %d, want 98 (the shipped 2-SP cost charged)", got)
	}
}

/*
================
TestSkillLearnZeroCostSkillRefuses
================
*/
func TestSkillLearnZeroCostSkillRefuses(t *testing.T) {
	character := testCharacter()
	rt := newSkillTestRuntime(character)

	result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillPunch))

	assertSkillRefusal(t, result, skillLearnUnavailable)
	if len(character.Skills) != 0 {
		t.Fatal("zero-SP learning changed the learned list")
	}
	if got := *character.SkillPoints; got != 100 {
		t.Fatalf("skill points = %d, want 100 (sp cost 0)", got)
	}
}

// The client's ack handler REPLACES the group's previous-level entry
// (sub_8509f0 @0x00850ac9), so the persisted list must do the same: one
// id per group, never both levels.
/*
================
TestSkillLearnUpgradeReplacesTheGroupEntry
================
*/
func TestSkillLearnUpgradeReplacesTheGroupEntry(t *testing.T) {
	character := testCharacter()
	setMastery(character, chMastery, 10)
	character.Skills = []uint32{skillSmashA1}
	rt := newSkillTestRuntime(character)

	result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashA2))

	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("upgrade refused: %v", result.Frames[0].Payload)
	}
	if len(character.Skills) != 1 || character.Skills[0] != skillSmashA2 {
		t.Fatalf("learned list = %v, want the level-2 id %d REPLACING the level-1 entry", character.Skills, skillSmashA2)
	}
}

// Chain sub-rows (2S/3S) are never learnable: retail resolves them from
// the learned ROOT via the skilldata chain link, and its skill board can
// only compose 0x72CB with the root id. The fresh-group
// case is the load-bearing one: with group 177 unlearned, the continuity
// gate alone would PASS a 2S/3S learn (level 1-1 == 0), so only the
// chain-sub gate stands between a modified client and a learned list
// holding a state retail cannot reach.
/*
================
TestSkillLearnRefusesChainSubRows
================
*/
func TestSkillLearnRefusesChainSubRows(t *testing.T) {
	character := testCharacter()
	setMastery(character, chMastery, 7)
	rt := newSkillTestRuntime(character)

	// Fresh group: both zero-SP sub-rows refuse silently (0x09), nothing written.
	assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillChain2S)), skillLearnUnavailable)
	assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillChain3S)), skillLearnUnavailable)
	if len(character.Skills) != 0 {
		t.Fatalf("refused chain sub-row learn still wrote %v", character.Skills)
	}
	if got := *character.SkillPoints; got != 100 {
		t.Fatalf("refused chain sub-row learn charged SP: %d", got)
	}

	// The ROOT is the set's one learnable row: acked, charged, listed.
	result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillChain1S))
	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("chain root learn refused: %v", result.Frames[0].Payload)
	}
	if len(character.Skills) != 1 || character.Skills[0] != skillChain1S {
		t.Fatalf("learned list = %v, want the root %d only", character.Skills, skillChain1S)
	}
	if got := *character.SkillPoints; got != 95 {
		t.Fatalf("skill points = %d, want 95 (the shipped 5-SP root cost)", got)
	}

	// With the root learned the sub-rows still refuse.
	assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillChain2S)), skillLearnUnavailable)
}

/*
================
TestSkillLearnPrerequisiteGroupGate
================
*/
func TestSkillLearnPrerequisiteGroupGate(t *testing.T) {
	character := testCharacter()
	setMastery(character, chMastery, 27)
	character.SkillPoints = int64Ptr(500)
	rt := newSkillTestRuntime(character)

	// Group 174 unlearned: SMASH_B (prereq grp 174@9) must refuse.
	result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashB1))
	assertSkillRefusal(t, result, skillLearnPrerequisiteMissing)
	if len(character.Skills) != 0 {
		t.Fatalf("refused learn still wrote %v", character.Skills)
	}

	// Group 174 at level 9 satisfies it.
	character.Skills = []uint32{skillSmashA9}
	result = rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashB1))
	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("prereq-satisfied learn refused: %v", result.Frames[0].Payload)
	}
	if got := *character.SkillPoints; got != 500-117 {
		t.Fatalf("skill points = %d, want %d (the shipped 117-SP cost)", got, 500-117)
	}
}

/*
================
TestSkillLearnRefusals
================
*/
func TestSkillLearnRefusals(t *testing.T) {
	t.Run("unknown skill id", func(t *testing.T) {
		character := testCharacter()
		rt := newSkillTestRuntime(character)
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(0xdead)), skillLearnUnavailable)
	})

	t.Run("already learned (the client asserts on a wrong success ack)", func(t *testing.T) {
		character := testCharacter()
		character.Skills = []uint32{skillPunch}
		rt := newSkillTestRuntime(character)
		result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillPunch))
		assertSkillRefusal(t, result, skillLearnUnavailable)
		if len(character.Skills) != 1 {
			t.Fatalf("duplicate learn mutated the list: %v", character.Skills)
		}
	})

	t.Run("skipped group level", func(t *testing.T) {
		character := testCharacter()
		setMastery(character, chMastery, 10)
		rt := newSkillTestRuntime(character)
		// Level 2 of group 174 with the group unlearned: the client's own
		// mark-learned walk has nothing to replace.
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashA2)), skillLearnRankRefusal)
	})

	t.Run("mastery level below the requirement", func(t *testing.T) {
		character := testCharacter() // Bicheon at the seeded level 1 < 5
		rt := newSkillTestRuntime(character)
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashA1)), skillLearnMasteryLevelRefusal)
		if got := *character.SkillPoints; got != 100 {
			t.Fatalf("refused learn charged SP: %d", got)
		}
	})

	t.Run("mastery record missing (other race's skill)", func(t *testing.T) {
		character := testCharacter() // CH set: no 513 record even at req level 0
		rt := newSkillTestRuntime(character)
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillEUReserve)), wire.ErrCodeSkillLearnRefused)
	})

	t.Run("insufficient skill points", func(t *testing.T) {
		character := testCharacter()
		setMastery(character, chMastery, 5)
		character.SkillPoints = int64Ptr(1)
		rt := newSkillTestRuntime(character)
		result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashA1))
		assertSkillRefusal(t, result, wire.ErrCodeSkillLearnSP)
		if got := *character.SkillPoints; got != 1 {
			t.Fatalf("skill points = %d, want 1 (untouched)", got)
		}
		if len(character.Skills) != 0 {
			t.Fatalf("refused learn still wrote %v", character.Skills)
		}
	})

	t.Run("strength below a custom-data requirement", func(t *testing.T) {
		character := testCharacter() // STR 20 < 200
		rt := newSkillTestRuntime(character)
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillStrGated)), wire.ErrCodeSkillLearnStr)
	})

	t.Run("intellect below a custom-data requirement", func(t *testing.T) {
		character := testCharacter()
		rt := newSkillTestRuntime(character)
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillIntGated)), wire.ErrCodeSkillLearnInt)
	})

	t.Run("malformed bodies", func(t *testing.T) {
		character := testCharacter()
		rt := newSkillTestRuntime(character)
		for _, body := range [][]byte{nil, {0x01}, {0x01, 0x02, 0x03}, {0x01, 0x02, 0x03, 0x04, 0x05}} {
			assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, body), wire.ErrCodeSkillLearnRefused)
		}
	})

	t.Run("delete-pending character", func(t *testing.T) {
		character := testCharacter()
		character.DeletePending = true
		rt := newSkillTestRuntime(character)
		assertSkillRefusal(t, rt.HandleSkillLearn(testDivision, character, skillPayload(skillPunch)), skillLearnUnavailable)
	})
}

// An unavailable skilldata table must refuse, never learn for free: the
// cost and prerequisites are authority DATA (the leveldata posture).
/*
================
TestSkillLearnRefusesWithoutATable
================
*/
func TestSkillLearnRefusesWithoutATable(t *testing.T) {
	character := testCharacter()
	rt := newTestRuntime(character) // Deps.Skills nil

	result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillPunch))

	assertSkillRefusal(t, result, wire.ErrCodeSkillLearnRefused)
	if len(character.Skills) != 0 {
		t.Fatalf("table-less learn still wrote %v", character.Skills)
	}
}

/*
================
assertSkillRefusal
================
*/
func assertSkillRefusal(t *testing.T, result OpResult, wantCode uint8) {
	t.Helper()
	if len(result.Frames) != 1 {
		t.Fatalf("frames = %d, want the refusal only (no SP refresh)", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpSkillLearnResponse {
		t.Fatalf("refusal opcode = 0x%04X, want 0x%04X", result.Frames[0].Opcode, wire.OpSkillLearnResponse)
	}
	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.ResultError {
		t.Fatalf("refusal payload = %v, want [02 err]", payload)
	}
	if payload[1] != wantCode {
		t.Fatalf("error code = 0x%02X, want 0x%02X", payload[1], wantCode)
	}
}

// The commit door makes check-spend-append atomic: a storm of concurrent
// learns can spend at most the SP pool.
/*
================
TestConcurrentSkillLearnsNeverOverspend
================
*/
func TestConcurrentSkillLearnsNeverOverspend(t *testing.T) {
	skills := staticSkills{}
	const cost = 10
	ids := make([]uint32, 40)
	for i := range ids {
		id := uint32(20000 + i)
		ids[i] = id
		skills[id] = enterworld.SkillRow{ID: id, Group: id, Level: 1, SPCost: cost}
	}

	character := testCharacter() // 100 SP -> exactly 10 affordable learns
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Skills:     skills,
	}
	var doorMu sync.Mutex
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, fn func()) {
		doorMu.Lock()
		defer doorMu.Unlock()
		if fn != nil {
			fn()
		}
	}
	rt := NewRuntime(deps)

	var wg sync.WaitGroup
	granted := make([]bool, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func(index int, skillID uint32) {
			defer wg.Done()
			result := rt.HandleSkillLearn(testDivision, character, skillPayload(skillID))
			granted[index] = result.Frames[0].Payload[0] == wire.ResultSuccess
		}(i, id)
	}
	wg.Wait()

	successes := 0
	for _, ok := range granted {
		if ok {
			successes++
		}
	}
	if successes != 10 {
		t.Fatalf("granted %d learns, want exactly the 10 the pool affords", successes)
	}
	if got := *character.SkillPoints; got != 0 {
		t.Fatalf("skill points = %d, want 0", got)
	}
	if got := len(character.Skills); got != 10 {
		t.Fatalf("learned list carries %d ids, want 10", got)
	}
}

// Store-backed continuity: a granted learn commits through the authority
// door, so a watchdog reboot resumes with the skill learned and the SP
// spent - never the pre-spend state.
/*
================
TestSkillLearnSurvivesRestart
================
*/
func TestSkillLearnSurvivesRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	openRuntime := func(seed *enterworld.Character) (*Runtime, *enterworld.Character, *store.Store) {
		authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
		if err != nil {
			t.Fatalf("store.Open: %v", err)
		}
		t.Cleanup(authority.Close)
		existing := authority.Characters().CharactersForDivision(testDivision)
		if len(existing) == 0 && seed != nil {
			if err := authority.CreateCharacter(testDivision, "test-account", seed); err != nil {
				t.Fatalf("CreateCharacter: %v", err)
			}
			existing = authority.Characters().CharactersForDivision(testDivision)
		}
		deps := &enterworld.Deps{
			Characters: authority.Characters(),
			Levels:     testLevels(),
			Skills:     testSkills(),
		}
		deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
			authority.MutateCharacter(c, label, fn)
		}
		var character *enterworld.Character
		if len(existing) > 0 {
			character = existing[0]
		}
		return NewRuntime(deps), character, authority
	}

	seed := testCharacter()
	// Creation always carries the racial base-skill invariant. This
	// scenario proves the newly learned skill is appended and survives
	// alongside that required baseline.
	seed.Skills = []uint32{}
	rt, character, authority := openRuntime(seed)
	setMastery(character, chMastery, 5)

	if got := rt.HandleSkillLearn(testDivision, character, skillPayload(skillSmashA1)).Frames[0].Payload[0]; got != wire.ResultSuccess {
		t.Fatalf("learn refused: %v", got)
	}
	authority.Close()

	_, restored, _ := openRuntime(nil)
	if restored == nil {
		t.Fatal("character lost across the restart")
	}
	wantSkills := []uint32{1, 2, 40, 70, skillSmashA1}
	if !reflect.DeepEqual(restored.Skills, wantSkills) {
		t.Fatalf("skills after reboot = %v, want %v", restored.Skills, wantSkills)
	}
	if got := *restored.SkillPoints; got != 98 {
		t.Fatalf("skill points after reboot = %d, want 98", got)
	}
}
