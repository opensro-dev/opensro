/*
===========================================================================

levelup_test.go - progression state, packet order, and refusal boundaries.

Curve rows 118/470/1058 exercise single and multiple crossings. One seeded
stat point distinguishes the absolute wire total from a per-level grant.

===========================================================================
*/

package progression

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
levelupTestCharacter

Seeds a fresh Chinese character with deliberately nonzero unspent points.
================
*/
func levelupTestCharacter() *enterworld.Character {
	c := &enterworld.Character{
		ID:            2,
		Name:          "levelupTester",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		Level:         int64Ptr(1),
		StatPoints:    int64Ptr(1),
		SkillPoints:   int64Ptr(0),
		Strength:      int64Ptr(enterworld.BaseStat),
		Intellect:     int64Ptr(enterworld.BaseStat),
	}
	c.Masteries = enterworld.DefaultMasteries(enterworld.ResolveCharacterRaceKey(c))
	return c
}

/*
================
frameHex

Displays the emitted bytes without using the encoder under test.
================
*/
func frameHex(t *testing.T, frame wire.Frame) string {
	t.Helper()
	return hex.EncodeToString(frame.Payload)
}

/*
================
TestGrantExperienceAccumulatesWithoutCrossing

A grant below the next threshold changes EXP without a level or recovery.
================
*/
func TestGrantExperienceAccumulatesWithoutCrossing(t *testing.T) {
	character := levelupTestCharacter()
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 50, 0, 0)

	if len(result.Frames) != 1 {
		t.Fatalf("frames = %d, want exactly 1 (0x30D2)", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpExpUpdate {
		t.Fatalf("opcode = 0x%04X, want 0x30D2", result.Frames[0].Opcode)
	}
	// [gid=0 u32][+50 s32][0 s32][flags=0 u8], no tail.
	if got, want := frameHex(t, result.Frames[0]), "00000000320000000000000000"; got != want {
		t.Errorf("0x30D2 payload = %s, want %s", got, want)
	}
	if character.Experience == nil || *character.Experience != 50 {
		t.Errorf("persisted exp = %v, want 50", character.Experience)
	}
	if *character.Level != 1 || *character.StatPoints != 1 {
		t.Errorf("level/statPoints moved on a non-crossing grant: %d/%d", *character.Level, *character.StatPoints)
	}
	if *character.Strength != enterworld.BaseStat || *character.Intellect != enterworld.BaseStat {
		t.Errorf("stats moved on a non-crossing grant: %d/%d", *character.Strength, *character.Intellect)
	}
}

/*
================
TestGrantExperienceMultiLevelSingleGrant

Two crossings commit final stats and recovered gauges before the EXP tail.
================
*/
func TestGrantExperienceMultiLevelSingleGrant(t *testing.T) {
	character := levelupTestCharacter()
	character.Experience = int64Ptr(50)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 550, 0, 0)

	if len(result.Frames) != 4 {
		t.Fatalf("frames = %d, want presentation, maxima, currents, EXP", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpLevelUpEffect {
		t.Fatalf("frame[0] = 0x%04X, want 0x36B0", result.Frames[0].Opcode)
	}
	if len(result.Broadcast) != 1 || result.Broadcast[0].Opcode != wire.OpLevelUpEffect {
		t.Fatalf("broadcast = %+v, want the single public 0x36B0 presentation", result.Broadcast)
	}
	// gid = 100000 + character ID 2.
	if got, want := frameHex(t, result.Frames[0]), "a2860100"; got != want {
		t.Errorf("0x36B0 payload = %s, want %s (gid 100002)", got, want)
	}
	if result.Frames[1].Opcode != wire.OpBaseStats {
		t.Fatalf("frame[1] = 0x%04X, want 0x343C", result.Frames[1].Opcode)
	}
	// Canonical post-level ParamKeeper projection: phy 6..7, mag 10..12,
	// def 4/6, hit/parry 13; then MaxHP/MP 228 and STR/INT 22.
	wantBlock := "06000000070000000a0000000c000000040006000d000d00" + "e4000000" + "e4000000" + "1600" + "1600"
	if got := frameHex(t, result.Frames[1]); got != wantBlock {
		t.Errorf("0x343C payload = %s, want %s", got, wantBlock)
	}
	if result.Frames[2].Opcode != vitalsUpdateOpcode || result.Frames[3].Opcode != wire.OpExpUpdate {
		t.Fatalf("grant tail = %+v, want currents then EXP", result.Frames[2:])
	}
	// [gid=0][+550][0][flags=0][statPoints=7 ABSOLUTE]: 1 seeded + 3x2.
	if got, want := frameHex(t, result.Frames[3]), "000000002602000000000000000700"; got != want {
		t.Errorf("0x30D2 payload = %s, want %s", got, want)
	}

	if *character.Level != 3 || *character.Experience != 12 {
		t.Errorf("persisted level/exp = %d/%d, want 3/12", *character.Level, *character.Experience)
	}
	if *character.StatPoints != 7 {
		t.Errorf("statPoints = %d, want 7", *character.StatPoints)
	}
	if *character.Strength != 22 || *character.Intellect != 22 {
		t.Errorf("STR/INT = %d/%d, want 22/22 (+1/+1 per level)", *character.Strength, *character.Intellect)
	}
	if character.MaxLevel == nil || *character.MaxLevel != 3 {
		t.Errorf("MaxLevel watermark = %v, want 3", character.MaxLevel)
	}
	if character.CurrentHP == nil || *character.CurrentHP != 228 {
		t.Errorf("CurrentHP = %v, want recovered 228", character.CurrentHP)
	}
	if character.CurrentMP == nil || *character.CurrentMP != 228 {
		t.Errorf("CurrentMP = %v, want recovered 228", character.CurrentMP)
	}
}

/*
================
TestLevelGrantDoesNotPartiallyCommitWhenCombatGraphFails

An invalid equipped item must refuse the complete candidate transaction.
================
*/
func TestLevelGrantDoesNotPartiallyCommitWhenCombatGraphFails(t *testing.T) {
	character := levelupTestCharacter()
	character.Experience = int64Ptr(50)
	character.MissionInventory = []enterworld.InventoryRow{{
		Slot: 6, RefObjID: 999999, Codename: "ITEM_MISSING_COMBAT_ROW",
		TypeFlags:    wire.PackTypeFlags(3, 1, 6, 2),
		VarianceBits: "0", Durability: 1, StackCount: 1,
	}}
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 550, 0, 0)
	if len(result.Frames) != 0 {
		t.Fatalf("failed combat graph emitted frames: %+v", result.Frames)
	}
	if *character.Level != 1 || *character.Experience != 50 ||
		*character.StatPoints != 1 || *character.Strength != enterworld.BaseStat ||
		*character.Intellect != enterworld.BaseStat ||
		character.CurrentHP != nil || character.CurrentMP != nil {
		t.Fatalf("refused grant leaked progression: %+v", character)
	}
}

/*
================
TestGrantExperienceEmitsAppliedNotRequestedDelta

The level cap truncates the grant; the client must receive only the amount
the server applied or its own curve walk would advance beyond authority.
================
*/
func TestGrantExperienceEmitsAppliedNotRequestedDelta(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(89)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 0x7fffffff, 0, 0)

	if *character.Level != 90 {
		t.Fatalf("level = %d, want 90 (cap)", *character.Level)
	}
	// Frozen just below the level-90 boundary (req 281672373).
	if *character.Experience != 281672372 {
		t.Errorf("exp = %d, want 281672372 (req-1)", *character.Experience)
	}
	// applied = crossed(265353867) + 281672372 - 0.
	const applied = 265353867 + 281672372
	if len(result.Frames) == 0 {
		t.Fatal("no frames emitted")
	}
	payload := result.Frames[len(result.Frames)-1].Payload
	if len(payload) != 15 {
		t.Fatalf("0x30D2 length = %d, want 15 (levelled)", len(payload))
	}
	gotDelta := int64(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
	if gotDelta != applied {
		t.Errorf("emitted expDelta = %d, want applied %d (not the requested %d)", gotDelta, applied, 0x7fffffff)
	}

	// A further grant at the frozen ceiling applies nothing and emits
	// NOTHING (an all-zero 0x30D2 would be noise the client toasts on).
	second := rt.GrantExperience(character, 1000, 0, 0)
	if len(second.Frames) != 0 {
		t.Errorf("grant at frozen cap emitted %d frame(s), want none", len(second.Frames))
	}
	if *character.Experience != 281672372 || *character.Level != 90 {
		t.Errorf("frozen state moved: level %d exp %d", *character.Level, *character.Experience)
	}
}

/*
================
TestGrantExperienceRefusesWithoutCurveRow

Missing curve authority refuses the whole grant before any crossing commits.
================
*/
func TestGrantExperienceRefusesWithoutCurveRow(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(5) // fake table has row 5 but not row 6
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 3000, 0, 0)

	if len(result.Frames) != 0 {
		t.Fatalf("frames = %d, want none on refusal", len(result.Frames))
	}
	if *character.Level != 5 {
		t.Errorf("level = %d, want untouched 5", *character.Level)
	}
	if character.Experience != nil && *character.Experience != 0 {
		t.Errorf("exp = %d, want untouched", *character.Experience)
	}

	// Same posture with NO table at all: refuse, never level free.
	rtNoTable := newTestRuntime(character)
	deps, ok := rtNoTable.deps.(*enterworld.Deps)
	if !ok {
		t.Fatalf("runtime dependencies are %T, want *enterworld.Deps test fixture", rtNoTable.deps)
	}
	deps.Levels = nil
	if result := rtNoTable.GrantExperience(character, 10, 0, 0); len(result.Frames) != 0 {
		t.Error("grant with no leveldata source must refuse")
	}
}

