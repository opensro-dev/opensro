/*
===========================================================================

inventorypages_test.go - each pet kind's bag pages read on their own

The default stays the owner's five pages for both kinds, the older shared
setting still fills a kind left unset, and a typo fails boot.

===========================================================================
*/
package companion

import "testing"

/*
================
setPetInventoryPages

Sets the shared, grab and transport settings for one case.
================
*/
func setPetInventoryPages(t *testing.T, shared, grab, transport string) {
	t.Helper()
	t.Setenv(EnvPetInventoryPages, shared)
	t.Setenv(EnvGrabPetInventoryPages, grab)
	t.Setenv(EnvTransportPetInventoryPages, transport)
}

/*
================
TestPetInventoryPagesDefaultsToFivePages
================
*/
func TestPetInventoryPagesDefaultsToFivePages(t *testing.T) {
	setPetInventoryPages(t, "", "", "")
	pages, err := PetInventoryPagesFromEnv()
	if err != nil || pages != (PetInventoryPages{Grab: 5, Transport: 5}) {
		t.Fatalf("unset = %+v, %v; want five pages for both kinds", pages, err)
	}
	if PetInventorySlots != 5*28 {
		t.Fatalf("five-page bag = %d slots, want 140", PetInventorySlots)
	}
}

/*
================
TestPetInventoryPagesKindsAreIndependent
================
*/
func TestPetInventoryPagesKindsAreIndependent(t *testing.T) {
	cases := []struct {
		shared, grab, transport string
		want                    PetInventoryPages
	}{
		{"", "off", "", PetInventoryPages{Grab: 0, Transport: 5}},
		{"", "", "off", PetInventoryPages{Grab: 5, Transport: 0}},
		{"", "3", "9", PetInventoryPages{Grab: 3, Transport: 9}},
		{"", " ON ", "1", PetInventoryPages{Grab: 5, Transport: 1}},
		{"", "false", "0", PetInventoryPages{}},
		// The older shared setting fills only a kind left unset.
		{"off", "", "", PetInventoryPages{}},
		{"off", "4", "", PetInventoryPages{Grab: 4, Transport: 0}},
		{"on", "", "off", PetInventoryPages{Grab: 5, Transport: 0}},
		{"1", "2", "", PetInventoryPages{Grab: 2, Transport: 5}},
	}
	for _, c := range cases {
		setPetInventoryPages(t, c.shared, c.grab, c.transport)
		pages, err := PetInventoryPagesFromEnv()
		if err != nil || pages != c.want {
			t.Fatalf("shared %q grab %q transport %q = %+v, %v; want %+v", c.shared, c.grab, c.transport, pages, err, c.want)
		}
	}
}

/*
================
TestPetInventoryPagesTypoFailsBoot

A raised bag persists, so an unreadable value is a boot error instead of
a guess.
================
*/
func TestPetInventoryPagesTypoFailsBoot(t *testing.T) {
	cases := [][3]string{
		{"of", "", ""},
		{"2", "", ""},
		{"", "10", ""},
		{"", "-1", ""},
		{"", "unexpected", ""},
		{"", "", "of"},
		{"", "", "256"},
	}
	for _, c := range cases {
		setPetInventoryPages(t, c[0], c[1], c[2])
		if pages, err := PetInventoryPagesFromEnv(); err == nil {
			t.Fatalf("shared %q grab %q transport %q read as %+v; want a boot error", c[0], c[1], c[2], pages)
		}
	}
}

/*
================
TestPetInventoryCapacityOnlyRaises
================
*/
func TestPetInventoryCapacityOnlyRaises(t *testing.T) {
	pages := PetInventoryPages{Grab: 3, Transport: 9}
	if got := pages.GrabCapacity(28); got != 84 {
		t.Fatalf("three-page grab bag = %d, want 84", got)
	}
	if got := pages.TransportCapacity(56); got != 252 {
		t.Fatalf("nine-page transport bag = %d, want 252", got)
	}
	if got := pages.GrabCapacity(140); got != 140 {
		t.Fatalf("a 140-slot grab bag under three pages = %d; a bag is never lowered", got)
	}
	native := PetInventoryPages{}
	if native.GrabCapacity(28) != 28 || native.TransportCapacity(56) != 56 {
		t.Fatal("off must keep the native sizes")
	}
}
