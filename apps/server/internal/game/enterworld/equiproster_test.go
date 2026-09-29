/*
===========================================================================

equiproster_test.go - the starter items a creation choice grants

===========================================================================
*/

package enterworld

import (
	"bytes"
	"os"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

/*
================
TestResolveEquipRosterGrantsTheCreationStarterItems

The darkstaff choice grants the robe and the darkstaff in socket order.
================
*/
func TestResolveEquipRosterGrantsTheCreationStarterItems(t *testing.T) {
	character := europeWarlock()
	items := fakeItems{}
	for index, item := range CreationStarterItems(character, character.ModelCodename) {
		items[item.Codename] = &ItemRef{RefObjID: uint32(100 + index), Codename: item.Codename, TypeIDs: [4]int64{3, 1, 1, 1}}
	}
	roster := ResolveEquipRoster(character, character.ModelCodename, items, true)
	want := []struct {
		slot     int64
		codename string
	}{
		{slotChest, "ITEM_EU_W_CLOTHES_01_BA_A_DEF"},
		{slotLegs, "ITEM_EU_W_CLOTHES_01_LA_A_DEF"},
		{slotFeet, "ITEM_EU_W_CLOTHES_01_FA_A_DEF"},
		{slotWeapon, "ITEM_EU_DARKSTAFF_01_A_DEF"},
	}
	if len(roster) != len(want) {
		t.Fatalf("roster = %+v, want %d rows", roster, len(want))
	}
	for index, row := range want {
		if roster[index].Slot != row.slot || roster[index].Codename != row.codename {
			t.Errorf("row %d = slot %d %s, want slot %d %s", index, roster[index].Slot, roster[index].Codename, row.slot, row.codename)
		}
	}
	if got := ResolveEquipRoster(character, character.ModelCodename, items, false); len(got) != 0 {
		t.Fatalf("disabled equipment seeded items: %+v", got)
	}
}

/*
================
TestResolveEquipRosterMissingStarterItemIsAnExplicitSkip

A starter item missing from itemdata is skipped with a warning naming it,
never fabricated.
================
*/
func TestResolveEquipRosterMissingStarterItemIsAnExplicitSkip(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	character := chinaSpearman()
	roster := ResolveEquipRoster(character, character.ModelCodename, fakeItems{}, true)
	if len(roster) != 0 {
		t.Fatalf("missing starter item produced rows: %+v", roster)
	}
	if !strings.Contains(logged.String(), "ITEM_CH_SPEAR_01_A_DEF") {
		t.Errorf("missing starter item skipped without a diagnostic naming it; log output: %q", logged.String())
	}
}