/*
================
TestGrantExperienceSkillExpWrapsAndYieldsSP

Seeded SP distinguishes the absolute update from the one-point yield. The
skill-EXP remainder still follows the client's modulo-400 accumulator.
================
*/
func TestGrantExperienceSkillExpWrapsAndYieldsSP(t *testing.T) {
	character := levelupTestCharacter()
	character.SkillExp = int64Ptr(350)
	character.SkillPoints = int64Ptr(5)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 0, 100, 0)

	if len(result.Frames) != 2 {
		t.Fatalf("frames = %d, want 2 (0x30B3, 0x30D2)", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpPointsUpdate {
		t.Fatalf("frame[0] = 0x%04X, want 0x30B3", result.Frames[0].Opcode)
	}
	// [type=2][sp=6 u32 ABSOLUTE][notify=0]: 5 seeded + 1 period.
	if got, want := frameHex(t, result.Frames[0]), "020600000000"; got != want {
		t.Errorf("0x30B3 payload = %s, want %s", got, want)
	}
	if result.Frames[1].Opcode != wire.OpExpUpdate {
		t.Fatalf("frame[1] = 0x%04X, want 0x30D2", result.Frames[1].Opcode)
	}
	// [gid=0][0][+100][flags=0], 13 bytes, no tail: 0x30D2 carries the
	// skill-exp DELTA only - the SP conversion never rides it (the
	// client-side handler leaves +0x838 alone).
	if got, want := frameHex(t, result.Frames[1]), "00000000000000006400000000"; got != want {
		t.Errorf("0x30D2 payload = %s, want %s", got, want)
	}
	if *character.SkillExp != 50 {
		t.Errorf("skillExp = %d, want wrapped 50 ((350+100) mod 400)", *character.SkillExp)
	}
	if *character.SkillPoints != 6 {
		t.Errorf("skillPoints = %d, want 6 (5 + 1 period)", *character.SkillPoints)
	}
}

/*
================
TestGrantExperienceSkillExpBelowPeriodYieldsNothing

An incomplete skill-EXP period changes the remainder without awarding SP.
================
*/
func TestGrantExperienceSkillExpBelowPeriodYieldsNothing(t *testing.T) {
	character := levelupTestCharacter()
	character.SkillExp = int64Ptr(100)
	character.SkillPoints = int64Ptr(5)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 0, 250, 0)

	if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpExpUpdate {
		t.Fatalf("frames = %d, want exactly one 0x30D2", len(result.Frames))
	}
	if *character.SkillExp != 350 || *character.SkillPoints != 5 {
		t.Errorf("skillExp/SP = %d/%d, want 350/5 (no period completed)", *character.SkillExp, *character.SkillPoints)
	}
}

