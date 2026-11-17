package progression

import (
	"sync"
	"testing"

	"opensro.online/server/internal/domain/charactervitals"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const testDivision = "global-official"

// staticLevels is a test enterworld.LevelDataSource: the shipped
// leveldata.txt SP-cost curve's first rows (level 1 -> 1, 4 -> 2, 6 -> 4,
// 10 -> 9) plus a mid-curve row for the training cases, and the shipped
// exp curve's first rows plus the two cap-boundary rows (89/90) for the
// level-up walk cases. Values read off the shipped table so a fake-vs-
// production drift cannot hide (the e2e preflight re-asserts the same
// rows through the real loader).
type staticLevels struct {
	sp    map[int64]int64
	exp   map[int64]int64
	death map[int64]int64
}

func (s staticLevels) SkillPointCost(level int64) (int64, bool) {
	cost, ok := s.sp[level]
	return cost, ok
}

func (s staticLevels) ExpRequired(level int64) (int64, bool) {
	exp, ok := s.exp[level]
	return exp, ok
}

func (s staticLevels) MonsterExpBasis(level int64) (int64, bool) {
	basis, ok := s.death[level]
	return basis, ok
}

func testLevels() staticLevels {
	return staticLevels{
		sp: map[int64]int64{1: 1, 2: 1, 3: 1, 4: 2, 5: 2, 6: 4, 10: 9, 36: 50},
		exp: map[int64]int64{
			1: 118, 2: 470, 3: 1058, 4: 1880, 5: 2938,
			10: 23500, 11: 34898, 50: 10857676,
			89: 265353867, 90: 281672373,
		},
		death: map[int64]int64{11: 259, 50: 2029, 90: 6949},
	}
}

func int64Ptr(v int64) *int64 { return &v }

type emptyItemRefs struct{}

func (emptyItemRefs) ItemRefByCodename(string) (*enterworld.ItemRef, bool) {
	return nil, false
}

// testCharacter is a CH character with the racial mastery set, allocatable
// stat points and enough SP to train once.
func testCharacter() *enterworld.Character {
	c := &enterworld.Character{
		ID:            3,
		Name:          "statTester",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		Level:         int64Ptr(40),
		StatPoints:    int64Ptr(3),
		SkillPoints:   int64Ptr(100),
		Strength:      int64Ptr(enterworld.BaseStat),
		Intellect:     int64Ptr(enterworld.BaseStat),
	}
	c.Masteries = enterworld.DefaultMasteries(enterworld.ResolveCharacterRaceKey(c))
	return c
}

// newTestRuntime wires a runtime whose commit door just runs the closure
// (the store-backed door is exercised by the persistence test).
func newTestRuntime(character *enterworld.Character) *Runtime {
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      emptyItemRefs{},
		Levels:     testLevels(),
	}
	var doorMu sync.Mutex
	deps.MutateCharacter = func(_ *enterworld.Character, _ string, fn func()) {
		doorMu.Lock()
		defer doorMu.Unlock()
		if fn != nil {
			fn()
		}
	}
	return NewRuntime(deps)
}

// masteryPayload encodes a 0x7165 body.
func masteryPayload(masteryID uint32, amount uint8) []byte {
	return wire.NewWriter(5).U32(masteryID).U8(amount).Payload()
}

// chMastery is the first CH racial mastery id (Bicheon).
const chMastery uint32 = 257

