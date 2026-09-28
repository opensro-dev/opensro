/*
===========================================================================

world_identity_test.go - party markers share enter-world instance identity

The map rejects a roster row from a different packed world. Test against the
entry projection so a default value cannot drift between those two lanes.

===========================================================================
*/
package party

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestPartyMemberWorldMatchesEntry
================
*/
func TestPartyMemberWorldMatchesEntry(t *testing.T) {
	for _, packed := range []uint32{domain.DefaultWorldInstance, 0, 0x20001} {
		character := &enterworld.Character{Name: "MapProbe"}
		if packed != domain.DefaultWorldInstance {
			character.World = &domain.CharacterWorld{PackedInstance: &packed}
		}
		runtime := NewRuntime(vitalsDeps{}, nil)
		row := runtime.memberRowFor(testDivision, character)
		entry := enterworld.ResolveLocalPlayerEntry(character, nil)
		if entry.FortressWorld == nil || row.War != *entry.FortressWorld {
			t.Fatalf("party world %x differs from entry %v", row.War, entry.FortressWorld)
		}
		if row.War != packed {
			t.Fatalf("party world %x, want %x", row.War, packed)
		}
	}
}
