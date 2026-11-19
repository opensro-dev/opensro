/*
===========================================================================

muhan_npcs_test.go - the Muhan arena NPCs load from the import's own rows

The rows are written in the producer's layout (characterdata with the
NPC class the import gives these references, no hit points, and the
nest file muhan_import.py writes), never as hand-built references: a
fixture that hands the loader its references cannot catch a class gate
that drops them, which is how SRO_MUHAN failed its first boot (r203).

===========================================================================
*/
package simulation

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/instance"
)

// muhanTestColumns covers every characterdata column the loader reads.
const muhanTestColumns = 100

/*
================
muhanCharacterRow

One characterdata row as the import writes an arena reference: TID
1/2/<tid3>, scale 200, level 0, no hit points.
================
*/
func muhanCharacterRow(refID int, codename string, tid3 int) string {
	cols := make([]string, muhanTestColumns)
	for index := range cols {
		cols[index] = "0"
	}
	cols[npcColService] = "1"
	cols[npcColRefObjID] = strconv.Itoa(refID)
	cols[npcColCodename] = codename
	cols[npcColNameStrID] = "SN_" + codename
	cols[npcColCharBit] = "1"
	cols[npcColTid1] = "1"
	cols[npcColTid2] = "2"
	cols[npcColTid3] = strconv.Itoa(tid3)
	cols[npcColScale] = "200"
	cols[npcColModel] = "mob\\muhan.bsr"
	return strings.Join(cols, "\t")
}

/*
================
muhanTextdata

A textdata folder holding the arena references and the nest file.
================
*/
func muhanTextdata(t *testing.T, nests string) string {
	t.Helper()
	dir := t.TempDir()
	characters := strings.Join([]string{
		muhanCharacterRow(51209, "NPC_MUHAN_SOLO_75_MANAGER", 2),
		muhanCharacterRow(51200, "NPC_MUHAN_CHANGE_SOLO_75_A", 2),
		// A monster-class row: an arena wave kind, never an arena NPC.
		muhanCharacterRow(51300, "NPC_MUHAN_WAVE_KIND", 1),
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "characterdata_220000.txt"), []byte(characters), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, muhanNpcsFile), []byte(nests), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// muhanNestRow is one nest as muhan_import.py writes it.
func muhanNestRow(codename string, world int) string {
	return `{"Codename": "` + codename + `", "NestID": 39604, "WorldID": ` + strconv.Itoa(world) +
		`, "RegionID": 27091, "X": 779, "Y": 628, "Z": 1108, "Radius": 0, "GenerateRadius": 0, "MaxCount": 1,
		"RespawnDelayMinSec": 0, "RespawnDelayMaxSec": 0, "Respawn": true, "InitialDir": 0, "SpawnType": 0,
		"ChampionGenPercentage": 0, "Controls": {"ID": 1489}, "Champion": null}`
}

/*
================
TestMuhanArenaNpcsStandInTheirWorld

Both NPC-class arena references load as NPCs of their arena world, in
the arena band, with the identities the file's order gives them.
================
*/
func TestMuhanArenaNpcsStandInTheirWorld(t *testing.T) {
	dir := muhanTextdata(t, `{"Version": 2, "Rows": [`+muhanNestRow("NPC_MUHAN_SOLO_75_MANAGER", 103)+`, `+
		muhanNestRow("NPC_MUHAN_CHANGE_SOLO_75_A", 104)+`]}`)
	npcs, err := LoadMuhanArenaNpcs(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(npcs) != 2 {
		t.Fatalf("npcs: got %d, want 2", len(npcs))
	}
	manager, change := npcs[0], npcs[1]
	if manager.World != instance.DefinitionID(103) || manager.Npc.Codename != "NPC_MUHAN_SOLO_75_MANAGER" ||
		manager.Npc.RefObjID != 51209 || manager.Npc.ObjectID != muhanNpcGIDBase || manager.Npc.TidWord&0x7fe != 0x0146 {
		t.Fatalf("manager loaded wrong: %+v", manager)
	}
	if change.World != instance.DefinitionID(104) || change.Npc.ObjectID != muhanNpcGIDBase+1 ||
		change.Npc.Spawn.RegionID != 27091 || !change.Npc.AuthoredSpawn {
		t.Fatalf("change NPC loaded wrong: %+v", change)
	}
}

/*
================
TestMuhanArenaNpcsRefuseWhatCannotStand

Off reads nothing. On refuses a monster-class row, an unknown world, a
row standing twice and a missing file before startup.
================
*/
func TestMuhanArenaNpcsRefuseWhatCannotStand(t *testing.T) {
	if npcs, err := LoadMuhanArenaNpcs(t.TempDir(), false); err != nil || npcs != nil {
		t.Fatalf("off: got %v, %v; want nothing", npcs, err)
	}
	if _, err := LoadMuhanArenaNpcs(t.TempDir(), true); err == nil {
		t.Fatal("on without the nest file loaded; want a refusal")
	}
	for name, rows := range map[string]string{
		"monster class":  muhanNestRow("NPC_MUHAN_WAVE_KIND", 103),
		"unknown world":  muhanNestRow("NPC_MUHAN_SOLO_75_MANAGER", 99),
		"stands twice":   muhanNestRow("NPC_MUHAN_SOLO_75_MANAGER", 103) + ", " + muhanNestRow("NPC_MUHAN_SOLO_75_MANAGER", 103),
		"unknown family": strings.Replace(muhanNestRow("NPC_MUHAN_SOLO_75_MANAGER", 103), "NPC_MUHAN_SOLO_75_MANAGER", "NPC_CH_SMITH", 1),
	} {
		dir := muhanTextdata(t, `{"Version": 2, "Rows": [`+rows+`]}`)
		if _, err := LoadMuhanArenaNpcs(dir, true); err == nil {
			t.Fatalf("%s: loaded; want a refusal", name)
		}
	}
}