func TestAllocStrengthSpendsAPointAndRefreshesTheStatWords(t *testing.T) {
	character := testCharacter()
	rt := newTestRuntime(character)

	result := rt.HandleAllocStr(testDivision, character, nil)

	if len(result.Frames) != 2 {
		t.Fatalf("frames = %d, want the ack + the 0x343C refresh", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpAllocStrResponse {
		t.Fatalf("ack opcode = 0x%04X, want 0x%04X", result.Frames[0].Opcode, wire.OpAllocStrResponse)
	}
	if got := result.Frames[0].Payload; len(got) != 1 || got[0] != wire.ResultSuccess {
		t.Fatalf("ack payload = %v, want the single success byte", got)
	}
	// The ack alone must move the client's counter: a 0x30B3 type-3
	// absolute update riding along would double-apply the spend.
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpPointsUpdate && len(frame.Payload) > 0 && frame.Payload[0] == wire.PointsTypeStat {
			t.Fatal("stat success must NOT carry a 0x30B3 type-3 absolute update (double-decrement)")
		}
	}
	if result.Frames[1].Opcode != wire.OpBaseStats {
		t.Fatalf("second frame = 0x%04X, want the 0x343C base-stat block", result.Frames[1].Opcode)
	}
	if len(result.Frames[1].Payload) != wire.BaseStatsSize {
		t.Fatalf("0x343C payload = %d bytes, want %d", len(result.Frames[1].Payload), wire.BaseStatsSize)
	}

	if got := *character.Strength; got != enterworld.BaseStat+1 {
		t.Fatalf("strength = %d, want %d", got, enterworld.BaseStat+1)
	}
	if got := *character.Intellect; got != enterworld.BaseStat {
		t.Fatalf("intellect = %d, want it untouched", got)
	}
	if got := *character.StatPoints; got != 2 {
		t.Fatalf("stat points = %d, want 2 (one spent)", got)
	}
}

func TestAllocIntellectSpendsAPoint(t *testing.T) {
	character := testCharacter()
	rt := newTestRuntime(character)

	result := rt.HandleAllocInt(testDivision, character, nil)

	if result.Frames[0].Opcode != wire.OpAllocIntResponse {
		t.Fatalf("ack opcode = 0x%04X, want 0x%04X", result.Frames[0].Opcode, wire.OpAllocIntResponse)
	}
	if got := *character.Intellect; got != enterworld.BaseStat+1 {
		t.Fatalf("intellect = %d, want %d", got, enterworld.BaseStat+1)
	}
	if got := *character.Strength; got != enterworld.BaseStat {
		t.Fatalf("strength = %d, want it untouched", got)
	}
	if got := *character.StatPoints; got != 2 {
		t.Fatalf("stat points = %d, want 2", got)
	}
}

func TestStatAllocationDoesNotPartiallyCommitWhenCombatGraphFails(t *testing.T) {
	character := testCharacter()
	character.MissionInventory = []enterworld.InventoryRow{{
		Slot: 6, RefObjID: 999999, Codename: "ITEM_MISSING_COMBAT_ROW",
		TypeFlags:    wire.PackTypeFlags(3, 1, 6, 2),
		VarianceBits: "0", Durability: 1, StackCount: 1,
	}}
	rt := newTestRuntime(character)

	result := rt.HandleAllocStr(testDivision, character, nil)
	if len(result.Frames) != 1 || result.Frames[0].Payload[0] != wire.ResultError {
		t.Fatalf("failed combat graph response = %+v, want one typed refusal", result.Frames)
	}
	if *character.StatPoints != 3 || *character.Strength != enterworld.BaseStat ||
		character.CurrentHP != nil {
		t.Fatalf(
			"refused allocation leaked state: points=%d str=%d hp=%v",
			*character.StatPoints,
			*character.Strength,
			character.CurrentHP,
		)
	}
}

func TestAllocRefusedWithoutPoints(t *testing.T) {
	character := testCharacter()
	character.StatPoints = int64Ptr(0)
	rt := newTestRuntime(character)

	result := rt.HandleAllocStr(testDivision, character, nil)

	if len(result.Frames) != 1 {
		t.Fatalf("frames = %d, want the refusal only (no 0x343C)", len(result.Frames))
	}
	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.ResultError {
		t.Fatalf("refusal payload = %v, want [02 err]", payload)
	}
	if got := *character.Strength; got != enterworld.BaseStat {
		t.Fatalf("refused allocation still raised strength to %d", got)
	}
	if got := *character.StatPoints; got != 0 {
		t.Fatalf("stat points = %d, want 0 (never negative)", got)
	}
}

// A nil StatPoints pool reads as zero, never as "unlimited".
func TestAllocRefusedWhenPointsUnset(t *testing.T) {
	character := testCharacter()
	character.StatPoints = nil
	rt := newTestRuntime(character)

	if result := rt.HandleAllocStr(testDivision, character, nil); len(result.Frames) != 1 {
		t.Fatalf("frames = %d, want the refusal only", len(result.Frames))
	}
	if character.Strength == nil || *character.Strength != enterworld.BaseStat {
		t.Fatalf("strength moved on a refusal: %v", character.Strength)
	}
}

// The native request carries NO body; anything else is a shape the retail
// client cannot compose.
func TestAllocRefusesNonEmptyPayload(t *testing.T) {
	character := testCharacter()
	rt := newTestRuntime(character)

	result := rt.HandleAllocStr(testDivision, character, []byte{0x01})

	if len(result.Frames) != 1 || result.Frames[0].Payload[0] != wire.ResultError {
		t.Fatalf("payload-bearing stat request = %v, want a refusal", result.Frames)
	}
	if got := *character.StatPoints; got != 3 {
		t.Fatalf("stat points = %d, want 3 (untouched)", got)
	}
}

func TestAllocRefusesDeletePendingCharacter(t *testing.T) {
	character := testCharacter()
	character.DeletePending = true
	rt := newTestRuntime(character)

	if result := rt.HandleAllocStr(testDivision, character, nil); len(result.Frames) != 1 {
		t.Fatalf("delete-pending allocation = %v, want a refusal", result.Frames)
	}
	if got := *character.StatPoints; got != 3 {
		t.Fatalf("stat points = %d, want 3 (untouched)", got)
	}
}

// A granted allocation leaves an EXPLICIT current exactly where it was (the
// LEAVE policy: a raised derived maximum is headroom, not a heal). Canonical
// records carry no persisted maxHp/maxMp keys; derivation is the sole authority.
func TestAllocLeavesExplicitCurrentsAtRaisedHeadroom(t *testing.T) {
	character := testCharacter()
	current := int64(123)
	character.CurrentHP = &current
	character.CurrentMP = &current
	rt := newTestRuntime(character)

	result := rt.HandleAllocStr(testDivision, character, nil)

	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("allocation refused: %v", result.Frames[0].Payload)
	}
	if *character.CurrentHP != 123 || *character.CurrentMP != 123 {
		t.Fatalf("currents = %d/%d, want the untouched 123/123 (LEAVE policy)", *character.CurrentHP, *character.CurrentMP)
	}
}

