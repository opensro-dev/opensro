/*
===========================================================================

inventory_pages_test.go - world entry under the 2.5-page inventory base

The entry is where the client learns its capacity, so the port-only base
is presented and adopted there: raised on entry, never lowered, and a
record grown past it keeps every slot with the flag off.

===========================================================================
*/
package enterworld

import (
	"os"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

/*
================
TestInventoryPagesFromEnv

The owner's on is the default: only an explicit off value is native.
================
*/
func TestInventoryPagesFromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", true}, {"on", true}, {"ON", true}, {"1", true}, {"true", true},
		{"off", false}, {"OFF", false}, {"0", false}, {"false", false}, {" off ", false},
	} {
		t.Setenv(EnvInventoryPages, tc.value)
		if got := InventoryPagesFromEnv(); got != tc.want {
			t.Fatalf("%s=%q: got %v, want %v", EnvInventoryPages, tc.value, got, tc.want)
		}
	}
	if saved, had := os.LookupEnv(EnvInventoryPages); had {
		defer os.Setenv(EnvInventoryPages, saved)
	}
	os.Unsetenv(EnvInventoryPages)
	if !InventoryPagesFromEnv() {
		t.Fatal("unset must keep the owner's on")
	}
}

/*
================
enterWithPages

One world entry for the character under the given flag setting, returning
the presented capacity byte.
================
*/
func enterWithPages(t *testing.T, character *Character, pages bool) (uint8, *BootstrapResult) {
	t.Helper()
	deps := testDeps(character)
	deps.InventoryPages = pages
	payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, character.Name))
	outcome := HandleEnterWorld(deps, payload)
	if !outcome.OK {
		t.Fatalf("entry refused: %+v", outcome.Result)
	}
	return outcome.Result.Packets[2].Payload[inventoryCapacityOffset], outcome.Result
}

/*
================
TestWorldEntryRaisesToTheInventoryPagesBase

A native record enters at 2.5 pages and the live record adopts the base;
a quest-grown record below it is raised, not restarted.
================
*/
func TestWorldEntryRaisesToTheInventoryPagesBase(t *testing.T) {
	character := chinaSpearman()
	presented, result := enterWithPages(t, character, true)
	if presented != domain.InventoryPagesBase || bootstrapSlotCount(t, result) != int(domain.InventoryPagesBase) {
		t.Fatalf("entry presented %d (bootstrap %d), want %d", presented, bootstrapSlotCount(t, result), domain.InventoryPagesBase)
	}
	if character.InventorySize != domain.InventoryPagesBase {
		t.Fatalf("live record = %d, want %d adopted", character.InventorySize, domain.InventoryPagesBase)
	}
	if again, _ := enterWithPages(t, character, true); again != domain.InventoryPagesBase {
		t.Fatalf("repeat entry presented %d, want %d", again, domain.InventoryPagesBase)
	}

	grown := chinaSpearman()
	grown.InventorySize = 61
	if presented, _ := enterWithPages(t, grown, true); presented != domain.InventoryPagesBase || grown.InventorySize != domain.InventoryPagesBase {
		t.Fatalf("quest-grown entry = %d (record %d), want %d", presented, grown.InventorySize, domain.InventoryPagesBase)
	}
}

/*
================
TestWorldEntryNeverLowersInventoryCapacity

The flag never takes slots away: a three-page record enters whole under
either setting, and a native record with the flag off is untouched.
================
*/
func TestWorldEntryNeverLowersInventoryCapacity(t *testing.T) {
	for _, pages := range []bool{true, false} {
		full := chinaSpearman()
		full.InventorySize = domain.MaxInventorySize
		presented, _ := enterWithPages(t, full, pages)
		if presented != domain.MaxInventorySize || full.InventorySize != domain.MaxInventorySize {
			t.Fatalf("pages=%v: entry %d (record %d), want %d kept", pages, presented, full.InventorySize, domain.MaxInventorySize)
		}
	}

	native := chinaSpearman()
	presented, _ := enterWithPages(t, native, false)
	if presented != domain.DefaultInventorySize || native.InventorySize != 0 {
		t.Fatalf("native entry = %d (record %d), want %d and an untouched record", presented, native.InventorySize, domain.DefaultInventorySize)
	}
}
