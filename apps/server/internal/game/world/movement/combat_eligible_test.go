package movement

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/staff"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport/worldsession"
)

// TestWorldSnapshotCombatEligibilityLifecycle verifies that the production
// WorldSnapshot implementation transitions CombatEligible across alive, dead,
// revived, and delete-pending states while strictly maintaining the character's
// presence as a world viewer (World state, RegionID, and Appearance intact).
func TestWorldSnapshotCombatEligibilityLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)

	modelRef := int64(1907)
	bodyShape := int64(1)
	seed := &enterworld.Character{
		Name:             "snaphero",
		ModelCodename:    "CHAR_EU_MAN",
		ModelRef:         &modelRef,
		BodyShapeByte:    &bodyShape,
		MissionInventory: raceEquipSet(0),
	}
	if err := authority.CreateCharacter(doorTestDivision, "test-account", seed); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	character := authority.Characters().CharactersForDivision(doorTestDivision)[0]

	deps := &enterworld.Deps{Characters: authority.Characters()}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.ReadCharacter = func(divisionID string, fn func()) {
		authority.ReadCharacters(divisionID, func([]*enterworld.Character) { fn() })
	}
	deps.Guilds = authority.Guilds()
	deps.UpdateCharacter = authority.UpdateCharacter

	rt := NewRuntime(deps, simulation.NewWorldStore())
	recorded := &recordedWorld{}
	rt.WorldBound(recorded, doorTestDivision, character)
	provider, ok := recorded.snapshot.(worldsession.SnapshotProvider)
	if !ok {
		t.Fatalf("recorded snapshot does not implement SnapshotProvider: %T", recorded.snapshot)
	}

	// 1. Initial State: alive, not deleting -> CombatEligible MUST be true
	snap := provider.WorldSnapshot()
	if !snap.CombatEligible {
		t.Fatalf("expected alive character to be CombatEligible: true, got false")
	}
	if snap.World.Spawn.RegionID == 0 {
		t.Fatalf("expected non-zero region for live character")
	}
	initialRegion := snap.World.Spawn.RegionID
	deps.Mutate(character, "grant-test-gm", func() { character.GMPrivilege, character.GMCapabilities = true, uint32(staff.All) })
	statusOwner := action.NewRuntime(deps, nil)
	set := func() {
		outcome := gmcommand.HandleGmCommand(deps, nil, doorTestDivision, character, []byte{gmcommand.SubInvisible}, statusOwner)
		if len(outcome.Ack) != 2 || outcome.Ack[0] != 1 {
			t.Fatalf("GM status writer refused: %+v", outcome)
		}
	}
	set()
	statusSnapshot := provider.WorldSnapshot()
	if !statusSnapshot.CombatEligible || statusSnapshot.NativeBodyStatus != 4 {
		t.Fatal("observer-dependent status was lost or globally excluded the viewer")
	}
	set()
	if statusSnapshot.NativeBodyStatus != 4 || provider.WorldSnapshot().NativeBodyStatus != 0 {
		t.Fatal("body state snapshot aliases mutable character state")
	}

	// 2. Character Dies: HP set to 0 -> CombatEligible MUST be false, but viewer state remains
	zeroHP := int64(0)
	deps.Mutate(character, "death", func() {
		character.CurrentHP = &zeroHP
	})
	snapDead := provider.WorldSnapshot()
	if snapDead.CombatEligible {
		t.Fatalf("expected dead character to have CombatEligible: false, got true")
	}
	if snapDead.World.Spawn.RegionID != initialRegion {
		t.Fatalf("dead character region drifted: got %d, want %d", snapDead.World.Spawn.RegionID, initialRegion)
	}
	if snapDead.Appearance == nil || snapDead.Appearance.Name != "snaphero" {
		t.Fatalf("dead character must retain viewer appearance, got %+v", snapDead.Appearance)
	}

	// 3. Character Revives: HP restored -> CombatEligible MUST transition back to true
	fullHP := int64(1500)
	deps.Mutate(character, "revive", func() {
		character.CurrentHP = &fullHP
	})
	snapRevived := provider.WorldSnapshot()
	if !snapRevived.CombatEligible {
		t.Fatalf("expected revived character to have CombatEligible: true, got false")
	}

	// 4. Character Pending Deletion: DeletePending = true -> CombatEligible MUST be false
	deps.Mutate(character, "delete-pending", func() {
		character.DeletePending = true
	})
	snapDeleted := provider.WorldSnapshot()
	if snapDeleted.CombatEligible {
		t.Fatalf("expected delete-pending character to have CombatEligible: false, got true")
	}
	if snapDeleted.World.Spawn.RegionID != initialRegion {
		t.Fatalf("delete-pending character must retain region until disconnect: got %d, want %d", snapDeleted.World.Spawn.RegionID, initialRegion)
	}
}
