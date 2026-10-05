/*
===========================================================================

combatfixture_test.go - shared deterministic runtime and cast-bracket fixtures

Own the combat data sources and runtime factory used by admission, attack,
area, and lifecycle tests. Behavioral cases stay beside their respective lane.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"math"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

/*
================
assertSkillCastClose
================
*/
func assertSkillCastClose(t *testing.T, routed []simulation.DivisionFrames, divisionID string, token uint32) {
	t.Helper()
	routed = assertAndSeparateActionReleases(t, routed)
	if len(routed) != 1 {
		t.Fatalf("due tick routed %d division bursts, want 1", len(routed))
	}
	if routed[0].DivisionID != divisionID || routed[0].ExceptSessionID != "" {
		t.Fatalf("due route = division %q except %q, want division %q including caster",
			routed[0].DivisionID, routed[0].ExceptSessionID, divisionID)
	}
	var matches []simulation.Frame
	for _, frame := range routed[0].Frames {
		if frame.Opcode == opSkillEffectCtrlB505 && len(frame.Payload) == skillCastFinalizeLen &&
			binary.LittleEndian.Uint32(frame.Payload[2:]) == token {
			matches = append(matches, frame)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("due route has %d matching 0xB505 frame(s), want exactly one; route=%+v", len(matches), routed[0].Frames)
	}
	finalize := matches[0]
	if finalize.Opcode != opSkillEffectCtrlB505 {
		t.Fatalf("due opcode = 0x%04X, want 0xB505", finalize.Opcode)
	}
	if len(finalize.Payload) != skillCastFinalizeLen {
		t.Fatalf("finalize payload = % X (%d bytes), want exactly %d", finalize.Payload, len(finalize.Payload), skillCastFinalizeLen)
	}
	if finalize.Payload[0] != 0x02 {
		t.Fatalf("finalize mode = 0x%02X, want mode 2 (CIDecoSkill_RequestCancellation at 0x8DCF40)", finalize.Payload[0])
	}
	if got := binary.LittleEndian.Uint32(finalize.Payload[2:]); got != token {
		t.Fatalf("finalize token = 0x%X, want the success frame's 0x%X (a miss is a SILENT skip - the bracket never closes)", got, token)
	}
}

/*
================
staticSkillSource
================
*/
type staticSkillSource map[uint32]enterworld.SkillRow

// The synthetic sword row below mirrors SKILL_CH_SWORD_BASE_01 col 13.
// Production never reads this constant; it resolves Action_ActionDuration
// from the shipped skilldata row.
const testBasicAttackActionDuration = 1200 * time.Millisecond

/*
================
SkillByID
================
*/
func (source staticSkillSource) SkillByID(id uint32) (enterworld.SkillRow, bool) {
	row, ok := source[id]
	return row, ok
}

/*
================
testInt64
================
*/
func testInt64(value int64) *int64 { return &value }

/*
================
newCombatTestRuntime
================
*/
func newCombatTestRuntime(
	t *testing.T,
	monsterHP uint32,
) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	return newCombatTestRuntimeAtLevel(t, monsterHP, 1)
}