// An ABSENT current reads as "full at max" on every emission, so without
// pinning it the raised maximum would silently drag the bar up with it.
// The door materializes the affected pool's current at its PRE-raise
// derived value: after a +STR at level 40 the character reads 432/454
// (headroom visible), never 454/454 (an invented top-up).
func TestAllocMaterializesAbsentCurrentAtThePreRaiseMax(t *testing.T) {
	character := testCharacter()
	character.CurrentHP = nil
	character.CurrentMP = nil
	rt := newTestRuntime(character)

	if got := rt.HandleAllocStr(testDivision, character, nil).Frames[0].Payload[0]; got != wire.ResultSuccess {
		t.Fatalf("allocation refused: %v", got)
	}
	// Pre-raise: level 40, STR 20 -> trunc(1.02^39 * 200) = 432.
	if character.CurrentHP == nil || *character.CurrentHP != 432 {
		t.Fatalf("materialized currentHp = %v, want the pre-raise 432 (454 means the bar followed the max up)", character.CurrentHP)
	}
	// The untouched pool stays absent: its max did not move, so there is
	// nothing to pin and creation's field-free shape survives.
	if character.CurrentMP != nil {
		t.Fatalf("currentMp = %v, want it still absent (+STR must not touch the MP pool)", *character.CurrentMP)
	}
}

// The client stores STR/INT as u16 words, so the server must never hand it
// a value it cannot hold.
func TestAllocClampsAtTheNativeWordCeiling(t *testing.T) {
	character := testCharacter()
	character.Strength = int64Ptr(charactervitals.StatWordMax)
	rt := newTestRuntime(character)

	result := rt.HandleAllocStr(testDivision, character, nil)

	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("allocation at the ceiling refused: %v", result.Frames[0].Payload)
	}
	if got := *character.Strength; got != charactervitals.StatWordMax {
		t.Fatalf("strength = %d, want it clamped at %d", got, charactervitals.StatWordMax)
	}
}

