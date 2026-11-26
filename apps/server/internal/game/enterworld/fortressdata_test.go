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
client-facing fortress list only while SRO_FORT_CONSTANTINOPLE is on.
Off is the shipped Jangan row plus the Bandit fortress's, which is
later-native content and always joins (#3586).
================
*/
func TestSiegeFortressRowsConstantinopleSwitch(t *testing.T) {
	rows := siegeFortressRows(nil)
	if len(rows) != 2 || rows[0].CodeName != "FORTRESS_JANGAN" || rows[1].CodeName != "FORTRESS_BIJEOKDAN" {
		t.Fatalf("switch off: rows %+v", rows)
	}
	t.Setenv(monster.EnvConstantinople, "on")
	rows = siegeFortressRows(nil)
	if len(rows) != 3 {
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
TestSiegeFortressRowsLaterFortresses

The Bandit row (id 6) is later-native content: it joins with no switch
set, and the retired SRO_FORT_BIJEOKDAN name in the environment no
longer switches it (#3586). The Hotan row (id 3) stays port-only
(#3886): it joins while its own switch is on. Rows keep fortress id
order around Constantinople's.
================
*/
func TestSiegeFortressRowsLaterFortresses(t *testing.T) {
	codes := func() string {
		var out []string
		for _, row := range siegeFortressRows(nil) {
			out = append(out, row.CodeName)
		}
		return strings.Join(out, " ")
	}
	if got := codes(); got != "FORTRESS_JANGAN FORTRESS_BIJEOKDAN" {
		t.Fatalf("no switches: %v", got)
	}
	// A shard still carrying the retired variable behaves natively.
	t.Setenv("SRO_FORT_BIJEOKDAN", "off")
	if got := codes(); got != "FORTRESS_JANGAN FORTRESS_BIJEOKDAN" {
		t.Fatalf("retired switch off: %v", got)
	}
	t.Setenv(monster.EnvHotanFortress, "on")
	if got := codes(); got != "FORTRESS_JANGAN FORTRESS_HOTAN FORTRESS_BIJEOKDAN" {
		t.Fatalf("hotan on: %v", got)
	}
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

// fortressOfficialSource models the production TextdataItems for a
// fortress official: the clerk is a stationary NPC row (MaxHP 0), so
// buildCharacterRef refuses it and CharacterRefByCodename misses, while
// the complete codename index still carries its RefObjID (#4475).
type fortressOfficialSource struct {
	ids map[string]uint32
}

func (s fortressOfficialSource) ItemRefByCodename(codename string) (*ItemRef, bool) {
	return nil, false
}

func (s fortressOfficialSource) ItemRefByID(id uint32) (*ItemRef, bool) {
	return nil, false
}

func (s fortressOfficialSource) CharacterRefByCodename(codename string) (*CharacterRef, bool) {
	return nil, false
}

func (s fortressOfficialSource) SummonableCharacterRefs() []CharacterRef {
	return nil
}

func (s fortressOfficialSource) CharacterRefObjIDByCodename(codename string) (uint32, bool) {
	id, ok := s.ids[codename]
	return id, ok
}

/*
================
TestSiegeFortressRowsOfficialRefObjID

#4475: the fortress war window lists the fortresses of the official in
conversation, matched by RefObjID. The officials are stationary NPC
rows the full character reference refuses, so the rows must resolve
their id through the complete codename index; resolving through
CharacterRefByCodename left every row at 0 and the window empty.
================
*/
func TestSiegeFortressRowsOfficialRefObjID(t *testing.T) {
	deps := &Deps{Items: fortressOfficialSource{ids: map[string]uint32{
		"NPC_CH_FORTRESS_OFFICIAL": 19520,
	}}}
	rows := siegeFortressRows(deps)
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].OfficialRefObjID != 19520 || rows[1].OfficialRefObjID != 19520 {
		t.Fatalf("official ids jangan=%d bandit=%d, want 19520", rows[0].OfficialRefObjID, rows[1].OfficialRefObjID)
	}

	deps.Items = fortressOfficialSource{ids: map[string]uint32{}}
	for _, row := range siegeFortressRows(deps) {
		if row.OfficialRefObjID != 0 {
			t.Fatalf("unresolvable official resolved: %+v", row)
		}
	}
}

/*
================
TestSiegeFortressRowsShippedOfficial

The shipped textdata itself: the Jangan fortress clerk's row carries
MaxHP 0, so the full character reference refuses it while the codename
index answers 19520. The bootstrap row must carry that id or the
fortress war window lists nothing (#4475).
================
*/
func TestSiegeFortressRowsShippedOfficial(t *testing.T) {
	items := sharedShippedItems(t)
	if _, found := items.CharacterRefByCodename("NPC_CH_FORTRESS_OFFICIAL"); found {
		t.Fatal("shipped official unexpectedly in the full character reference")
	}
	rows := siegeFortressRows(&Deps{Items: items})
	if len(rows) == 0 || rows[0].OfficialRefObjID != 19520 {
		t.Fatalf("jangan row %+v", rows)
	}
}
