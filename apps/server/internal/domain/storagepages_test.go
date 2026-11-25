/*
===========================================================================

storagepages_test.go - the warehouse page rule

===========================================================================
*/
package domain

import "testing"

/*
================
TestStoragePagesNativeByDefault
================
*/
func TestStoragePagesNativeByDefault(t *testing.T) {
	var native StoragePages
	if got := native.PersonalCapacity(StorageDefaultCapacity); got != 150 {
		t.Fatalf("native account warehouse = %d, want 150", got)
	}
	for level, want := range map[uint8]int64{1: 0, 2: 30, 3: 60, 4: 90, 5: 120} {
		if got := native.GuildCapacity(level); got != want {
			t.Errorf("native guild level %d = %d, want %d", level, got, want)
		}
	}
}

/*
================
TestStoragePagesRaise

Eight pages: 240 slots for the account; guild 2, 4, 6, 8 pages at levels
2 to 5, none at level 1. A stored capacity above the setting is kept.
================
*/
func TestStoragePagesRaise(t *testing.T) {
	pages := StoragePages{Personal: 8, Guild: 8}
	if got := pages.PersonalCapacity(150); got != 240 {
		t.Fatalf("account warehouse = %d, want 240", got)
	}
	if got := (StoragePages{Personal: 6}).PersonalCapacity(240); got != 240 {
		t.Fatalf("a lower setting lowered a raised warehouse to %d", got)
	}
	for level, want := range map[uint8]int64{1: 0, 2: 60, 3: 120, 4: 180, 5: 240} {
		if got := pages.GuildCapacity(level); got != want {
			t.Errorf("guild level %d = %d, want %d", level, got, want)
		}
	}
	// A setting that does not divide evenly rounds up, never below native.
	for level, want := range map[uint8]int64{2: 60, 3: 90, 4: 150, 5: 180} {
		if got := (StoragePages{Guild: 6}).GuildCapacity(level); got != want {
			t.Errorf("guild 6 pages, level %d = %d, want %d", level, got, want)
		}
	}
}

/*
================
TestParseStoragePages
================
*/
func TestParseStoragePages(t *testing.T) {
	for raw, want := range map[string]uint8{"": 0, "off": 0, "0": 0, "5": 5, "8": 8, " 7 ": 7} {
		got, err := ParseStoragePages("SRO_STORAGE_PAGES", raw, 5)
		if err != nil || got != want {
			t.Errorf("%q = %d, %v; want %d", raw, got, err, want)
		}
	}
	// Above the one-byte limit, below native, or not a number fails the boot.
	for _, raw := range []string{"9", "4", "on", "eight"} {
		if _, err := ParseStoragePages("SRO_STORAGE_PAGES", raw, 5); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
	if int64(MaxStoragePages)*StoragePageSlots > StorageMaxCapacity {
		t.Fatalf("MaxStoragePages overflows the capacity byte")
	}
}
