/*
===========================================================================

companion_persistence_test.go - ground ownership retains detached pet records

===========================================================================
*/
package grounditem

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestGroundCompanionRecordSurvivesRestartWithoutAliasing
================
*/
func TestGroundCompanionRecordSurvivesRestartWithoutAliasing(t *testing.T) {
	registry := NewRegistry()
	pet := &domain.CharacterCOS{RefObjID: 950, Name: "Retained", Experience: 1234, StateFlags: 1,
		Rentals:   []domain.COSRental{{Kind: 5, ID: 17, ExpiresAtUnix: 1234567}},
		Container: &domain.COSContainer{Capacity: 28, Rows: []domain.InventoryRow{{Slot: 0, StackCount: 2}}}}
	added := registry.Add("test", Item{RefObjID: 900, TypeFlags: 0x8cc, StackCount: 1, Summon: pet})
	pet.Name = "Caller mutation"
	added.Summon.Container.Rows[0].StackCount = 99
	encoded, err := json.Marshal(registry.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var saved domain.GroundSnapshot
	if err = json.Unmarshal(encoded, &saved); err != nil {
		t.Fatal(err)
	}
	restored := NewRegistry()
	restored.Restore(saved)
	row, found := restored.Get("test", added.Gid)
	if !found || row.Summon.Name != "Retained" || row.Summon.Experience != 1234 || row.Summon.Container.Rows[0].StackCount != 2 || row.Summon.Rentals[0].ExpiresAtUnix != 1234567 {
		t.Fatal("ground roundtrip lost or aliased companion", row)
	}
	row.Summon.Rentals[0].ExpiresAtUnix = 0
	again, _ := restored.Get("test", added.Gid)
	if again.Summon.Rentals[0].ExpiresAtUnix != 1234567 {
		t.Fatal("read leaked retained rental ownership")
	}
}
