/*
===========================================================================

entities_test.go - the agents as world entities (slice 3 snapshot)

===========================================================================
*/
package caravanai

import (
	"context"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestAgentEntitiesCarryIdentityJobAndGear

Every roster agent snapshots with its synthetic character identity
(reserved range, roster order), the wire job byte of its role, and
the shipped appearance codenames the wiring resolves. A real store
rowid can never reach the synthetic range, so the PlayerObjectID
gids stay disjoint from minted characters.
================
*/
func TestAgentEntitiesCarryIdentityJobAndGear(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, WorldHooks{})
	const div = "div"
	entities := rt.AgentEntities(div)
	if len(entities) != 4 {
		t.Fatalf("entities = %d, want the 4-agent roster", len(entities))
	}
	wantJobs := map[string]uint8{
		"[AI]Trader_Jin":  domain.JobTrader,
		"[AI]Trader_Mei":  domain.JobTrader,
		"[AI]Hunter_Kwan": domain.JobHunter,
		"[AI]Thief_Viper": domain.JobThief,
	}
	wantSuitFamily := map[string]string{
		"[AI]Trader_Jin":  "TRADE",
		"[AI]Trader_Mei":  "TRADE",
		"[AI]Hunter_Kwan": "HUNTER",
		"[AI]Thief_Viper": "THIEF",
	}
	for i, entity := range entities {
		if want := AgentCharacterIDBase + int64(i); entity.CharacterID != want {
			t.Errorf("%s CharacterID = %d, want %d (roster slot)", entity.Name, entity.CharacterID, want)
		}
		if entity.CharacterID < AgentCharacterIDBase || entity.CharacterID > 0x7fffffff {
			t.Errorf("%s CharacterID %d escapes the reserved range", entity.Name, entity.CharacterID)
		}
		if got, want := entity.JobType, wantJobs[entity.Name]; got != want {
			t.Errorf("%s JobType = %d, want %d", entity.Name, got, want)
		}
		if entity.ModelCode == "" || entity.SuitCode == "" || entity.WeaponCode == "" {
			t.Errorf("%s appearance codenames = %q/%q/%q, want all set",
				entity.Name, entity.ModelCode, entity.SuitCode, entity.WeaponCode)
		}
		if !strings.Contains(entity.SuitCode, wantSuitFamily[entity.Name]) {
			t.Errorf("%s SuitCode = %q, want the %s family", entity.Name, entity.SuitCode, wantSuitFamily[entity.Name])
		}
	}
}

/*
================
TestAgentEntityMarchTargetAndPose

A travelling agent's entity carries where it stands (its anchor Y,
not a terrain guess) and the anchor it marches to; stepping moves
the pose toward the target.
================
*/
func TestAgentEntityMarchTargetAndPose(t *testing.T) {
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil,
		WorldHooks{Walker: straightWalker{}},
	)
	const div = "div"
	roster := rt.EnsureDivisionRoster(div)
	var jin *AgentUnit
	for _, unit := range roster {
		if unit.Name == "[AI]Trader_Jin" {
			jin = unit
		}
	}
	if jin == nil {
		t.Fatal("roster has no [AI]Trader_Jin")
	}

	before := entityByName(t, rt.AgentEntities(div), jin.Name)
	anchor := jin.Route[jin.WaypointIndex]
	if !before.Moving {
		t.Fatal("a travelling agent's entity is not Moving")
	}
	if before.Target != (Pose{RegionID: anchor.RegionID, X: anchor.X, Y: anchor.Y, Z: anchor.Z}) {
		t.Fatalf("Target = %+v, want the current anchor %+v", before.Target, anchor)
	}
	if before.Pose.Y != jin.Y {
		t.Fatalf("Pose.Y = %v, want the agent's carried Y %v", before.Pose.Y, jin.Y)
	}

	ctx := context.Background()
	for step := int64(1); step <= 30; step++ {
		rt.StepAgent(ctx, div, jin.Name, nil, step*1000)
	}
	after := entityByName(t, rt.AgentEntities(div), jin.Name)
	if after.Pose == before.Pose {
		t.Fatalf("pose did not move after stepping: %+v", after.Pose)
	}
	if after.Pose.RegionID != before.Pose.RegionID {
		t.Fatalf("pose left region %d for %d within the first anchors", before.Pose.RegionID, after.Pose.RegionID)
	}
}

/*
================
entityByName
================
*/
func entityByName(t *testing.T, entities []AgentEntity, name string) AgentEntity {
	t.Helper()
	for _, entity := range entities {
		if entity.Name == name {
			return entity
		}
	}
	t.Fatalf("no entity named %s", name)
	return AgentEntity{}
}