/*
================
TestGrantExperienceSkillExpMultiPeriodSingleGrant

Multiple completed periods produce one absolute SP update and one remainder.
================
*/
func TestGrantExperienceSkillExpMultiPeriodSingleGrant(t *testing.T) {
	character := levelupTestCharacter()
	character.SkillPoints = int64Ptr(5)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 0, 1000, 0)

	if len(result.Frames) != 2 {
		t.Fatalf("frames = %d, want 2 (0x30B3, 0x30D2)", len(result.Frames))
	}
	// [type=2][sp=7 u32 ABSOLUTE][notify=0]: 5 + 1000/400.
	if got, want := frameHex(t, result.Frames[0]), "020700000000"; got != want {
		t.Errorf("0x30B3 payload = %s, want %s", got, want)
	}
	// [gid=0][0][+1000][flags=0], 13 bytes.
	if got, want := frameHex(t, result.Frames[1]), "0000000000000000e803000000"; got != want {
		t.Errorf("0x30D2 payload = %s, want %s", got, want)
	}
	if *character.SkillExp != 200 {
		t.Errorf("skillExp = %d, want remainder 200 (1000 mod 400)", *character.SkillExp)
	}
	if *character.SkillPoints != 7 {
		t.Errorf("skillPoints = %d, want 7 (5 + 2 periods)", *character.SkillPoints)
	}
}

