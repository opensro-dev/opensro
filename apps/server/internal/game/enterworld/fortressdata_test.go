package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestSiegeFortressRowsConstantinopleSwitch

Port-only, not native (#3275): the Constantinople row joins the
client-facing fortress list only while SRO_FORT_CONSTANTINOPLE is on;
off is exactly the shipped Jangan row.
================
*/
func TestSiegeFortressRowsConstantinopleSwitch(t *testing.T) {
	rows := siegeFortressRows(nil)
	if len(rows) != 1 || rows[0].CodeName != "FORTRESS_JANGAN" {
		t.Fatalf("switch off: rows %+v", rows)
	}
	t.Setenv(monster.EnvConstantinople, "on")
	rows = siegeFortressRows(nil)
	if len(rows) != 2 {
		t.Fatalf("switch on: rows %+v", rows)
	}
	got := rows[1]
	if got.FortressID != 4 || got.CodeName != "FORTRESS_CONSTANTINOPLE" ||
		got.NameStrID != "SN_FORTRESS_CONSTANTINOPLE" ||
		got.OfficialNpcCode != "NPC_EU_FORTRESS_OFFICIAL" ||
		got.RequestFee != 5000000 || got.TaxTargets != 63 {
		t.Fatalf("constantinople row %+v", got)
	}
	if rows[0].CodeName != "FORTRESS_JANGAN" {
		t.Fatalf("jangan row moved: %+v", rows)
	}
}
