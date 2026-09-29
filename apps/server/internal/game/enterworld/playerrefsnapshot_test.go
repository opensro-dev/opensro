package enterworld

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCharacterModelRefObjSnapshotCarriesPlayerAdmissionFields(t *testing.T) {
	roster := &Roster{Models: []RosterModel{
		{Codename: "CHAR_CH_MAN_SCHOLAR", RefObjID: 1917},
		{Codename: "CHAR_EU_WOMAN_ADVENTURER", RefObjID: 14738},
		{Codename: "DUPLICATE", RefObjID: 1917},
		{Codename: "MISSING_REF"},
	}}
	rows := CharacterModelRefObjSnapshot(roster)
	if len(rows) != 2 {
		t.Fatalf("player snapshot rows = %d, want 2 unique valid models: %+v", len(rows), rows)
	}
	chinaMan := rows[0]
	if chinaMan.RefObjID != 1917 || chinaMan.TidWord != PlayerRefObjTIDWord ||
		chinaMan.Kind != "player" || chinaMan.CountryByte9C == nil || *chinaMan.CountryByte9C != 0 ||
		chinaMan.SexSelector1AC == nil || *chinaMan.SexSelector1AC != 1 {
		t.Fatalf("China man row lacks CICUser admission fields: %+v", chinaMan)
	}
	europeWoman := rows[1]
	if europeWoman.CountryByte9C == nil || *europeWoman.CountryByte9C != 1 ||
		europeWoman.SexSelector1AC == nil || *europeWoman.SexSelector1AC != 0 {
		t.Fatalf("Europe woman selectors = %+v", europeWoman)
	}
	encoded, err := json.Marshal(europeWoman)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, field := range []string{`"countryByte9c":1`, `"sexSelector1ac":0`} {
		if !strings.Contains(text, field) {
			t.Errorf("player row JSON %s lacks %s", text, field)
		}
	}
}

func TestBuildRefItemSnapshotStocksDivisionPeerEquipment(t *testing.T) {
	viewer := chinaSpearman()
	viewer.Name = "Viewer"
	peer := chinaSpearman()
	peer.Name = "Peer"
	peer.MissionInventory = []InventoryRow{{
		Slot: 6, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x1b2c,
	}}
	deps := testDeps(viewer, peer)
	rows := buildRefItemSnapshot(deps, DefaultDivisionID, viewer)
	found := false
	for _, row := range rows {
		if row.RefObjID == 107 {
			found = true
			if row.TypeFlags != 0x1b2c {
				t.Fatalf("peer item type word = %#x, want persisted %#x", row.TypeFlags, 0x1b2c)
			}
		}
	}
	if !found {
		t.Fatal("viewer bootstrap did not stock the peer's worn RefItem row")
	}
}