/*
================
TestGrantExperienceFullBurstOrderWithSPChange

SP conversion follows the completed level transition and precedes EXP.
================
*/
func TestGrantExperienceFullBurstOrderWithSPChange(t *testing.T) {
	character := levelupTestCharacter()
	character.Experience = int64Ptr(50)
	character.SkillPoints = int64Ptr(5)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 550, 400, 0)

	wantOpcodes := []uint16{wire.OpLevelUpEffect, wire.OpBaseStats, vitalsUpdateOpcode, wire.OpPointsUpdate, wire.OpExpUpdate}
	if len(result.Frames) != len(wantOpcodes) {
		t.Fatalf("frames = %d, want %d", len(result.Frames), len(wantOpcodes))
	}
	for i, want := range wantOpcodes {
		if result.Frames[i].Opcode != want {
			t.Errorf("frame[%d] = 0x%04X, want 0x%04X", i, result.Frames[i].Opcode, want)
		}
	}
	// [gid=0][+550][+400][flags=0][statPoints=7 ABSOLUTE].
	if got, want := frameHex(t, result.Frames[4]), "000000002602000090010000000700"; got != want {
		t.Errorf("0x30D2 payload = %s, want %s", got, want)
	}
	// [type=2][sp=6 ABSOLUTE][notify=0]: 5 + 400/400.
	if got, want := frameHex(t, result.Frames[3]), "020600000000"; got != want {
		t.Errorf("0x30B3 payload = %s, want %s", got, want)
	}
	if *character.Level != 3 || *character.SkillExp != 0 || *character.SkillPoints != 6 {
		t.Errorf("persisted level/skillExp/SP = %d/%d/%d, want 3/0/6",
			*character.Level, *character.SkillExp, *character.SkillPoints)
	}
}

