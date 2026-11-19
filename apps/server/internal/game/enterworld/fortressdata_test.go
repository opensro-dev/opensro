package enterworld

import (
	"strings"
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

/*
================
TestSiegeFortressRowsLaterFortressSwitches

Port-only, not native (#3886): the Hotan (id 3) and Bandit (id 6) rows
join only while their own switches are on, in id order around
Constantinople's; each switch alone adds only its own row.
================
*/
func TestSiegeFortressRowsLaterFortressSwitches(t *testing.T) {
	codes := func() string {
		var out []string
		for _, row := range siegeFortressRows(nil) {
			out = append(out, row.CodeName)
		}
		return strings.Join(out, " ")
	}
	t.Setenv(monster.EnvBanditFortress, "on")
	if got := codes(); got != "FORTRESS_JANGAN FORTRESS_BIJEOKDAN" {
		t.Fatalf("bandit alone: %v", got)
	}
	t.Setenv(monster.EnvBanditFortress, "off")
	t.Setenv(monster.EnvHotanFortress, "on")
	if got := codes(); got != "FORTRESS_JANGAN FORTRESS_HOTAN" {
		t.Fatalf("hotan alone: %v", got)
	}
	t.Setenv(monster.EnvBanditFortress, "on")
	t.Setenv(monster.EnvConstantinople, "on")
	if got := codes(); got != "FORTRESS_JANGAN FORTRESS_HOTAN FORTRESS_CONSTANTINOPLE FORTRESS_BIJEOKDAN" {
		t.Fatalf("all on: %v", got)
	}
	rows := siegeFortressRows(nil)
	hotan, bandit := rows[1], rows[3]
	if hotan.FortressID != 3 || hotan.Icon != "etc/fort_hotan.ddj" || hotan.NameStrID != "SN_FORTRESS_HOTAN" ||
		hotan.OfficialNpcCode != "NPC_KT_FORTRESS_OFFICIAL" || hotan.RequestFee != 1000000 || hotan.TaxTargets != 63 {
		t.Fatalf("hotan row %+v", hotan)
	}
	if bandit.FortressID != 6 || bandit.Icon != "etc/fort_bijeokdan.ddj" || bandit.NameStrID != "SN_FORTRESS_BIJEOKDAN" ||
		bandit.OfficialNpcCode != "NPC_CH_FORTRESS_OFFICIAL" || bandit.RequestFee != 500000 || bandit.TaxTargets != 96 {
		t.Fatalf("bandit row %+v", bandit)
	}
}