/*
================
newCombatTestRuntimeAtLevel
================
*/
func newCombatTestRuntimeAtLevel(t *testing.T, monsterHP uint32, level uint8) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	const region = uint16(0x62A8)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}

	ref := monster.MonsterRef{
		RefObjID:   1933,
		Codename:   "MOB_CH_MANGNYANG",
		Level:      level,
		MaxHP:      monsterHP,
		BodyRadius: 6,
		ExpToGive:  24,

		RewardActionPinned: true,

		CombatPinned:    true,
		PhysicalDefense: 7,
		MagicalDefense:  10,
		ParryRate:       1,
		MagicalParry:    1,
		EvasionRate:     27,
		HitRate:         27,
		CriticalRate:    2,
	}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{
			SpawnPoint: monster.SpawnPoint{
				RefObjID: ref.RefObjID,
				RegionID: region,
				X:        963,
				Y:        20,
				Z:        458,
			},
			RetailEvidence: true,
			MaxCount:       1,
		}},
	))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	monsters.AdvancePopulation(monsters.CurrentTimeMillis())
	instances := monsters.InstancesInRegions(testDivision, []uint16{region})
	if len(instances) != 1 {
		t.Fatalf("combat fixture materialized %d monsters, want one", len(instances))
	}

	sword := &enterworld.ItemRef{
		RefObjID: 71,
		Codename: "ITEM_CH_SWORD_01_A",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			ActionRange: 6,
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4000001},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4000001},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.0999999},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.0999999},
			},
			HitRate:      enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate: enterworld.ItemStatRange{Min: 3, Max: 15},
		},
	}
	character := &enterworld.Character{
		ID:            3,
		Name:          "asd2",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     testInt64(enterworld.RaceChina),
		Level:         testInt64(1),
		MaxLevel:      testInt64(1),
		CurrentHP:     testInt64(100),
		Strength:      testInt64(20),
		Intellect:     testInt64(20),
		Masteries:     []enterworld.CharacterMastery{{ID: 257, Level: 1}},
		Skills:        []uint32{2},
		MissionInventory: []enterworld.InventoryRow{{
			Slot:         6,
			RefObjID:     sword.RefObjID,
			Codename:     sword.Codename,
			TypeFlags:    sword.TypeFlags(),
			VarianceBits: "0",
			Durability:   76,
			StackCount:   1,
		}},
		World: &enterworld.CharacterWorld{
			Spawn: &enterworld.WorldSpawn{
				RegionID: testInt64(int64(region)),
				X:        func() *float64 { value := 960.0; return &value }(),
				Y:        func() *float64 { value := 20.0; return &value }(),
				Z:        func() *float64 { value := 458.0; return &value }(),
				// The fixture starts facing its target. Individual transition tests
				// deliberately override this to prove B2F5 ordering and commitment.
				Angle: testInt64(0),
			},
			SpawnSet: true,
		},
	}
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Roster: &enterworld.Roster{
			Format: "sro-server-character-authority", Version: 3,
			Models: []enterworld.RosterModel{{
				Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
			}, {
				Codename: "CHAR_EU_MAN_NOBLE", RefObjID: 14717, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
			}},
		},
		Items:  staticItemSource{sword.Codename: sword},
		Levels: testCombatRewardLevels(),
		Skills: staticSkillSource{2: {
			ID:                      2,
			Codename:                "SKILL_CH_SWORD_BASE_01",
			TargetRequired:          true,
			ActionCastingTimeMs:     0,
			ActionCastingTimePinned: true,
			ActionDurationMs:        uint32(testBasicAttackActionDuration.Milliseconds()),
			ActionDurationPinned:    true,
			CoolTimeMs:              1000,
			TimingPinned:            true,
			// The weight a shipped basic attack carries (column 66).
			AIWeight:            100,
			ActionRange:         6,
			ActionRangePinned:   true,
			RequiredWeaponKinds: [2]uint8{2, 3},
			Attack: enterworld.SkillAttack{
				Present:     true,
				Flags:       5,
				Percent:     60,
				Value5:      60,
				ImpactCount: 2,
			},
			CombatPinned: true,
		}},
	}
	rt := NewRuntime(deps, monsters)
	rt.BerserkRoll = func() (uint32, error) { return 9999, nil }
	// Combat-order fixtures must not acquire extra frames from random loot.
	// Loot cases supply their own draws through this same injection boundary.
	rt.DropRoll = nil
	rt.Now = clock.Now
	// Keep ordinary-action fixtures on the noncritical branch; dedicated
	// critical tests exercise the inclusive zero/boundary outcomes.
	rt.CombatRoll = func() (uint32, error) { return 100, nil }
	// Equipment wears only where a test asks it to (equipmentwear_test.go).
	rt.WearRoll = func() (uint32, error) { return 99, nil }
	// Fixtures fight already in battle, so a strike changes no state+0xD;
	// battle entry and exit are pinned by the battle-state tests.
	character.BattleUntilMs = math.MaxInt64
	// The production store forbids a read inside a character write; every
	// combat fixture runs behind the same door so a nested read fails here.
	installStoreDoor(t, rt)
	return rt, clock, character, instances[0]
}