/*
================
TestGrantExperienceRefusesNoOpAndImpossibleInputs

Zero grants and invalid recipients have no output. Skill EXP is gain-only;
ordinary EXP losses are covered by the death-penalty tests below.
================
*/
func TestGrantExperienceRefusesNoOpAndImpossibleInputs(t *testing.T) {
	character := levelupTestCharacter()
	rt := newTestRuntime(character)

	for name, run := range map[string]func() OpResult{
		"both zero":      func() OpResult { return rt.GrantExperience(character, 0, 0, 0) },
		"negative skill": func() OpResult { return rt.GrantExperience(character, 0, -50, 0) },
		"nil character":  func() OpResult { return rt.GrantExperience(nil, 50, 0, 0) },
	} {
		if result := run(); len(result.Frames) != 0 {
			t.Errorf("%s: emitted %d frame(s), want none", name, len(result.Frames))
		}
	}
	if character.Experience != nil {
		t.Errorf("exp moved: %d", *character.Experience)
	}

	character.DeletePending = true
	if result := rt.GrantExperience(character, 50, 0, 0); len(result.Frames) != 0 {
		t.Error("delete-pending character accepted a grant")
	}
}

/*
================
TestOrdinaryDeathPenaltyProtectsThroughLevelTen

The native beginner protection returns before calculating any EXP loss.
================
*/
func TestOrdinaryDeathPenaltyProtectsThroughLevelTen(t *testing.T) {
	for _, level := range []int64{1, 4, 10} {
		character := levelupTestCharacter()
		character.Level = int64Ptr(level)
		character.MaxLevel = int64Ptr(level)
		character.Experience = int64Ptr(123)
		rt := newTestRuntime(character)

		result := rt.ApplyDeathPenalty(character, OrdinaryDeathPenalty())
		if len(result.Frames) != 0 || *character.Experience != 123 || *character.Level != level {
			t.Fatalf("protected level %d death moved progression: frames=%+v level/exp=%d/%d",
				level, result.Frames, *character.Level, *character.Experience)
		}
	}
}

/*
================
TestOrdinaryDeathPenaltyUsesRetailTwoPercentAndCanDelevel

Losing a level changes the current curve position but preserves earned stats.
================
*/
func TestOrdinaryDeathPenaltyUsesRetailTwoPercentAndCanDelevel(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(11)
	character.MaxLevel = int64Ptr(11)
	character.Experience = int64Ptr(0)
	character.StatPoints = int64Ptr(7)
	character.Strength = int64Ptr(30)
	character.Intellect = int64Ptr(28)
	character.CurrentHP = int64Ptr(0)
	character.CurrentMP = int64Ptr(100)
	rt := newTestRuntime(character)

	result := rt.ApplyDeathPenalty(character, OrdinaryDeathPenalty())
	if len(result.Frames) != 2 {
		t.Fatalf("death-loss frames = %d, want 343C + 30D2 after delevel", len(result.Frames))
	}
	if result.Frames[0].Opcode != wire.OpBaseStats || result.Frames[1].Opcode != wire.OpExpUpdate {
		t.Fatalf("death-loss opcodes = %04X/%04X, want 343C/30D2",
			result.Frames[0].Opcode, result.Frames[1].Opcode)
	}
	// trunc(34898 * .02) = 697; cap 259*100 does not bind.
	if got, want := frameHex(t, result.Frames[1]), "0000000047fdffff0000000000"; got != want {
		t.Fatalf("death 30D2 = %s, want %s (-697, no level-up tail)", got, want)
	}
	if *character.Level != 10 || *character.Experience != 23500-697 {
		t.Fatalf("post-death level/exp = %d/%d, want 10/%d", *character.Level, *character.Experience, 23500-697)
	}
	// 4E5710 records the loss that resurrection later returns a share of.
	if character.LastExpLoss != 697 {
		t.Fatalf("recorded loss = %d, want 697", character.LastExpLoss)
	}
	if *character.MaxLevel != 11 || *character.StatPoints != 7 ||
		*character.Strength != 30 || *character.Intellect != 28 {
		t.Fatalf("death changed earned watermark/stats: max=%d points=%d STR/INT=%d/%d",
			*character.MaxLevel, *character.StatPoints, *character.Strength, *character.Intellect)
	}
}

