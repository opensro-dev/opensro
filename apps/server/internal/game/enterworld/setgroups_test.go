/*
===========================================================================

setgroups_test.go - tests for setgroups.go

===========================================================================
*/

package enterworld

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

/*
================
writeSetTable

writeSetTable writes one textdata table as tab-joined rows, the shape
readTextdataFile splits back.
================
*/
func writeSetTable(t *testing.T, dir, name string, rows [][]string) {
	t.Helper()
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, strings.Join(row, "\t"))
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

/*
================
setGroupRow

setGroupRow authors one 19-field refsetitemgroup row; links fill the
(figure, group) cells from index 6.
================
*/
func setGroupRow(service, group, code string, links ...string) []string {
	row := []string{service, group, code, "later name", "SN_" + code, "xxx"}
	row = append(row, links...)
	for len(row) < 19 {
		row = append(row, "0")
	}
	return row
}

/*
================
magicGroupRow

magicGroupRow authors one 12-field refmagicoptgroup row.
================
*/
func magicGroupRow(service, group, code, option, value string) []string {
	return []string{service, group, "2", code, option, value, "1", "0", "xxx", "0", "xxx", ""}
}

/*
================
TestTextdataSetGroupsLoadsTheChain

The loader keeps only the dormant degree-11 set rows, resolves each
group link's requirement off its magic group codename, and drops
service-off rows, non-set magic groups and links to unknown groups.
================
*/
func TestTextdataSetGroupsLoadsTheChain(t *testing.T) {
	dir := t.TempDir()
	writeSetTable(t, dir, "refmagicoptgroup.txt", [][]string{
		magicGroupRow("1", "2", "MAGIC_11_SET_A_DEFENSE_2", "389", "1"),
		magicGroupRow("1", "2", "MAGIC_11_SET_A_DEFENSE_2", "390", "10"),
		magicGroupRow("1", "3", "MAGIC_11_SET_A_DEFENSE_4", "389", "2"),
		magicGroupRow("1", "4", "MAGIC_11_SET_A_DEFENSE_6", "389", "4"),
		magicGroupRow("1", "77", "MAGIC_OPTION_SET_LIKE", "389", "9"),
		magicGroupRow("0", "8", "MAGIC_11_SET_A_DEFENSE_2", "389", "99"),
	})
	writeSetTable(t, dir, "refsetitemgroup.txt", [][]string{
		setGroupRow("1", "13", "ITEM_11_SET_A_CH_M_HEAVY", "0", "21", "2", "0", "3", "0", "4"),
		setGroupRow("1", "14", "ITEM_11_SET_B_EU_M_LIGHT", "0", "5", "99"),
		setGroupRow("0", "15", "ITEM_11_SET_A_CH_F_HEAVY", "0", "21", "2"),
	})

	catalogue := NewTextdataSetGroups(dir)
	if got := catalogue.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1 surviving group", got)
	}
	wantTiers := []SetTier{
		{RequiredCount: 2, MagicGroupID: 2},
		{RequiredCount: 4, MagicGroupID: 3},
		{RequiredCount: 6, MagicGroupID: 4},
	}
	if got := catalogue.SetGroupTiers(13); !reflect.DeepEqual(got, wantTiers) {
		t.Fatalf("tiers(13) = %+v, want %+v", got, wantTiers)
	}
	wantOptions := []SetOption{
		{OptionID: SetOptionReinforceItem, Value: 1},
		{OptionID: SetOptionResistAll, Value: 10},
	}
	if got := catalogue.MagicGroupOptions(2); !reflect.DeepEqual(got, wantOptions) {
		t.Fatalf("options(2) = %+v, want %+v", got, wantOptions)
	}
	if got := catalogue.MagicGroupOptions(3); !reflect.DeepEqual(got,
		[]SetOption{{OptionID: SetOptionReinforceItem, Value: 2}}) {
		t.Fatalf("options(3) = %+v, want the lone 389 row", got)
	}
	for _, absent := range []struct {
		name string
		got  int
	}{
		{"tiers(14) link to unknown magic group", len(catalogue.SetGroupTiers(14))},
		{"tiers(15) service-off group", len(catalogue.SetGroupTiers(15))},
		{"options(77) non-set magic group", len(catalogue.MagicGroupOptions(77))},
		{"options(8) service-off magic row", len(catalogue.MagicGroupOptions(8))},
	} {
		if absent.got != 0 {
			t.Fatalf("%s loaded %d row(s), want none", absent.name, absent.got)
		}
	}
}

