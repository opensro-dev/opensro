package store

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// The 2.5-page base is part of the record from birth when the option is
// wired, absent otherwise, and a record created under it keeps its slots
// when the store later opens without it (flag on -> off loses nothing).
func TestCreateCharacterInventoryPagesBase(t *testing.T) {
	dir := t.TempDir()
	pages, err := Open(dir, Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder, InventoryPages: true})
	if err != nil {
		t.Fatal(err)
	}
	raised := &enterworld.Character{Name: "pager", ModelCodename: "CHAR_CH_MAN_01"}
	if err := pages.CreateCharacter(testDivision, "test-account", raised); err != nil {
		t.Fatal(err)
	}
	if raised.InventorySize != domain.InventoryPagesBase || raised.InventoryCapacity() != domain.InventoryPagesBase {
		t.Fatalf("created at %d, want the %d base stamped", raised.InventorySize, domain.InventoryPagesBase)
	}
	pages.Close()

	native, err := Open(dir, Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	kept := native.Characters().CharactersForDivision(testDivision)[0]
	if kept.InventoryCapacity() != domain.InventoryPagesBase {
		t.Fatalf("reopened without the flag at %d, want %d kept", kept.InventoryCapacity(), domain.InventoryPagesBase)
	}
	fresh := &enterworld.Character{Name: "native", ModelCodename: "CHAR_CH_MAN_01"}
	if err := native.CreateCharacter(testDivision, "test-account", fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.InventorySize != 0 || fresh.InventoryCapacity() != domain.DefaultInventorySize {
		t.Fatalf("native creation = %d, want an unstamped %d", fresh.InventorySize, domain.DefaultInventorySize)
	}
}
