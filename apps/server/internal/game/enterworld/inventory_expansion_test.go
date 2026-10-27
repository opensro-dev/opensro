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
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
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
	payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, character.Name))

	outcome := HandleEnterWorld(deps, payload)
	if !outcome.OK {
		t.Fatalf("entry refused: %+v", outcome.Result)
	}
	result := outcome.Result
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
	again := HandleEnterWorld(deps, payload).Result
	if got := again.Packets[2].Payload[inventoryCapacityOffset]; got != 57 || character.InventorySize != 57 {
		t.Fatalf("repeat entry capacity = %d, record %d, want 57", got, character.InventorySize)
	}
}

/*
================
TestInventoryExpansionWaitsForBrowserEncoding

The native projection can succeed before the browser envelope fails. Both
login and live teleport reentry must preserve the old usable capacity then.
================
*/
func TestInventoryExpansionWaitsForBrowserEncoding(t *testing.T) {
	for _, reentry := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "reentry"}[reentry], func(t *testing.T) {
			character := chinaSpearman()
			character.InventoryExpansion = 10
			deps := testDeps(character)
			deps.SystemMessages = func(*Character) interface{} { return math.NaN() }
			projection := Build(deps, BootstrapRequest{CharacterName: character.Name})
			if projection.NativeResult != nativeResultSuccess {
				t.Fatalf("fixture did not reach browser encoding: %+v", projection)
			}
			payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, character.Name))
			if reentry {
				if packets, ok := deps.ReentryPackets(DefaultDivisionID, character.Name); ok || len(packets) != 0 {
					t.Fatal("unencodable reentry was accepted")
				}
			} else {
				outcome := HandleEnterWorld(deps, payload)
				if outcome.OK || outcome.Result.Reason != "blobEncodeFailed" {
					t.Fatalf("unencodable login was accepted: %+v", outcome.Result)
				}
			}
			if character.InventoryCapacity() != 45 || character.InventoryExpansion != 10 {
				t.Fatalf("failed encoding adopted capacity %d (+%d)", character.InventoryCapacity(), character.InventoryExpansion)
			}
			deps.SystemMessages = nil
			if reentry {
				if _, ok := deps.ReentryPackets(DefaultDivisionID, character.Name); !ok {
					t.Fatal("valid reentry refused")
				}
			} else if !HandleEnterWorld(deps, payload).OK {
				t.Fatal("valid login refused")
			}
			if character.InventoryCapacity() != 55 || character.InventoryExpansion != 0 {
				t.Fatalf("successful encoding did not adopt: %d (+%d)", character.InventoryCapacity(), character.InventoryExpansion)
			}
		})
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

/*
================
TestHandleEnterWorldStampsLastLoginWithoutInventoryExpansion

Every world entry stamps LastLoginAtMs on the live character even when no
inventory expansion is pending, while teleport and resurrection re-entry
packets leave LastLoginAtMs untouched.
================
*/
func TestHandleEnterWorldStampsLastLoginWithoutInventoryExpansion(t *testing.T) {
	character := chinaSpearman()
	character.InventoryExpansion = 0
	character.LastLoginAtMs = 0
	deps := testDeps(character)
	payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, character.Name))

	outcome := HandleEnterWorld(deps, payload)
	if !outcome.OK {
		t.Fatalf("entry refused: %+v", outcome.Result)
	}
	if character.LastLoginAtMs <= 0 {
		t.Fatalf("LastLoginAtMs = %d after login without inventory expansion, want > 0", character.LastLoginAtMs)
	}
	if character.InventoryCapacity() != 45 {
		t.Fatalf("InventoryCapacity = %d, want unchanged 45", character.InventoryCapacity())
	}

	character.LastLoginAtMs = 12345
	if _, ok := deps.ReentryPackets(DefaultDivisionID, character.Name); !ok {
		t.Fatal("ReentryPackets failed")
	}
	if _, ok := deps.PrepareReentry(DefaultDivisionID, character); !ok {
		t.Fatal("PrepareReentry failed")
	}
	if character.LastLoginAtMs != 12345 {
		t.Fatalf("LastLoginAtMs = %d after re-entry, want unchanged 12345", character.LastLoginAtMs)
	}
}

/*
================
TestReentryPacketsBuildThroughTheDeferredDoor

A teleport re-entry is built on the tick, so the bootstrap's own
writes (the seed and the encoded inventory adoption) ride the deferred
door and join the tick's batched commit (#1820). Login builds through
the immediate door, as it always has: only ReentryPackets swaps its
projection's door.
================
*/
func TestReentryPacketsBuildThroughTheDeferredDoor(t *testing.T) {
	doors := func(deps *Deps) (immediate, deferred *[]string) {
		seenImmediate, seenDeferred := []string{}, []string{}
		deps.MutateCharacter = func(_ *Character, label string, fn func()) {
			seenImmediate = append(seenImmediate, label)
			fn()
		}
		deps.MutateCharacterDeferred = func(_ *Character, label string, fn func()) {
			seenDeferred = append(seenDeferred, label)
			fn()
		}
		return &seenImmediate, &seenDeferred
	}
	contains := func(labels []string, want string) bool {
		for _, label := range labels {
			if label == want {
				return true
			}
		}
		return false
	}

	reentryCharacter := chinaSpearman()
	reentryCharacter.InventoryExpansion = 10
	reentryDeps := testDeps(reentryCharacter)
	immediate, deferred := doors(reentryDeps)
	if _, ok := reentryDeps.ReentryPackets(DefaultDivisionID, reentryCharacter.Name); !ok {
		t.Fatal("re-entry refused")
	}
	if len(*immediate) != 0 {
		t.Fatalf("re-entry built through the immediate door: %v", *immediate)
	}
	if !contains(*deferred, "inventory-expansion") {
		t.Fatalf("re-entry deferred writes = %v, want the inventory adoption among them", *deferred)
	}
	if reentryCharacter.InventoryCapacity() != 55 || reentryCharacter.InventoryExpansion != 0 {
		t.Fatalf("re-entry adoption = %d (+%d), want 55 (+0)", reentryCharacter.InventoryCapacity(), reentryCharacter.InventoryExpansion)
	}

	loginCharacter := chinaSpearman()
	loginCharacter.InventoryExpansion = 10
	loginDeps := testDeps(loginCharacter)
	immediate, deferred = doors(loginDeps)
	payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, loginCharacter.Name))
	if outcome := HandleEnterWorld(loginDeps, payload); !outcome.OK {
		t.Fatalf("login refused: %+v", outcome.Result)
	}
	if len(*deferred) != 0 {
		t.Fatalf("login built through the deferred door: %v", *deferred)
	}
	if !contains(*immediate, "inventory-expansion") {
		t.Fatalf("login immediate writes = %v, want the inventory adoption among them", *immediate)
	}
}
