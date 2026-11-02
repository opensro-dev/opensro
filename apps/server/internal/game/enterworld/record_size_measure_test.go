/*
===========================================================================

record_size_measure_test.go - the 0x0007 record's size by character weight

#2311 item 1: the EnterWorld answer is one stream record, so a slow
link holds the whole bootstrap hostage until its last byte lands.
This measures the served record through the production composition
(NewDevDepsWithRoster with the real textdata catalogues and the
published BrowserReferences wired as the gameworld wires them) for a
fresh, a mid and a heavy character: payload bytes, blob bytes, the
length-prefixed stream record on the wire, the frame total behind it,
and the per-field byte share inside the bootstrap, so the shrink
question (item 5) is decided on numbers.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"sort"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
	"opensro.online/server/internal/transport"
)

// richRow builds one fully optioned inventory row, the heaviest shape
// a persisted row takes: plus, variance, durability and eight encoded
// magic options.
func richRow(slot int64, codename string, refObjID uint32) InventoryRow {
	options := make([]uint64, 8)
	for i := range options {
		options[i] = uint64(3000+i) | uint64(90+i)<<32
	}
	return InventoryRow{
		Slot:         slot,
		RefObjID:     refObjID,
		Codename:     codename,
		Plus:         12,
		VarianceBits: "3f9a1c44d2",
		Durability:   87,
		StackCount:   1,
		MagicOptions: options,
	}
}

// measuredCharacter builds one weight of character on the fresh
// fixture: level, a bag of rich rows, learned skills sampled from the
// real catalog, active quests and masteries.
func measuredCharacter(level int64, rows int, skills []uint32, quests int) *Character {
	character := chinaSpearman()
	character.Level = &level
	character.MaxLevel = &level
	gears := []struct {
		codename string
		ref      uint32
	}{
		{"ITEM_CH_SPEAR_01_A_DEF", 3644},
		{"ITEM_CH_BLADE_01_A_DEF", 3633},
		{"ITEM_CH_M_CLOTHES_01_BA_A_DEF", 11},
		{"ITEM_CH_M_CLOTHES_01_LA_A_DEF", 12},
	}
	for i := 0; i < rows; i++ {
		gear := gears[i%len(gears)]
		character.MissionInventory = append(character.MissionInventory, richRow(int64(i), gear.codename, gear.ref))
	}
	character.Skills = skills
	for i := 0; i < quests; i++ {
		character.ActiveQuests = append(character.ActiveQuests, domain.ActiveQuestRecord{})
	}
	character.Masteries = []domain.CharacterMastery{{ID: 200, Level: 90}, {ID: 201, Level: 90}, {ID: 202, Level: 80}}
	return character
}

/*
================
TestEnterWorldRecordSizeByWeight

Runs the three weights through HandleEnterWorld on production deps
and reports the 0x0007 record's sizes plus where the bootstrap's
bytes live.
================
*/
func TestEnterWorldRecordSizeByWeight(t *testing.T) {
	tier.Long(t, "record size by character weight on production deps")
	clientdata.RequireGameData(t)
	textdataDir := gamedatatest.TextdataDir(t)
	textdata, err := LoadTextdataCatalogs(textdataDir)
	if err != nil {
		t.Fatal(err)
	}
	paths := DevPaths{
		TextdataDir:       textdataDir,
		EquipItemsEnabled: true,
		NpcSpawns:         NpcSpawnConfigFromEnv(),
		MonsterSpawns:     MonsterSpawnConfigFromEnv(),
	}
	base, err := NewDevDepsWithRoster(paths, textdata, testRoster())
	if err != nil {
		t.Fatal(err)
	}
	references, err := NewBrowserReferences(BrowserReferenceSources{
		Skills:       base.Skills,
		ItemCommands: textdata.Items,
		StaticItems:  StaticRefItemRows(base),
		Monsters:     PublicMonsterRefObjRows(base.MonsterState),
	})
	if err != nil {
		t.Fatal(err)
	}
	base.BrowserReferences = references
	skillRows := spawnSkillSnapshot(base.Skills)
	skillIDs := make([]uint32, 0, len(skillRows))
	for _, row := range skillRows {
		skillIDs = append(skillIDs, row.ID)
	}
	take := func(n int) []uint32 {
		if n > len(skillIDs) {
			n = len(skillIDs)
		}
		return append([]uint32(nil), skillIDs[:n]...)
	}
	weights := []struct {
		name      string
		character *Character
	}{
		{"fresh", chinaSpearman()},
		{"mid", measuredCharacter(45, 30, take(120), 5)},
		{"heavy", measuredCharacter(90, 90, take(400), 20)},
	}
	for _, weight := range weights {
		deps := *base
		deps.Characters = StaticCharacterSource{DefaultDivisionID: []*Character{weight.character}}
		outcome := HandleEnterWorld(&deps, transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, weight.character.Name)))
		if !outcome.OK {
			t.Fatalf("%s: admission failed: %+v", weight.name, outcome.Result)
		}
		decoded, err := transport.DecodeEnterWorldResult(outcome.ResultPayload)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := transport.EncodeStreamFrame(transport.Frame{Opcode: transport.OpEnterWorldResult, Payload: outcome.ResultPayload})
		if err != nil {
			t.Fatal(err)
		}
		var compressed bytes.Buffer
		zip := gzip.NewWriter(&compressed)
		if _, err := zip.Write(decoded.Blob); err != nil {
			t.Fatal(err)
		}
		if err := zip.Close(); err != nil {
			t.Fatal(err)
		}
		framesBytes := 0
		for _, frame := range outcome.Frames {
			framesBytes += len(frame.Payload) + 2
		}
		t.Logf("weight=%s payload=%d blob=%d wire=%d gzip(blob)=%d nativeFrames=%d bytes=%d pctOf4MiB=%.2f pctOfMaxFrame=%.2f refObjRows=%d magicRows=%d skillRowsTotal=%d",
			weight.name, len(outcome.ResultPayload), len(decoded.Blob), len(wire), compressed.Len(),
			len(outcome.Frames), framesBytes,
			float64(len(wire))/(4<<20)*100, float64(len(wire))/transport.MaxFrameBytes*100,
			len(outcome.Result.RefObjSnapshot), len(outcome.Result.MagicOptionSnapshot), len(skillIDs))
		var envelope struct {
			V          int
			Bootstrap  map[string]json.RawMessage
			References json.RawMessage
		}
		if err := json.Unmarshal(decoded.Blob, &envelope); err != nil {
			t.Fatal(err)
		}
		type share struct {
			key  string
			size int
		}
		shares := make([]share, 0, len(envelope.Bootstrap))
		for key, raw := range envelope.Bootstrap {
			shares = append(shares, share{key, len(raw)})
		}
		sort.Slice(shares, func(i, j int) bool { return shares[i].size > shares[j].size })
		for _, s := range shares[:min(10, len(shares))] {
			t.Logf("weight=%s field=%s bytes=%d (%.1f%% of blob)", weight.name, s.key, s.size, float64(s.size)/float64(len(decoded.Blob))*100)
		}
	}
}
