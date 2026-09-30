/*
===========================================================================

actionresultfixture_test.go - shared committed hit and HP fixture

Generate wire packets from the simulation owner so the browser tests consume
the full hit while independently checking the clamped HP outcome.

===========================================================================
*/

package wire_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// This fixture joins two real owners rather than reimplementing either side:
// simulation.MonsterState commits CurrentHP, then wire serializes the committed
// Damage/Fatal outcome. The browser harness reads the same bytes through the
// real B245 -> 8e0440 -> 8e0190 -> 8e1710 -> 8db770 -> 8d5440 chain.
//
// Regenerate:
//
//	UPDATE_SKILL_ACTION_RESULT_FIXTURE=1 go test ./internal/game/item/wire -run TestSkillActionResultFixturePinned
/*
================
actionResultFixture
================
*/
type actionResultFixture struct {
	Comment   []string                      `json:"comment"`
	Expect    actionResultFixtureExpect     `json:"expect"`
	Scenarios []actionResultFixtureScenario `json:"scenarios"`
}

/*
================
actionResultFixtureExpect
================
*/
type actionResultFixtureExpect struct {
	SkillID            uint32 `json:"skillId"`
	CasterGid          uint32 `json:"casterGid"`
	TargetGid          uint32 `json:"targetGid"`
	InitialHP          uint32 `json:"initialHp"`
	MaxWireDamage      uint32 `json:"maxWireDamage"`
	OwnerOrTargetGid   uint32 `json:"ownerOrTargetGid"`
	FacingRegionID     uint16 `json:"facingRegionId"`
	FacingX            int16  `json:"facingX"`
	FacingY            int16  `json:"facingY"`
	FacingZ            int16  `json:"facingZ"`
	ImpactCount        uint8  `json:"impactCount"`
	TargetCount        uint8  `json:"targetCount"`
	ResultKind         uint8  `json:"resultKind"`
	SecondaryAmount    uint32 `json:"secondaryAmount"`
	NonFatalResultFlag uint8  `json:"nonFatalResultFlags"`
}

/*
================
actionResultFixtureScenario
================
*/
type actionResultFixtureScenario struct {
	Name            string `json:"name"`
	RequestedDamage uint32 `json:"requestedDamage"`
	BeforeHP        uint32 `json:"beforeHp"`
	AppliedDamage   uint32 `json:"appliedDamage"`
	CurrentHP       uint32 `json:"currentHp"`
	Fatal           bool   `json:"fatal"`
	InstanceToken   uint32 `json:"instanceToken"`
	Opcode          uint16 `json:"opcode"`
	PayloadHex      string `json:"payloadHex"`
}

/*
================
buildActionResultFixture
================
*/
func buildActionResultFixture(t *testing.T) actionResultFixture {
	t.Helper()

	const (
		divisionID = "action-result-fixture"
		skillID    = uint32(1)
		casterGid  = uint32(100003)
		regionID   = uint16(0x62AA)
	)
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {
				RefObjID: 1933,
				TidWord:  0x00C6,
				Codename: "MOB_CH_MANGNYANG",
				Level:    1,
				MaxHP:    54,
			},
		},
		[]monster.NestRow{{
			SpawnPoint: monster.SpawnPoint{
				RefObjID: 1933,
				RegionID: regionID,
				X:        812.68,
				Y:        75.08,
				Z:        392.90,
			},
		}},
	)
	registry := simulation.NewMonsterState(template)
	registry.StartDivision(divisionID)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions(divisionID, []uint16{regionID})
	if len(instances) != 1 {
		t.Fatalf("fixture materialized %d monsters, want 1", len(instances))
	}
	target := instances[0]
	facingPoint, facingPointOK := wire.NewSkillCastFacingPoint(
		target.Spawn.RegionID,
		target.Spawn.X,
		target.Spawn.Y,
		target.Spawn.Z,
	)
	if !facingPointOK {
		t.Fatal("fixture target pose is not representable by v1.150 B245 steering")
	}

	requested := []uint32{7, 1000}
	names := []string{"non-fatal-hit", "fatal-overkill"}
	scenarios := make([]actionResultFixtureScenario, 0, len(requested))
	for index, damage := range requested {
		committed, ok := registry.ApplyDamage(divisionID, target.Gid, damage)
		if !ok {
			t.Fatalf("fixture damage %d rejected target %d", damage, target.Gid)
		}
		token := uint32(index + 1)
		frame := wire.SkillCastSingleTargetResultFrame(
			wire.NewSkillCastSingleTargetResult(
				wire.SkillCastSuccess{
					BtResult:      0,
					SkillId:       skillID,
					CasterGid:     casterGid,
					InstanceToken: token,
				},
				target.Gid,
				[]wire.SkillCastTargetImpact{{
					Damage: committed.Damage,
					Fatal:  committed.Fatal,
				}},
				facingPoint,
			),
		)
		scenarios = append(scenarios, actionResultFixtureScenario{
			Name:            names[index],
			RequestedDamage: damage,
			BeforeHP:        committed.BeforeHP,
			AppliedDamage:   committed.Applied,
			CurrentHP:       committed.CurrentHP,
			Fatal:           committed.Fatal,
			InstanceToken:   token,
			Opcode:          frame.Opcode,
			PayloadHex:      hex.EncodeToString(frame.Payload),
		})
	}

	return actionResultFixture{
		Comment: []string{
			"GENERATED + PINNED by internal/game/item/wire/actionresultfixture_test.go.",
			"MonsterState.ApplyDamage owns the sole monster CurrentHP mutation; its committed Damage/Fatal result",
			"is serialized by SkillCastSingleTargetResult as one B245 impact row by one target column.",
			"The same live target pose is appended as the B245 bit-3 steering point, which owns local holder facing.",
			"The browser verifier consumes these exact bytes through the real 776830/8e0440/8e0190/8e1710/8db770/8d5440 chain.",
			"Live equipped-weapon base attacks emit this shape after the pinned v1.188 CFormulae normal lane.",
		},
		Expect: actionResultFixtureExpect{
			SkillID:          skillID,
			CasterGid:        casterGid,
			TargetGid:        target.Gid,
			InitialHP:        target.CurrentHP,
			MaxWireDamage:    wire.MaxSkillActionDamage,
			ImpactCount:      1,
			TargetCount:      1,
			ResultKind:       0,
			SecondaryAmount:  0,
			OwnerOrTargetGid: target.Gid,
			FacingRegionID:   target.Spawn.RegionID,
			FacingX:          int16(target.Spawn.X),
			FacingY:          int16(target.Spawn.Y),
			FacingZ:          int16(target.Spawn.Z),
		},
		Scenarios: scenarios,
	}
}

/*
================
TestSkillActionResultFixturePinned
================
*/
func TestSkillActionResultFixturePinned(t *testing.T) {
	path := filepath.Join("testdata", "skill_action_result_fixture.json")
	fresh := buildActionResultFixture(t)

	if os.Getenv("UPDATE_SKILL_ACTION_RESULT_FIXTURE") == "1" {
		blob, err := json.MarshalIndent(fresh, "", "  ")
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, append(blob, '\n'), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture missing (%v) - regenerate with UPDATE_SKILL_ACTION_RESULT_FIXTURE=1", err)
	}
	var pinned actionResultFixture
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if !reflect.DeepEqual(pinned, fresh) {
		got, _ := json.MarshalIndent(pinned, "", "  ")
		want, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("skill action-result fixture drifted:\n got %s\nwant %s\nregenerate with UPDATE_SKILL_ACTION_RESULT_FIXTURE=1",
			got, want)
	}
}