/*
================
TestTextdataSetGroupsAbsentFilesAreEmpty

A native shard's textdata has neither overlay table; the catalogue
degrades to empty instead of failing.
================
*/
func TestTextdataSetGroupsAbsentFilesAreEmpty(t *testing.T) {
	catalogue := NewTextdataSetGroups(t.TempDir())
	if got := catalogue.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
	if got := catalogue.SetGroupTiers(13); len(got) != 0 {
		t.Fatalf("tiers(13) = %+v, want empty", got)
	}
	if got := catalogue.MagicGroupOptions(2); len(got) != 0 {
		t.Fatalf("options(2) = %+v, want empty", got)
	}
}

/*
================
TestSetTierRequirement

The requirement is the codename's trailing count, bounded by the
equipment slots; anything else names no tier.
================
*/
func TestSetTierRequirement(t *testing.T) {
	cases := []struct {
		codename string
		want     int
		ok       bool
	}{
		{"MAGIC_11_SET_A_DEFENSE_2", 2, true},
		{"MAGIC_11_SET_A_DEFENSE_6", 6, true},
		{"MAGIC_11_SET_B_RESIST_13", 13, true},
		{"MAGIC_11_SET_A_DEFENSE_1", 0, false},
		{"MAGIC_11_SET_A_DEFENSE_14", 0, false},
		{"MAGIC_11_SET_A_DEFENSE", 0, false},
		{"MAGIC_11_SET_A_DEFENSE_", 0, false},
		{"MAGIC_11_SET_A_DEFENSE_X", 0, false},
	}
	for _, tc := range cases {
		got, ok := setTierRequirement(tc.codename)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("setTierRequirement(%q) = %d, %v; want %d, %v",
				tc.codename, got, ok, tc.want, tc.ok)
		}
	}
}

/*
================
TestBuildItemRefReadsTheSetColumn

Column 62 of an itemdata row is the piece's set group; native rows and
short rows read as no set.
================
*/
func TestBuildItemRefReadsTheSetColumn(t *testing.T) {
	fields := make([]string, 64)
	fields[1] = "5001"
	fields[2] = "ITEM_CH_TEST_SET_A"
	fields[62] = "13"
	ref := buildItemRef(fields, nil)
	if ref == nil {
		t.Fatal("buildItemRef rejected the fixture row")
	}
	if ref.SetID != 13 {
		t.Fatalf("SetID = %d, want 13", ref.SetID)
	}
	fields[62] = "0"
	if ref = buildItemRef(fields, nil); ref == nil || ref.SetID != 0 {
		t.Fatalf("SetID = %v, want 0 for an unaffiliated piece", ref)
	}
	if ref = buildItemRef(fields[:40], nil); ref == nil || ref.SetID != 0 {
		t.Fatalf("SetID = %v, want 0 for a short row", ref)
	}
}

/*
================
TestSetBonusesEnabledReadsTheSwitch
================
*/
func TestSetBonusesEnabledReadsTheSwitch(t *testing.T) {
	cases := map[string]bool{
		"": false, "off": false, "0": false, "no": false,
		"1": true, "on": true, "true": true, "ON": true, " True ": true,
	}
	for value, want := range cases {
		t.Setenv(EnvCap110SetBonuses, value)
		if got := SetBonusesEnabled(); got != want {
			t.Fatalf("SetBonusesEnabled with %q = %v, want %v", value, got, want)
		}
	}
}

/*
================
TestSetCatalogueGatesOnTheFlag

Combat only ever sees the catalogue while the port-only flag is on;
that nil is what keeps the off snapshot byte-identical.
================
*/
func TestSetCatalogueGatesOnTheFlag(t *testing.T) {
	deps := &Deps{SetGroups: NewTextdataSetGroups(t.TempDir())}
	if deps.SetCatalogue() != nil {
		t.Fatal("flag off handed combat a set catalogue")
	}
	t.Setenv(EnvCap110SetBonuses, "on")
	if deps.SetCatalogue() == nil {
		t.Fatal("flag on hid the set catalogue")
	}
	var nilDeps *Deps
	if nilDeps.SetCatalogue() != nil {
		t.Fatal("nil deps handed out a catalogue")
	}
	if (&Deps{}).SetCatalogue() != nil {
		t.Fatal("deps without a catalogue handed one out")
	}
}
