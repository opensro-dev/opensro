/*
===========================================================================

muhan_test.go - the Muhan wave file's load contract.

Off never reads the file; on refuses an absent, malformed or
wrong-version table before startup, and carries a good table whole.

===========================================================================
*/
package monster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const muhanWavesFixture = `{"Version": 2, "Rows": [
	{"WorldID": 103, "Seq": 1, "RegionID": 27091, "X": 1248, "Y": 378, "Z": 1379,
		"DelayMs": 5000, "Count": 4, "LevelOffset": -3, "Radius": 50, "MonsterType": 0},
	{"WorldID": 103, "Seq": 20, "RegionID": 27091, "X": 1519, "Y": 378, "Z": 562,
		"DelayMs": 72000, "Count": 1, "LevelOffset": 5, "Radius": 200, "MonsterType": 7}
]}
`

/*
================
TestLoadMuhanWavesFollowsTheSwitch
================
*/
func TestLoadMuhanWavesFollowsTheSwitch(t *testing.T) {
	dir := t.TempDir()
	waves, err := LoadMuhanWaves(dir, false)
	if err != nil || waves != nil {
		t.Fatalf("off: waves %v, err %v; want nil, nil (the file is never read)", waves, err)
	}
	if _, err := LoadMuhanWaves(dir, true); err == nil || !strings.Contains(err.Error(), muhanWavesFile) {
		t.Fatalf("on without the file: err %v; want a refusal naming %s", err, muhanWavesFile)
	}
	if err := os.WriteFile(filepath.Join(dir, muhanWavesFile), []byte(muhanWavesFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	waves, err = LoadMuhanWaves(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(waves) != 2 {
		t.Fatalf("waves: got %d, want 2", len(waves))
	}
	first, last := waves[0], waves[1]
	if first.WorldID != 103 || first.Seq != 1 || first.LevelOffset != -3 || first.DelayMs != 5000 || first.Count != 4 {
		t.Fatalf("first wave decoded wrong: %+v", first)
	}
	if last.Seq != 20 || last.MonsterType != 7 || last.Radius != 200 || last.X != 1519 || last.Z != 562 {
		t.Fatalf("last wave decoded wrong: %+v", last)
	}
}

const muhanNPCsFixture = `{"Version": 2, "Rows": [
	{"Codename": "NPC_MUHAN_SOLO_75_MANAGER", "NestID": 39604, "WorldID": 103, "RegionID": 27091,
		"X": 779, "Y": 628, "Z": 1108, "Radius": 0, "GenerateRadius": 0, "MaxCount": 1,
		"RespawnDelayMinSec": 0, "RespawnDelayMaxSec": 0, "Respawn": true, "InitialDir": 0,
		"SpawnType": 0, "ChampionGenPercentage": 0, "Controls": {"ID": 1489, "ObjectID": 40421}, "Champion": null},
	{"Codename": "NPC_MUHAN_CHANGE_SOLO_75_A", "NestID": 39605, "WorldID": 103, "RegionID": 27091,
		"X": 1142, "Y": 382, "Z": 1128, "Radius": 0, "GenerateRadius": 0, "MaxCount": 1,
		"RespawnDelayMinSec": 0, "RespawnDelayMaxSec": 0, "Respawn": true, "InitialDir": 0,
		"SpawnType": 0, "ChampionGenPercentage": 0, "Controls": {"ID": 1481, "ObjectID": 40458}, "Champion": null}
]}
`

/*
================
TestAppendMuhanNPCsJoinsArenaWorlds

The arena NPCs join their wave world; a wave kind in the NPC file,
and a nest naming an unknown world, refuse before startup.
================
*/
func TestAppendMuhanNPCsJoinsArenaWorlds(t *testing.T) {
	template := Template{Refs: map[uint32]MonsterRef{
		101: {Codename: "NPC_MUHAN_SOLO_75_MANAGER"},
		102: {Codename: "NPC_MUHAN_CHANGE_SOLO_75_A"},
		103: {Codename: "MOB_MUHAN_L75"},
	}}
	got, err := appendMuhanNPCData(template, []byte(muhanNPCsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nests) != 2 {
		t.Fatalf("nests: got %d, want 2", len(got.Nests))
	}
	for _, nest := range got.Nests {
		if nest.WorldCode != "GROUP_MUHAN_SOLO_75" || nest.RegionID != 27091 || nest.MaxCount != 1 || !nest.Respawn {
			t.Fatalf("nest joined wrong: %+v", nest)
		}
	}
	bad := `{"Version": 2, "Rows": [{"Codename": "MOB_MUHAN_L75", "NestID": 1, "WorldID": 103, "RegionID": 27091, "MaxCount": 1}]}`
	if _, err := appendMuhanNPCData(template, []byte(bad)); err == nil {
		t.Fatal("a wave kind in the npc file joined; want a refusal")
	}
	badWorld := `{"Version": 2, "Rows": [{"Codename": "NPC_MUHAN_SOLO_75_MANAGER", "NestID": 1, "WorldID": 99, "RegionID": 27091, "MaxCount": 1}]}`
	if _, err := appendMuhanNPCData(template, []byte(badWorld)); err == nil {
		t.Fatal("a nest in the unbound shell world joined; want a refusal")
	}
}

/*
================
TestParseMuhanWavesRefusesMalformedTables
================
*/
func TestParseMuhanWavesRefusesMalformedTables(t *testing.T) {
	for name, data := range map[string]string{
		"not json":      `{`,
		"version 1":     `{"Version": 1, "Rows": [{"WorldID": 103}]}`,
		"no rows":       `{"Version": 2, "Rows": []}`,
		"missing rows":  `{"Version": 2}`,
		"missing field": `{"Version": 2, "Rows": [{"WorldID": "103"}]}`,
	} {
		if _, err := ParseMuhanWaves([]byte(data)); err == nil {
			t.Fatalf("%s: parsed, want a refusal", name)
		}
	}
}
