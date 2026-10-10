/*
===========================================================================

session_view_test.go - the hooks' light session view skips the presentation

The tick's full snapshot builds each player's peer presentation (action
speed, spawn skills, companions) for the peer-visibility leg. The hooks
that read only identity and world take WorldView, which must not build it
and must agree with the full snapshot on everything it does carry.

===========================================================================
*/
package movement

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport/worldsession"
)

/*
================
TestWorldViewSkipsThePeerPresentation
================
*/
func TestWorldViewSkipsThePeerPresentation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)
	modelRef, bodyShape := int64(1907), int64(1)
	seed := &enterworld.Character{Name: "viewhero", ModelCodename: "CHAR_EU_MAN", ModelRef: &modelRef,
		BodyShapeByte: &bodyShape, MissionInventory: raceEquipSet(0)}
	if err := authority.CreateCharacter(doorTestDivision, "test-account", seed); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	character := authority.Characters().CharactersForDivision(doorTestDivision)[0]
	deps := &enterworld.Deps{Characters: authority.Characters()}
	deps.ReadCharacter = func(divisionID string, fn func()) {
		authority.ReadCharacters(divisionID, func([]*enterworld.Character) { fn() })
	}
	deps.Guilds = authority.Guilds()
	rt := NewRuntime(deps, simulation.NewWorldStore())
	presentation := 0
	rt.ActionSpeed = func(string, string) float32 { presentation++; return 125 }
	rt.SpawnSkills = func(string, string) []enterworld.EntrySkill { presentation++; return nil }
	rt.CompanionPresentations = func(string, string) []*simulation.PeerCOS { presentation++; return nil }
	recorded := &recordedWorld{}
	rt.WorldBound(recorded, doorTestDivision, character)
	viewer, ok := recorded.snapshot.(worldsession.ViewProvider)
	if !ok {
		t.Fatalf("the session world offers no light view: %T", recorded.snapshot)
	}
	view := viewer.WorldView()
	if presentation != 0 {
		t.Fatalf("WorldView built the peer presentation %d time(s)", presentation)
	}
	full := recorded.snapshot.(worldsession.SnapshotProvider).WorldSnapshot()
	if presentation == 0 || full.Appearance == nil || full.Appearance.ActionSpeed != 125 {
		t.Fatalf("the full snapshot lost its presentation: calls %d, appearance %+v", presentation, full.Appearance)
	}
	want := full.View()
	if view.DivisionID != want.DivisionID || view.CharacterID != want.CharacterID ||
		view.WorldInstance != want.WorldInstance || view.World.Spawn != want.World.Spawn {
		t.Fatalf("view %+v disagrees with the full snapshot %+v", view, want)
	}
}