func TestMasteryTrainingRaisesTheLevelAndChargesSP(t *testing.T) {
	character := testCharacter()
	// Seeded racial masteries sit at level 0 (the free-train case lives
	// in masteryseed_pricing_test.go); a PRICED train needs a trained
	// row, so raise the mastery to 1 - the next level is 2 and the cost
	// is the row of the level being left (row 1).
	setMastery(character, chMastery, 1)
	rt := newTestRuntime(character)
	cost, _ := testLevels().SkillPointCost(1)

	result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))

	if len(result.Frames) != 2 {
		t.Fatalf("frames = %d, want the ack + the SP refresh", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpMasteryLevelUpResponse {
		t.Fatalf("ack opcode = 0x%04X, want 0x%04X", result.Frames[0].Opcode, wire.OpMasteryLevelUpResponse)
	}
	// [1][u32 id][u8 newLevel] - the client writes newLevel straight into
	// the mastery record, so it must be the POST-training level.
	ack := result.Frames[0].Payload
	if len(ack) != 6 || ack[0] != wire.ResultSuccess || ack[5] != 2 {
		t.Fatalf("mastery ack = %v, want [01 id..  02]", ack)
	}
	sp := result.Frames[1]
	if sp.Opcode != wire.OpPointsUpdate {
		t.Fatalf("second frame = 0x%04X, want 0x30B3", sp.Opcode)
	}
	if len(sp.Payload) != 6 || sp.Payload[0] != wire.PointsTypeSkill {
		t.Fatalf("SP frame = %v, want a type-2 absolute update", sp.Payload)
	}
	if sp.Payload[5] != 0 {
		t.Fatal("the SP refresh must be silent - a gain/loss toast would read as a penalty for a purchase")
	}

	level, ok := enterworld.MasteryLevel(character, chMastery)
	if !ok || level != 2 {
		t.Fatalf("mastery level = %d/%v, want 2/true", level, ok)
	}
	if got := *character.SkillPoints; got != 100-cost {
		t.Fatalf("skill points = %d, want %d", got, 100-cost)
	}
}

func TestMasteryTrainingRefusals(t *testing.T) {
	t.Run("unknown mastery id", func(t *testing.T) {
		character := testCharacter()
		rt := newTestRuntime(character)
		// An EU mastery on a CH character: no record, so training it
		// would invent state the equip plane treats as absent.
		result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(513, 1))
		assertMasteryRefusal(t, result, character, 0)
	})

	t.Run("amount other than one", func(t *testing.T) {
		character := testCharacter()
		rt := newTestRuntime(character)
		result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 5))
		assertMasteryRefusal(t, result, character, 0)
	})

	t.Run("next level would pass the character level", func(t *testing.T) {
		character := testCharacter()
		character.Level = int64Ptr(1)
		// The seeded level-0 row legitimately trains 0->1 at character
		// level 1 (newLevel 1 <= 1), so the gate needs a TRAINED row:
		// 1 -> 2 passes the character level.
		setMastery(character, chMastery, 1)
		rt := newTestRuntime(character)
		result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))
		assertMasteryRefusal(t, result, character, 1)
		if code := result.Frames[0].Payload[1]; code != wire.ErrCodeMasteryLevelLimit {
			t.Fatalf("error code = 0x%02X, want the level-limit notice 0x%02X", code, wire.ErrCodeMasteryLevelLimit)
		}
	})

	t.Run("at the 120 cap", func(t *testing.T) {
		character := testCharacter()
		character.Level = int64Ptr(140)
		for i := range character.Masteries {
			if character.Masteries[i].ID == chMastery {
				character.Masteries[i].Level = MasteryLevelCap
			}
		}
		rt := newTestRuntime(character)
		result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))
		assertMasteryRefusal(t, result, character, MasteryLevelCap)
		if code := result.Frames[0].Payload[1]; code != wire.ErrCodeMasteryLevelLimit {
			t.Fatalf("error code = 0x%02X, want the level-limit notice", code)
		}
	})

	t.Run("insufficient skill points", func(t *testing.T) {
		character := testCharacter()
		character.SkillPoints = int64Ptr(0)
		// A level-0 row trains free even at SP 0 (the pinned retail
		// behaviour), so the SP gate needs a PRICED train: 1 -> 2
		// costs row 1 = 1 > 0.
		setMastery(character, chMastery, 1)
		rt := newTestRuntime(character)
		result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))
		assertMasteryRefusal(t, result, character, 1)
		if code := result.Frames[0].Payload[1]; code != wire.ErrCodeMasterySkillPoints {
			t.Fatalf("error code = 0x%02X, want the mastery-SP notice 0x%02X", code, wire.ErrCodeMasterySkillPoints)
		}
		if got := *character.SkillPoints; got != 0 {
			t.Fatalf("skill points = %d, want 0 (never negative)", got)
		}
	})

	t.Run("delete-pending character", func(t *testing.T) {
		character := testCharacter()
		character.DeletePending = true
		rt := newTestRuntime(character)
		assertMasteryRefusal(t, rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1)), character, 0)
	})

	t.Run("malformed body", func(t *testing.T) {
		character := testCharacter()
		rt := newTestRuntime(character)
		assertMasteryRefusal(t, rt.HandleMasteryLevelUp(testDivision, character, []byte{0x01, 0x02}), character, 0)
	})
}