/*
================
TestOrdinaryDeathPenaltyUsesLeveldataCeiling

At high levels the authored ceiling binds before the two-percent result.
================
*/
func TestOrdinaryDeathPenaltyUsesLeveldataCeiling(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(90)
	character.MaxLevel = int64Ptr(90)
	character.Experience = int64Ptr(1_000_000)
	rt := newTestRuntime(character)

	result := rt.ApplyDeathPenalty(character, OrdinaryDeathPenalty())
	// 2% is 5,633,447, so leveldata basis 6949 * 100 caps it at 694,900.
	if len(result.Frames) != 1 || *character.Experience != 305_100 {
		t.Fatalf("capped death loss = frames %d exp %d, want one frame / 305100",
			len(result.Frames), *character.Experience)
	}
	if got := int32(binary.LittleEndian.Uint32(result.Frames[0].Payload[4:8])); got != -694900 {
		t.Fatalf("emitted capped loss = %d, want -694900", got)
	}
}

/*
================
TestRelevelBelowMaxWatermarkDoesNotDuplicateEarnedStats

Recovering a lost level restores gauges without awarding its stats twice.
================
*/
func TestRelevelBelowMaxWatermarkDoesNotDuplicateEarnedStats(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(10)
	character.MaxLevel = int64Ptr(11)
	character.Experience = int64Ptr(23_400)
	character.StatPoints = int64Ptr(7)
	character.Strength = int64Ptr(30)
	character.Intellect = int64Ptr(28)
	rt := newTestRuntime(character)

	result := rt.GrantExperience(character, 100, 0, 0)
	if len(result.Frames) != 4 {
		t.Fatalf("relevel frames = %d, want 36B0/343C/33A6/30D2", len(result.Frames))
	}
	if *character.Level != 11 || *character.MaxLevel != 11 || *character.Experience != 0 {
		t.Fatalf("relevel state = level/max/exp %d/%d/%d", *character.Level, *character.MaxLevel, *character.Experience)
	}
	if *character.StatPoints != 7 || *character.Strength != 30 || *character.Intellect != 28 {
		t.Fatalf("relevel duplicated earned stats: points=%d STR/INT=%d/%d",
			*character.StatPoints, *character.Strength, *character.Intellect)
	}
	if tail := binary.LittleEndian.Uint16(result.Frames[3].Payload[13:15]); tail != 7 {
		t.Fatalf("relevel absolute stat-point tail = %d, want unchanged 7", tail)
	}
}

/*
================
TestGrantExperienceRunsInsideOneDoorClosure

The curve walk, recovery, and frame snapshots complete under one transaction.
================
*/
func TestGrantExperienceRunsInsideOneDoorClosure(t *testing.T) {
	character := levelupTestCharacter()
	character.Experience = int64Ptr(50)
	commits := 0
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      emptyItemRefs{},
		Levels:     testLevels(),
	}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		if label != "grant-exp" {
			t.Errorf("door label = %q, want grant-exp", label)
		}
		fn()
		commits++
		// The frames must be encoded by the time the door closes: the
		// record's post-commit fields already carry the new level.
		if c.Level == nil || *c.Level != 3 {
			t.Errorf("closure closed before the level write: %v", c.Level)
		}
	}
	rt := NewRuntime(deps)

	result := rt.GrantExperience(character, 550, 0, 0)
	if commits != 1 {
		t.Fatalf("door commits = %d, want exactly 1", commits)
	}
	if len(result.Frames) != 4 {
		t.Fatalf("frames = %d, want 4", len(result.Frames))
	}
}

/*
================
TestGrantExperienceMaxLevelWatermarkNeverLowers

An imported historical maximum survives crossings below that watermark.
================
*/
func TestGrantExperienceMaxLevelWatermarkNeverLowers(t *testing.T) {
	character := levelupTestCharacter()
	character.Experience = int64Ptr(50)
	character.MaxLevel = int64Ptr(5)
	rt := newTestRuntime(character)

	rt.GrantExperience(character, 550, 0, 0)

	if *character.Level != 3 {
		t.Fatalf("level = %d, want 3", *character.Level)
	}
	if *character.MaxLevel != 5 {
		t.Errorf("MaxLevel = %d, want kept 5", *character.MaxLevel)
	}
}

