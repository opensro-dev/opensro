/*
===========================================================================

inventory_expansion_test.go - world entry presents a paid inventory expansion

The v1.150 client learns its capacity only from the entry block. Slots an
expansion quest paid wait on the record until an entry ships them; the
entry that ships them is the one that makes them usable.

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/instance"
)

// inventoryCapacityOffset is the capacity byte of the local-player entry
// (TestBuildLocalPlayerEntryPayloadStructure pins the same offset).
const inventoryCapacityOffset = 54

/*
================
bootstrapSlotCount

The browser bootstrap's inventorySlotCount.
================
*/
func bootstrapSlotCount(t *testing.T, result *BootstrapResult) int {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	var view struct {
		InventorySlotCount int `json:"inventorySlotCount"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatalf("unmarshal bootstrap: %v", err)
	}
	return view.InventorySlotCount
}

/*
================
TestWorldEntryPresentsWaitingInventorySlots
================
*/
func TestWorldEntryPresentsWaitingInventorySlots(t *testing.T) {
	character := chinaSpearman()
	character.InventoryExpansion = 12
	deps := testDeps(character)

	result := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if result.NativeResult != nativeResultSuccess {
		t.Fatalf("entry refused: %+v", result)
	}
	if got := result.Packets[2].Payload[inventoryCapacityOffset]; got != 57 {
		t.Fatalf("entry capacity byte = %d, want 57", got)
	}
	if got := bootstrapSlotCount(t, result); got != 57 {
		t.Fatalf("bootstrap inventorySlotCount = %d, want 57", got)
	}
	if character.InventorySize != 57 || character.InventoryExpansion != 0 {
		t.Fatalf("live record = %d (+%d), want 57 (+0)", character.InventorySize, character.InventoryExpansion)
	}

	// A second entry presents the same capacity and changes nothing.
	again := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if got := again.Packets[2].Payload[inventoryCapacityOffset]; got != 57 || character.InventorySize != 57 {
		t.Fatalf("repeat entry capacity = %d, record %d, want 57", got, character.InventorySize)
	}
}

/*
================
TestRefusedEntryKeepsSlotsWaiting

An entry that is not sent must not make the slots usable: the server would
place items where the client has no slot.
================
*/
func TestRefusedEntryKeepsSlotsWaiting(t *testing.T) {
	character := chinaSpearman()
	character.InventoryExpansion = 10
	deps := testDeps(character)
	// The world lease fails while the entry is being built, after the
	// detached record has presented the slots.
	deps.EntryPopulationLease = func(string, string) (instance.Lease, bool) { return instance.Lease{}, false }

	if refused := Build(deps, BootstrapRequest{CharacterName: character.Name}); refused.Reason != "worldMembershipUnavailable" {
		t.Fatalf("entry was not refused while building: %+v", refused)
	}
	if character.InventoryCapacity() != 45 || character.InventoryExpansion != 10 {
		t.Fatalf("refused entry moved slots: %d (+%d)", character.InventoryCapacity(), character.InventoryExpansion)
	}
}

/*
================
TestPrepareReentryPresentsWithoutAdopting

Resurrection prepares its entry detached and commits later: the packets
carry the new capacity, and the live record waits for that commit.
================
*/
func TestPrepareReentryPresentsWithoutAdopting(t *testing.T) {
	character := chinaSpearman()
	character.MissionInventory = []InventoryRow{}
	character.InventorySize = 55
	character.InventoryExpansion = 2
	before := character.Snapshot()
	deps := testDeps(character)

	prepared, ok := deps.PrepareReentry(DefaultDivisionID, character)
	if !ok {
		t.Fatal("re-entry preparation failed")
	}
	if prepared.InventorySize != 57 {
		t.Fatalf("prepared capacity = %d, want 57", prepared.InventorySize)
	}
	if !reflect.DeepEqual(character.Snapshot(), before) {
		t.Fatal("detached preparation mutated the character")
	}
	character.AdoptInventorySize(prepared.InventorySize)
	if character.InventorySize != 57 || character.InventoryExpansion != 0 {
		t.Fatalf("commit = %d (+%d), want 57 (+0)", character.InventorySize, character.InventoryExpansion)
	}
}