// An unpriceable level must refuse, never train for free: the cost is
// authority DATA (leveldata), and a missing table is a degraded boot.
// Only the pinned-free 0->1 train is exempt (it has no row to price -
// masteryseed_pricing_test.go), so this gate needs a trained row.
func TestMasteryTrainingRefusesWithoutACostTable(t *testing.T) {
	character := testCharacter()
	setMastery(character, chMastery, 1)
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

	assertMasteryRefusal(t, result, character, 1)
	if got := *character.SkillPoints; got != 100 {
		t.Fatalf("skill points = %d, want 100 (no free training)", got)
	}
}

// assertMasteryRefusal checks the single-frame refusal shape and that the
// probed mastery still sits EXACTLY at the level the case started it at -
// a refusal must never move a level.
func assertMasteryRefusal(t *testing.T, result OpResult, character *enterworld.Character, wantLevel int64) {
	t.Helper()
	if len(result.Frames) != 1 {
		t.Fatalf("frames = %d, want the refusal only (no SP refresh)", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpMasteryLevelUpResponse {
		t.Fatalf("refusal opcode = 0x%04X, want 0x%04X", result.Frames[0].Opcode, wire.OpMasteryLevelUpResponse)
	}
	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.ResultError {
		t.Fatalf("refusal payload = %v, want [02 err]", payload)
	}
	if level, _ := enterworld.MasteryLevel(character, chMastery); level != wantLevel {
		t.Fatalf("refused training moved the mastery level to %d, want the untouched %d", level, wantLevel)
	}
}

// ---- total-mastery allowance (notice 07:05) ----

// euMastery is the first EU racial mastery id.
const euMastery uint32 = 513

// testEuCharacter is the European twin of testCharacter: codename AND
// race index both European, so the race helpers resolve it EU whichever
// signal they read.
func testEuCharacter(level int64) *enterworld.Character {
	c := &enterworld.Character{
		ID:            4,
		Name:          "euStatTester",
		ModelCodename: "CHAR_EU_WOMAN_ADVENTURER",
		RaceIndex:     int64Ptr(enterworld.RaceEurope),
		Level:         int64Ptr(level),
		StatPoints:    int64Ptr(3),
		SkillPoints:   int64Ptr(100),
		Strength:      int64Ptr(enterworld.BaseStat),
		Intellect:     int64Ptr(enterworld.BaseStat),
	}
	c.Masteries = enterworld.DefaultMasteries(enterworld.ResolveCharacterRaceKey(c))
	return c
}

// loadMasterySum arranges the character's mastery levels to sum to total:
// the mastery about to be trained stays at level 1 (so its next level is
// 2, priced in testLevels and under every per-mastery gate) and the rest
// spreads across the other racial records within the 120 per-mastery cap,
// so the TOTAL gate is the only gate that can refuse.
func loadMasterySum(t *testing.T, c *enterworld.Character, trainID uint32, total int64) {
	t.Helper()
	others := make([]int, 0, len(c.Masteries))
	for i := range c.Masteries {
		if c.Masteries[i].ID == trainID {
			c.Masteries[i].Level = 1
		} else {
			others = append(others, i)
		}
	}
	remaining := total - 1
	for n, idx := range others {
		share := remaining / int64(len(others)-n)
		if share > MasteryLevelCap {
			share = MasteryLevelCap
		}
		c.Masteries[idx].Level = share
		remaining -= share
	}
	if remaining != 0 {
		t.Fatalf("cannot spread mastery sum %d over %d records within the per-mastery cap", total, len(c.Masteries))
	}
	if got := masteryLevelSum(c); got != total {
		t.Fatalf("mastery sum = %d, want %d", got, total)
	}
}

func assertTotalLimitRefusal(t *testing.T, result OpResult) {
	t.Helper()
	if len(result.Frames) != 1 {
		t.Fatalf("frames = %d, want the refusal only (no SP refresh)", len(result.Frames))
	}
	payload := result.Frames[0].Payload
	if len(payload) != 2 || payload[0] != wire.ResultError {
		t.Fatalf("refusal payload = %v, want [02 err]", payload)
	}
	if payload[1] != wire.ErrCodeMasteryTotalLimit {
		t.Fatalf("error code = 0x%02X, want the total-limit notice 0x%02X", payload[1], wire.ErrCodeMasteryTotalLimit)
	}
}

// The two rules are structurally different - Chinese flat, European
// level-scaled - and keyed to the NATIVE country byte (0 China / 1
// Europe), which is INVERTED from the Node RaceIndex enum. Every level
// here is chosen so the two races' caps cannot coincide (2 x level != 300
// everywhere below the clamp, and 240 != 300 at it): a swapped branch
// fails every assertion.
func TestTotalMasteryCapRulesPerRace(t *testing.T) {
	ch40 := testCharacter()
	ch40.Level = int64Ptr(40)
	ch80 := testCharacter()
	ch80.Level = int64Ptr(80)
	// Flat means flat: the Chinese cap must not move with the level.
	if got := totalMasteryCap(ch40); got != TotalMasteryCapChina {
		t.Fatalf("CH cap at level 40 = %d, want the flat %d", got, TotalMasteryCapChina)
	}
	if got := totalMasteryCap(ch80); got != TotalMasteryCapChina {
		t.Fatalf("CH cap at level 80 = %d, want the flat %d (level-scaled = swapped races)", got, TotalMasteryCapChina)
	}

	// Scaled means scaled: the European cap must track 2 x level...
	if got := totalMasteryCap(testEuCharacter(40)); got != 80 {
		t.Fatalf("EU cap at level 40 = %d, want 80 (flat = swapped races)", got)
	}
	if got := totalMasteryCap(testEuCharacter(80)); got != 160 {
		t.Fatalf("EU cap at level 80 = %d, want 160", got)
	}
	// ...and clamp at the 0xf0 ceiling, never reach 300.
	if got := totalMasteryCap(testEuCharacter(150)); got != TotalMasteryCapEuropeCeiling {
		t.Fatalf("EU cap at level 150 = %d, want the %d ceiling", got, TotalMasteryCapEuropeCeiling)
	}
}

// The behavioral swap detector: the SAME mastery sum (250) at the SAME
// character level (45) must train on a Chinese character (cap 300) and
// refuse on a European one (cap 2x45 = 90). 300 and 90 cannot coincide;
// with the races swapped BOTH halves fail (CH would get min(90,240)=90
// and refuse, EU would get 300 flat and train).
func TestTotalMasteryCapIsNotSwappedBetweenRaces(t *testing.T) {
	ch := testCharacter()
	ch.Level = int64Ptr(45)
	loadMasterySum(t, ch, chMastery, 250)
	result := newTestRuntime(ch).HandleMasteryLevelUp(testDivision, ch, masteryPayload(chMastery, 1))
	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("CH sum 250 at level 45 refused (%v): the Chinese flat 300 is not being applied to Chinese", result.Frames[0].Payload)
	}

	eu := testEuCharacter(45)
	loadMasterySum(t, eu, euMastery, 250)
	result = newTestRuntime(eu).HandleMasteryLevelUp(testDivision, eu, masteryPayload(euMastery, 1))
	assertTotalLimitRefusal(t, result)
	if level, _ := enterworld.MasteryLevel(eu, euMastery); level != 1 {
		t.Fatalf("refused training moved the mastery level to %d", level)
	}
	if got := *eu.SkillPoints; got != 100 {
		t.Fatalf("refused training spent SP: %d, want 100", got)
	}
}

// The Chinese budget binds exactly at 300: sum 300 refuses the +1, sum
// 299 trains to exactly 300.
func TestChineseTotalCapBindsAtExactly300(t *testing.T) {
	character := testCharacter()
	character.Level = int64Ptr(60)
	loadMasterySum(t, character, chMastery, 300)
	rt := newTestRuntime(character)
	assertTotalLimitRefusal(t, rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1)))

	loadMasterySum(t, character, chMastery, 299)
	result := rt.HandleMasteryLevelUp(testDivision, character, masteryPayload(chMastery, 1))
	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("training to exactly the cap refused: %v", result.Frames[0].Payload)
	}
	if got := masteryLevelSum(character); got != 300 {
		t.Fatalf("post-training sum = %d, want exactly 300", got)
	}
}

