/*
===========================================================================

setpublication_test.go - the set tiers the world-entry blob carries

===========================================================================
*/

package enterworld

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

/*
================
publicationDeps

A Deps over a two-tier set: group 13 needs 2 pieces for one reinforce level
and 4 for two plus a resistance.
================
*/
func publicationDeps(t *testing.T) *Deps {
	t.Helper()
	dir := t.TempDir()
	writeSetTable(t, dir, "refmagicoptgroup.txt", [][]string{
		magicGroupRow("1", "2", "MAGIC_11_SET_A_DEFENSE_2", "389", "1"),
		magicGroupRow("1", "3", "MAGIC_11_SET_A_DEFENSE_4", "389", "2"),
		magicGroupRow("1", "3", "MAGIC_11_SET_A_DEFENSE_4", "390", "10"),
	})
	writeSetTable(t, dir, "refsetitemgroup.txt", [][]string{
		setGroupRow("1", "13", "ITEM_11_SET_A_CH_M_HEAVY", "0", "21", "2", "0", "3"),
	})
	return &Deps{SetGroups: NewTextdataSetGroups(dir)}
}

/*
================
TestPublishedSetsListTheTiers

The flag on lists each group with its tiers ascending, their options and
the name symbol; the flag off lists nothing.
================
*/
func TestPublishedSetsListTheTiers(t *testing.T) {
	deps := publicationDeps(t)
	if got := deps.PublishedSets(); got != nil {
		t.Fatalf("flag off published %+v", got)
	}
	t.Setenv(EnvCap110SetBonuses, "on")
	want := []SetPublication{{
		ID:   13,
		Name: "SN_ITEM_11_SET_A_CH_M_HEAVY",
		Tiers: []SetTierPublication{
			{Pieces: 2, Options: []SetOptionPublication{{Option: SetOptionReinforceItem, Value: 1}}},
			{Pieces: 4, Options: []SetOptionPublication{
				{Option: SetOptionReinforceItem, Value: 2},
				{Option: SetOptionResistAll, Value: 10},
			}},
		},
	}}
	if got := deps.PublishedSets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("published = %+v, want %+v", got, want)
	}
	var nilDeps *Deps
	if nilDeps.PublishedSets() != nil {
		t.Fatal("nil deps published sets")
	}
}

/*
================
TestBootstrapJSONPublishesEquipmentSets

The blob names the sets only when there are some; the native blob stays
byte-identical.
================
*/
func TestBootstrapJSONPublishesEquipmentSets(t *testing.T) {
	t.Setenv(EnvCap110SetBonuses, "on")
	for _, sets := range [][]SetPublication{nil, publicationDeps(t).PublishedSets()} {
		blob, err := json.Marshal(&BootstrapResult{
			NativeResult: nativeResultSuccess, Character: &Character{Name: "keeper", AccountID: "a"},
			LocalPlayerEntry: &LocalPlayerEntry{RaceKey: RaceKeyChina},
			ChatMessages:     []string{}, SystemMessages: []string{}, Packets: []Packet{},
			EquipmentSets: sets,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if got := strings.Contains(string(blob), `"equipmentSets":[{"id":13`); got != (sets != nil) {
			t.Fatalf("sets %v: blob carries them %v", sets != nil, got)
		}
		if sets == nil && strings.Contains(string(blob), "equipmentSets") {
			t.Fatal("the native blob names the sets")
		}
	}
}
