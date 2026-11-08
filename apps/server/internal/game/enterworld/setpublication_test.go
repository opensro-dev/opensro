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
	// Two pieces of group 13 (the shoulder listed before the head, and a rare twin of the head sharing
	// its name symbol), a piece of another group and a disabled row.
	writeSetTable(t, dir, "itemdata_5000.txt", [][]string{
		pieceRow("1", "SN_PIECE_SHOULDER", "2", "13"),
		pieceRow("1", "SN_PIECE_HEAD", "1", "13"),
		pieceRow("1", "SN_PIECE_HEAD", "1", "13"),
		pieceRow("1", "SN_OTHER_SET", "1", "14"),
		pieceRow("0", "SN_DISABLED", "3", "13"),
	})
	return &Deps{SetGroups: NewTextdataSetGroups(dir)}
}

/*
================
pieceRow

An itemdata row with the cells the set pieces are read from: service, name symbol, slot (TypeID4)
and set group (column 62).
================
*/
func pieceRow(service, name, slot, group string) []string {
	row := make([]string, 70)
	for i := range row {
		row[i] = "0"
	}
	row[0], row[5], row[12], row[62] = service, name, slot, group
	return row
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
		ID:     13,
		Name:   "SN_ITEM_11_SET_A_CH_M_HEAVY",
		Pieces: []SetPiece{{Slot: 1, Name: "SN_PIECE_HEAD"}, {Slot: 2, Name: "SN_PIECE_SHOULDER"}},
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