// The European budget binds at 2 x level (60 at level 30) and at the 240
// ceiling once 2 x level passes it (level 125).
func TestEuropeanTotalCapBindsAtTwiceLevelAndAtTheCeiling(t *testing.T) {
	eu := testEuCharacter(30)
	loadMasterySum(t, eu, euMastery, 60)
	rt := newTestRuntime(eu)
	assertTotalLimitRefusal(t, rt.HandleMasteryLevelUp(testDivision, eu, masteryPayload(euMastery, 1)))

	loadMasterySum(t, eu, euMastery, 59)
	result := rt.HandleMasteryLevelUp(testDivision, eu, masteryPayload(euMastery, 1))
	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("training to exactly 2 x level refused: %v", result.Frames[0].Payload)
	}
	if got := masteryLevelSum(eu); got != 60 {
		t.Fatalf("post-training sum = %d, want exactly 60", got)
	}

	high := testEuCharacter(125) // 2 x 125 = 250, past the 240 ceiling
	loadMasterySum(t, high, euMastery, 240)
	rt = newTestRuntime(high)
	assertTotalLimitRefusal(t, rt.HandleMasteryLevelUp(testDivision, high, masteryPayload(euMastery, 1)))

	loadMasterySum(t, high, euMastery, 239)
	result = rt.HandleMasteryLevelUp(testDivision, high, masteryPayload(euMastery, 1))
	if result.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("training to exactly the 240 ceiling refused: %v", result.Frames[0].Payload)
	}
}

