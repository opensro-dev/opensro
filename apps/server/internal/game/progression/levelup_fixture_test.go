/*
===========================================================================

levelup_fixture_test.go - the level-up burst fixture

The level-up burst fixture pins the real emitter's bytes.

===========================================================================
*/
package progression

// The FABLE-1 <-> FABLE-5 drift fixture (levelup-wave board seq 33/41/
// 55): testdata/levelup_burst_fixture.json pins the levelling-burst
// BYTES both sides agreed on. This test drives the REAL GrantExperience
// emitter over the REAL shipped leveldata and fails on any drift from
// the fixture; the browser harness has a twin that drives the same
// fixture bytes through the real client folds. Neither side may
// regenerate the fixture to make itself pass - the fixture IS the
// contract.

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

type burstFixture struct {
	PlayerGid         uint32           `json:"playerGid"`
	LeveldataCol1Rows map[string]int64 `json:"leveldataCol1Rows"`
	Scenarios         []burstScenario  `json:"scenarios"`
}

type burstScenario struct {
	Name string `json:"name"`
	Seed struct {
		CharacterID int64 `json:"characterId"`
		Level       int64 `json:"level"`
		Experience  int64 `json:"experience"`
		StatPoints  int64 `json:"statPoints"`
		SkillPoints int64 `json:"skillPoints"`
		SkillExp    int64 `json:"skillExp"`
		Strength    int64 `json:"strength"`
		Intellect   int64 `json:"intellect"`
	} `json:"seed"`
	Grant struct {
		ExpDelta      int64  `json:"expDelta"`
		SkillExpDelta int64  `json:"skillExpDelta"`
		SourceGid     uint32 `json:"sourceGid"`
	} `json:"grant"`
	Frames []struct {
		Opcode     string `json:"opcode"`
		PayloadHex string `json:"payloadHex"`
	} `json:"frames"`
	Persisted map[string]int64 `json:"persisted"`
}

/*
================
TestLevelUpBurstFixturePinsRealEmitterBytes
================
*/
// TestLevelUpBurstFixturePinsRealEmitterBytes runs every fixture
// scenario through the production emitter over the SHIPPED leveldata
// (skipped when the extracted tree is absent, the e2e posture - a
// substitute table would exercise a different server than the one
// deployed) and compares every frame byte and every persisted field.
func TestLevelUpBurstFixturePinsRealEmitterBytes(t *testing.T) {
	textdataDir := licensed.RetailTextdataDir(t)
	if _, err := os.Stat(filepath.Join(textdataDir, "leveldata.txt")); err != nil {
		t.Skipf("shipped textdata not present in this checkout: %v", err)
	}
	levels := enterworld.NewTextdataLevels(textdataDir)

	raw, err := os.ReadFile(filepath.Join("testdata", "levelup_burst_fixture.json"))
	if err != nil {
		t.Fatalf("fixture read: %v", err)
	}
	var fixture burstFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	if len(fixture.Scenarios) == 0 {
		t.Fatal("fixture carries no scenarios")
	}

	// Preflight: the fixture's curve rows must BE the shipped table's
	// (re-asserted through the production loader, so a re-extraction
	// that moves the column fails loudly here, not as a byte mismatch).
	for levelKey, wantExp := range fixture.LeveldataCol1Rows {
		level, err := strconv.ParseInt(levelKey, 10, 64)
		if err != nil {
			t.Fatalf("fixture level key %q: %v", levelKey, err)
		}
		gotExp, ok := levels.ExpRequired(level)
		if !ok || gotExp != wantExp {
			t.Fatalf("shipped leveldata row %d = %d (ok=%v), fixture pins %d", level, gotExp, ok, wantExp)
		}
	}

	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			character := &enterworld.Character{
				ID:            scenario.Seed.CharacterID,
				Name:          "fixtureTester",
				ModelCodename: "CHAR_CH_MAN_ADVENTURER",
				Level:         int64Ptr(scenario.Seed.Level),
				StatPoints:    int64Ptr(scenario.Seed.StatPoints),
				SkillPoints:   int64Ptr(scenario.Seed.SkillPoints),
				Strength:      int64Ptr(scenario.Seed.Strength),
				Intellect:     int64Ptr(scenario.Seed.Intellect),
			}
			if scenario.Seed.Experience != 0 {
				character.Experience = int64Ptr(scenario.Seed.Experience)
			}
			if scenario.Seed.SkillExp != 0 {
				character.SkillExp = int64Ptr(scenario.Seed.SkillExp)
			}
			character.Masteries = enterworld.DefaultMasteries(enterworld.ResolveCharacterRaceKey(character))

			deps := &enterworld.Deps{
				Characters: enterworld.StaticCharacterSource{testDivision: {character}},
				Items:      emptyItemRefs{},
				Levels:     levels,
			}
			deps.MutateCharacter = func(_ *enterworld.Character, _ string, fn func()) { fn() }
			rt := NewRuntime(deps)

			result := rt.GrantExperience(character, scenario.Grant.ExpDelta, scenario.Grant.SkillExpDelta, scenario.Grant.SourceGid)

			if len(result.Frames) != len(scenario.Frames) {
				t.Fatalf("frames = %d, fixture pins %d", len(result.Frames), len(scenario.Frames))
			}
			for i, want := range scenario.Frames {
				parsed, err := strconv.ParseUint(strings.TrimPrefix(want.Opcode, "0x"), 16, 16)
				if err != nil {
					t.Fatalf("fixture opcode %q: %v", want.Opcode, err)
				}
				wantOpcode := uint16(parsed)
				if result.Frames[i].Opcode != wantOpcode {
					t.Errorf("frame[%d] opcode = 0x%04X, fixture pins %s", i, result.Frames[i].Opcode, want.Opcode)
				}
				if got := hex.EncodeToString(result.Frames[i].Payload); got != want.PayloadHex {
					t.Errorf("frame[%d] payload = %s, fixture pins %s", i, got, want.PayloadHex)
				}
			}

			for field, want := range scenario.Persisted {
				var got *int64
				switch field {
				case "level":
					got = character.Level
				case "experience":
					got = character.Experience
				case "statPoints":
					got = character.StatPoints
				case "skillPoints":
					got = character.SkillPoints
				case "skillExp":
					got = character.SkillExp
				case "strength":
					got = character.Strength
				case "intellect":
					got = character.Intellect
				case "maxLevel":
					got = character.MaxLevel
				default:
					t.Fatalf("fixture pins unknown persisted field %q", field)
				}
				if got == nil || *got != want {
					t.Errorf("persisted %s = %v, fixture pins %d", field, got, want)
				}
			}
		})
	}
}