/*
================
TestGrantExperienceRecoversPresentCurrents

Existing depleted gauges recover just like absent full-at-maximum gauges.
================
*/
func TestGrantExperienceRecoversPresentCurrents(t *testing.T) {
	character := levelupTestCharacter()
	character.Experience = int64Ptr(50)
	character.CurrentHP = int64Ptr(120)
	character.CurrentMP = int64Ptr(80)
	rt := newTestRuntime(character)

	rt.GrantExperience(character, 550, 0, 0)

	if *character.CurrentHP != 228 || *character.CurrentMP != 228 {
		t.Errorf("currents after level-up: %d/%d, want 228/228", *character.CurrentHP, *character.CurrentMP)
	}
}

/*
================
TestHandleDevGrantExpDecodesAndRefusesSilently

The diagnostic requires a complete payload and a privileged bound character.
================
*/
func TestHandleDevGrantExpDecodesAndRefusesSilently(t *testing.T) {
	for _, payload := range [][]byte{nil, {}, {1, 2, 3}, make([]byte, 7), make([]byte, 9), make([]byte, 12)} {
		character := levelupTestCharacter()
		rt := newTestRuntime(character)
		if result := rt.HandleDevGrantExp(testDivision, character, payload); len(result.Frames) != 0 {
			t.Errorf("payload of %d bytes emitted frames", len(payload))
		}
		if character.Experience != nil {
			t.Errorf("payload of %d bytes moved exp", len(payload))
		}
	}

	character := levelupTestCharacter()
	character.GMPrivilege = true
	rt := newTestRuntime(character)
	// [exp=50][skillExp=0], little-endian.
	result := rt.HandleDevGrantExp(testDivision, character, []byte{50, 0, 0, 0, 0, 0, 0, 0})
	if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpExpUpdate {
		t.Fatalf("valid dev grant: frames = %+v, want one 0x30D2", result.Frames)
	}
	if character.Experience == nil || *character.Experience != 50 {
		t.Errorf("valid dev grant did not apply: %v", character.Experience)
	}

	ordinary := levelupTestCharacter()
	rt = newTestRuntime(ordinary)
	if result := rt.HandleDevGrantExp(testDivision, ordinary, []byte{50, 0, 0, 0, 0, 0, 0, 0}); len(result.Frames) != 0 {
		t.Fatalf("non-GM dev grant emitted frames: %+v", result.Frames)
	}
	if ordinary.Experience != nil {
		t.Fatalf("non-GM dev grant changed experience: %v", *ordinary.Experience)
	}
}

/*
================
TestDevExpGrantEnabledReadsTheEnvGate

Only the explicit startup value "1" exposes the diagnostic opcode.
================
*/
func TestDevExpGrantEnabledReadsTheEnvGate(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "true": false, "1": true} {
		t.Setenv(EnvDevExpGrant, value)
		if got := DevExpGrantEnabled(); got != want {
			t.Errorf("%s=%q -> %v, want %v", EnvDevExpGrant, value, got, want)
		}
	}
}

/*
================
TestEncodeExpUpdateShapes

Only a level crossing carries the absolute stat-point tail.
================
*/
func TestEncodeExpUpdateShapes(t *testing.T) {
	plain := wire.EncodeExpUpdate(7, 100, 25, false, 0xdead)
	if len(plain) != 13 {
		t.Fatalf("non-levelled length = %d, want 13", len(plain))
	}
	if !bytes.Equal(plain[0:4], []byte{7, 0, 0, 0}) {
		t.Errorf("sourceGid bytes = %x", plain[0:4])
	}
	levelled := wire.EncodeExpUpdate(0, 100, 25, true, 0x0102)
	if len(levelled) != 15 {
		t.Fatalf("levelled length = %d, want 15", len(levelled))
	}
	if levelled[13] != 0x02 || levelled[14] != 0x01 {
		t.Errorf("statPoints tail = %x %x, want little-endian 0x0102", levelled[13], levelled[14])
	}
	if levelled[12] != 0 {
		t.Errorf("flags byte = %d, want always 0", levelled[12])
	}
}