// A character already over its allowance is refused - with the TOTAL
// notice, even when SP is also short (the budget gate precedes pricing) -
// and is never repaired or damaged: stored levels stand untouched. Since
// the level-0 seed landed, a fresh character can no longer START over
// budget (that was the old level-1 seed's sum-6 artifact); the state
// still exists in imported history (a character whose trained sum passes
// its low-level EU budget), so it is constructed manually here: all six
// EU rows trained to 1 at character level 2 = sum 6 over the cap
// min(2x2, 240) = 4.
func TestOverCapCharacterOnlyRefusesFurtherTraining(t *testing.T) {
	eu := testEuCharacter(2)
	for i := range eu.Masteries {
		eu.Masteries[i].Level = 1
	}
	eu.SkillPoints = int64Ptr(0)
	rt := newTestRuntime(eu)

	assertTotalLimitRefusal(t, rt.HandleMasteryLevelUp(testDivision, eu, masteryPayload(euMastery, 1)))

	for _, row := range eu.Masteries {
		if row.Level != 1 {
			t.Fatalf("over-cap refusal rewrote mastery %d to level %d", row.ID, row.Level)
		}
	}
	if got := *eu.SkillPoints; got != 0 {
		t.Fatalf("over-cap refusal moved SP to %d", got)
	}
}

// The commit door makes check-decrement-increment atomic: a storm of
// concurrent allocations can spend at most the pool and never drives it
// negative.
func TestConcurrentAllocationsNeverOverspend(t *testing.T) {
	character := testCharacter()
	character.StatPoints = int64Ptr(10)
	rt := newTestRuntime(character)

	var wg sync.WaitGroup
	granted := make([]bool, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result := rt.HandleAllocStr(testDivision, character, nil)
			granted[index] = result.Frames[0].Payload[0] == wire.ResultSuccess
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, ok := range granted {
		if ok {
			successes++
		}
	}
	if successes != 10 {
		t.Fatalf("granted %d allocations, want exactly the 10 available points", successes)
	}
	if got := *character.StatPoints; got != 0 {
		t.Fatalf("stat points = %d, want 0", got)
	}
	if got := *character.Strength; got != enterworld.BaseStat+10 {
		t.Fatalf("strength = %d, want %d", got, enterworld.BaseStat+10)
	}
}
