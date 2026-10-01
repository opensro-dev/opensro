/*
===========================================================================

skillcastfixture_test.go - shared native cast packet capture generation and validation

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// ---- The pinned cross-language skill-cast accept fixture ----
//
// Two-sided pin, the monster_spawn_fixture pattern: this Go test regenerates
// testdata/skill_cast_accept_fixture.json from the REAL accept path
// (HandleTargetInteract -> acceptSkillCast -> TickHook) and
// requires the checked-in file to match byte for byte; the client parity
// harness reads the SAME file and drives the immediate 0xB245 and deferred 0xB505
// payloads through the
// client's REAL sub_776830 -> 8e06e0 -> 857830 state-2 enter and
// sub_7754f0 -> 8e2c90 -> 8dcf40 finalize chain. Either side drifting fails
// its own gate; neither re-implements the other.
//
// Regenerate:
//
//	UPDATE_SKILL_CAST_FIXTURE=1 go test ./internal/game/action -run TestSkillCastAcceptFixturePinned
/*
================
skillCastFixture
================
*/
type skillCastFixture struct {
	Comment   []string                   `json:"comment"`
	Expect    skillCastFixtureExpect     `json:"expect"`
	Scenarios []skillCastFixtureScenario `json:"scenarios"`
}

/*
================
skillCastFixtureExpect
================
*/
type skillCastFixtureExpect struct {
	CasterGid        uint32 `json:"casterGid"`
	BtResult         uint8  `json:"btResult"`
	OwnerOrTargetGid uint32 `json:"ownerOrTargetGid"`
	SteeringFlags    uint8  `json:"steeringFlags"`
	FacingRegionID   uint16 `json:"facingRegionId"`
	FacingX          int16  `json:"facingX"`
	FacingY          int16  `json:"facingY"`
	FacingZ          int16  `json:"facingZ"`
	TargetGid        uint32 `json:"targetGid"`
	ResultFlags      uint8  `json:"resultFlags"`
	ImpactCount      uint8  `json:"impactCount"`
	Damage           uint32 `json:"damage"`
	Fatal            bool   `json:"fatal"`
	FinalizeAfterMs  int64  `json:"finalizeAfterMs"`
}

/*
================
skillCastFixtureScenario
================
*/
type skillCastFixtureScenario struct {
	Name              string                   `json:"name"`
	SkillID           uint32                   `json:"skillId"`
	InstanceToken     uint32                   `json:"instanceToken"`
	Request           skillCastFixturePacket   `json:"request"`
	Frames            []skillCastFixturePacket `json:"frames"`
	Broadcast         []skillCastFixturePacket `json:"broadcast"`
	FinalizeBroadcast []skillCastFixturePacket `json:"finalizeBroadcast"`
}

/*
================
skillCastFixturePacket
================
*/
type skillCastFixturePacket struct {
	Opcode     uint16 `json:"opcode"`
	PayloadHex string `json:"payloadHex"`
}

/*
================
skillCastPacketsOf
================
*/
func skillCastPacketsOf(frames []wire.Frame) []skillCastFixturePacket {
	out := make([]skillCastFixturePacket, 0, len(frames))
	for _, frame := range frames {
		out = append(out, skillCastFixturePacket{
			Opcode:     frame.Opcode,
			PayloadHex: hex.EncodeToString(frame.Payload),
		})
	}
	return out
}

/*
================
skillCastMissionPacketsOf
================
*/
func skillCastMissionPacketsOf(frames []simulation.Frame) []skillCastFixturePacket {
	out := make([]skillCastFixturePacket, 0, len(frames))
	for _, frame := range frames {
		out = append(out, skillCastFixturePacket{
			Opcode:     frame.Opcode,
			PayloadHex: hex.EncodeToString(frame.Payload),
		})
	}
	return out
}

