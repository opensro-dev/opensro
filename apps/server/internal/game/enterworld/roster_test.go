package enterworld

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRoster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.json")
	fixture := `{
		"format": "sro-server-character-authority",
		"version": 2,
		"models": [
			{"codename": "CHAR_CH_MAN_ADVENTURER", "refObjId": 1907, "bodyRadius": 4}
		]
	}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	roster, err := LoadRoster(path)
	if err != nil {
		t.Fatalf("LoadRoster: %v", err)
	}
	if model := roster.ModelByRefObjID(1907); model == nil || model.Codename != "CHAR_CH_MAN_ADVENTURER" || model.BodyRadius != 4 {
		t.Fatalf("ModelByRefObjID(1907) = %+v", model)
	}
	if model := roster.ModelByCodename("CHAR_CH_MAN_ADVENTURER"); model == nil || model.RefObjID != 1907 {
		t.Fatalf("ModelByCodename = %+v", model)
	}
}

func TestRosterNilAndZeroLookups(t *testing.T) {
	var nilRoster *Roster
	if nilRoster.ModelByRefObjID(1907) != nil || nilRoster.ModelByCodename("X") != nil {
		t.Error("nil roster lookups must return nil")
	}
	roster := testRoster()
	if roster.ModelByRefObjID(0) != nil {
		t.Error("refObjId 0 means absent and must not match")
	}
	if roster.ModelByCodename("") != nil {
		t.Error("empty codename must not match")
	}
}

func TestLoadRosterRejectsBrowserPresentationFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	fixture := `{"format":"sro-server-character-authority","version":2,"models":[{"codename":"CHAR_CH_MAN_ADVENTURER","refObjId":1907,"bodyRadius":4,"glb":"/assets/char/player.glb"}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoster(path); err == nil {
		t.Fatal("character authority accepted browser presentation data")
	}
}

func TestLoadRosterRejectsMissingBodyRadius(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	fixture := `{"format":"sro-server-character-authority","version":2,"models":[{"codename":"CHAR_CH_MAN_ADVENTURER","refObjId":1907}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoster(path); err == nil {
		t.Fatal("character authority accepted a model without its body radius")
	}
}

func TestShippedPlayerBodyRadiusStaysPinned(t *testing.T) {
	if _, err := os.Stat(realAssetPaths(t).RosterPath); err != nil {
		t.Skipf("server character-authority projection is unavailable: %v", err)
	}
	roster, err := LoadRoster(realAssetPaths(t).RosterPath)
	if err != nil {
		t.Fatalf("load shipped character authority: %v", err)
	}
	model := roster.ModelByRefObjID(1907)
	if model == nil || model.Codename != "CHAR_CH_MAN_ADVENTURER" || model.BodyRadius != 4 {
		t.Fatalf("shipped player body-radius authority moved: %+v", model)
	}
}