// buildSkillCastFixture drives the REAL authoritative base-attack path. The
// former visual-only no-target/ground scenarios are deliberately absent:
// unsupported casts now fail closed before B245.
/*
================
buildSkillCastFixture
================
*/
func buildSkillCastFixture(t *testing.T) skillCastFixture {
	t.Helper()
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	casterGid := enterworld.ObjectIDForCharacter(character)
	request := wire.SkillAction{
		ActionId:  2,
		HasTarget: true,
		TargetGid: target.Gid,
	}.Encode()
	result := rt.HandleTargetInteract(testDivision, character, request)
	result = assertAndSeparateActionSession(t, result)
	token, damage, fatal := assertSkillDamageOpen(
		t,
		result.Frames,
		2,
		casterGid,
		target.Gid,
	)
	assertOnlySkillReleases(t, rt.TickHook()(clock.NowMs()))
	routed := rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli())
	routed = assertAndSeparateActionReleases(t, routed)
	if len(routed) != 1 || routed[0].DivisionID != testDivision {
		t.Fatal("skill-cast fixture: due finalize did not produce one division route")
	}
	scenarios := []skillCastFixtureScenario{{
		Name:          "authoritative-sword-base-attack",
		SkillID:       2,
		InstanceToken: token,
		Request: skillCastFixturePacket{
			Opcode:     wire.OpTargetInteract,
			PayloadHex: hex.EncodeToString(request),
		},
		Frames:            skillCastPacketsOf(result.Frames),
		Broadcast:         skillCastPacketsOf(result.Broadcast),
		FinalizeBroadcast: skillCastMissionPacketsOf(routed[0].Frames),
	}}

	return skillCastFixture{
		Comment: []string{
			"GENERATED + PINNED by internal/game/action/skillcastfixture_test.go (TestSkillCastAcceptFixturePinned).",
			"Authoritative v1.150 base attack evaluated by the pinned v1.188 CFormulae normal lane.",
			"The real HandleTargetInteract path snapshots the player, derives equipped-item stats, resolves",
			"the live monster, commits HP through MonsterState.ApplyDamage, and emits the committed result.",
			"Immediate 0xB245 is [success prefix][steeringFlags=01][impact rows][target columns]; movement is not a facing hint.",
			"For this single-target sword action: [impactCount=2][targetCount=1][target gid once][two ordered results].",
			"then a simulation-tick 0xB505",
			"mode-2 finalize [02][00][token] (6B) after the skill row's authored Action_CastingTime + Action_ActionDuration lifecycle, broadcast to the whole division.",
			"No-target, ground-target, unknown, unlearned, out-of-range, and incomplete-stat casts fail",
			"closed before a token is minted; there is no visual-only success fallback.",
			"The sword-base fixture uses shipped v1.150 phases 0+1200ms; split-phase rows preserve both cells;",
			"animation clips and network latency do not define the authoritative action window.",
			"The gid slot is a",
			"u32 GID (prereq Q3: integer mov to +0xcc, consumed by FindCharacterByGid), sent as 0, NEVER",
			"a float duration. Consumed by the harness skillCastServerAcceptParity test through the REAL",
			"776830/8e06e0/857830 enter + 7754f0/8e2c90/8dcf40 finalize chain.",
			"Regenerate: UPDATE_SKILL_CAST_FIXTURE=1 go test ./internal/game/action -run TestSkillCastAcceptFixturePinned",
		},
		Expect: skillCastFixtureExpect{
			CasterGid:        casterGid,
			BtResult:         0,
			OwnerOrTargetGid: target.Gid,
			SteeringFlags:    1,
			FacingRegionID:   target.Spawn.RegionID,
			FacingX:          int16(target.Spawn.X),
			FacingY:          int16(target.Spawn.Y),
			FacingZ:          int16(target.Spawn.Z),
			TargetGid:        target.Gid,
			ResultFlags:      1,
			ImpactCount:      2,
			Damage:           damage,
			Fatal:            fatal,
			FinalizeAfterMs:  testBasicAttackActionDuration.Milliseconds(),
		},
		Scenarios: scenarios,
	}
}

// TestSkillCastAcceptFixturePinned regenerates the fixture through the REAL
// accept path and requires the checked-in file to match byte for byte.
/*
================
TestSkillCastAcceptFixturePinned
================
*/
func TestSkillCastAcceptFixturePinned(t *testing.T) {
	path := filepath.Join("testdata", "skill_cast_accept_fixture.json")
	fresh := buildSkillCastFixture(t)

	if os.Getenv("UPDATE_SKILL_CAST_FIXTURE") == "1" {
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
		t.Fatalf("fixture missing (%v) - generate with UPDATE_SKILL_CAST_FIXTURE=1", err)
	}
	var pinned skillCastFixture
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	if pinned.Expect != fresh.Expect {
		t.Errorf("expect block drifted:\n got %+v\nwant %+v", pinned.Expect, fresh.Expect)
	}
	if len(pinned.Scenarios) != len(fresh.Scenarios) {
		t.Fatalf("fixture has %d scenarios, the accept path builds %d - regenerate", len(pinned.Scenarios), len(fresh.Scenarios))
	}
	for i, scenario := range fresh.Scenarios {
		got := pinned.Scenarios[i]
		if got.Name != scenario.Name || got.SkillID != scenario.SkillID || got.InstanceToken != scenario.InstanceToken {
			t.Fatalf("scenario %q header drifted (got %q skill 0x%X token %d)", scenario.Name, got.Name, got.SkillID, got.InstanceToken)
		}
		if got.Request != scenario.Request {
			t.Errorf("scenario %q request drifted:\n got 0x%04X %s\nwant 0x%04X %s",
				scenario.Name, got.Request.Opcode, got.Request.PayloadHex, scenario.Request.Opcode, scenario.Request.PayloadHex)
		}
		if got.FinalizeBroadcast == nil {
			t.Fatalf("scenario %q has no deferred finalize broadcast", scenario.Name)
		}
		for _, side := range []struct {
			label        string
			gotPackets   []skillCastFixturePacket
			freshPackets []skillCastFixturePacket
		}{
			{"frames", got.Frames, scenario.Frames},
			{"broadcast", got.Broadcast, scenario.Broadcast},
			{"finalizeBroadcast", got.FinalizeBroadcast, scenario.FinalizeBroadcast},
		} {
			if len(side.gotPackets) != len(side.freshPackets) {
				t.Fatalf("scenario %q %s count drifted: %d vs %d", scenario.Name, side.label, len(side.gotPackets), len(side.freshPackets))
			}
			for j, packet := range side.freshPackets {
				if side.gotPackets[j] != packet {
					t.Errorf("scenario %q %s[%d] drifted:\n got 0x%04X %s\nwant 0x%04X %s",
						scenario.Name, side.label, j,
						side.gotPackets[j].Opcode, side.gotPackets[j].PayloadHex,
						packet.Opcode, packet.PayloadHex)
				}
			}
		}
	}
}
