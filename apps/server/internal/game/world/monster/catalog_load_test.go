/*
===========================================================================

catalog_load_test.go - loading the shipped monster tables

Monster references are classified from characterdata and joined with the
character-info ride contract, dungeon spawn points parse, and the shipped
template stays pinned (the canary).

===========================================================================
*/
package monster_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestLoadMonsterRefsClassification
================
*/
// The classification filter is the arbiter-confirmed binary admit set
// (RZ seq234, closing the A8 dispute): rows with (TID1=1, TID2=2, TID3=1)
// pack to 0x00C6 and are monsters; TID3=2 is the NPC subtype; col12/TID4
// is NOT consumed by the gates, so a quest-clone row differing only in
// col12 IS a monster.
func TestLoadMonsterRefsClassification(t *testing.T) {
	dir := t.TempDir()
	rows := "" +
		// 120-column rows are not required by the loader; pad to the
		// consumed range. Column layout: 0 service, 1 id, 2 codename,
		// 8 charBit, 9-11 TID1-3, 12 unused-by-gates, 46/47/48
		// walk/run/scale, and 50 BCRadius.
		row(1, 1933, "MOB_CH_MANGNYANG", 1, 2, 1, 1, "8", "22", "100") +
		row(1, 5555, "MOB_QT_CLONE", 1, 2, 1, 0, "8", "22", "100") + // col12=0: STILL a monster (seq234)
		row(1, 7495, "NPC_EU_SMITH", 1, 2, 2, 0, "0", "0", "100") + // TID3=2: NPC
		row(0, 1934, "MOB_DISABLED", 1, 2, 1, 1, "8", "22", "100") + // service=0
		row(1, 6001, "MOB_BAD_WALK", 1, 2, 1, 1, "NaN", "22", "100") +
		row(1, 6002, "MOB_BAD_RUN", 1, 2, 1, 1, "8", "-1", "100") +
		row(1, 6003, "MOB_BAD_SCALE", 1, 2, 1, 1, "8", "22", "0") +
		rowWithMetadata(6004, "MOB_BAD_LEVEL", "256", "54") +
		rowWithMetadata(6005, "MOB_BAD_HP", "1", "not-a-number")
	if err := os.WriteFile(filepath.Join(dir, "characterdata_test.txt"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "textdataname.txt"),
		[]byte("1\tSN_MOB_CH_MANGNYANG\t0\t0\t0\t0\t0\t0\tMangyang\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	refs := LoadMonsterRefs(dir)
	if len(refs) != 2 {
		t.Fatalf("classified %d rows as monsters, want 2 (1933 + the col12=0 clone) - got %v", len(refs), refs)
	}
	if _, ok := refs[5555]; !ok {
		t.Fatal("col12=0 row excluded - the loader is still applying the retracted seq42 TID4 gate")
	}
	mangnyang, ok := refs[1933]
	if !ok {
		t.Fatal("1933 missing from the monster set")
	}
	if mangnyang.TidWord != 0x00C6 {
		t.Fatalf("TidWord = %#04x, want 0x00C6", mangnyang.TidWord)
	}
	if mangnyang.WalkSpeed != 8 || mangnyang.RunSpeed != 22 || mangnyang.ScaleDenom != 100 || mangnyang.BodyRadius != 2 {
		t.Fatalf("movement/contact data = %v/%v/%v radius %v, want 8/22/100 radius 2", mangnyang.WalkSpeed, mangnyang.RunSpeed, mangnyang.ScaleDenom, mangnyang.BodyRadius)
	}
	if mangnyang.MonsterType != 0 {
		t.Fatalf("Mangnyang monster type = %d, want ordinary type 0", mangnyang.MonsterType)
	}
	if mangnyang.Country != 0 {
		t.Fatalf("Mangnyang country = %d, want China bucket 0", mangnyang.Country)
	}
	if mangnyang.NameStrID != "SN_MOB_CH_MANGNYANG" || mangnyang.Name != "Mangyang" {
		t.Fatalf("name = %q -> %q, want SN_MOB_CH_MANGNYANG -> Mangyang", mangnyang.NameStrID, mangnyang.Name)
	}
	if mangnyang.Level != 1 || mangnyang.MaxHP != 54 ||
		mangnyang.ModelPath != `mob\china\mangnyang.bsr` {
		t.Fatalf("snapshot metadata = level %d hp %d model %q", mangnyang.Level, mangnyang.MaxHP, mangnyang.ModelPath)
	}
}

/*
================
TestLoadMonsterRefsJoinsCharacterInfoRideContract
================
*/
func TestLoadMonsterRefsJoinsCharacterInfoRideContract(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "characterdata_test.txt"),
		[]byte(row(1, 1954, "MOB_CH_TIGERWOMAN", 1, 2, 1, 1, "8", "22", "100")),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Retail skilleffect.txt is tabular even for its section directives. This
	// exact header shape previously made the server discard every ride row.
	const skillEffect = "#section\tcharacterInfo\n" +
		"MOB_CH_TIGERWOMAN\tMOB_TIGERWOMAN\t2.8\tnone\tres\\mob\\china\\bluetiger.bsr\n" +
		"MOB_FIXED\tMOB_FIXED\t1\tRT_FIXED\tres\\mob\\fixed.bsr\n" +
		"MOB_DUMMY\tMOB_DUMMY\t1\tRT_DUMMY\tres\\mob\\dummy.bsr\n" +
		"MOB_UNKNOWN\tMOB_UNKNOWN\t1\tRT_GUESSED\tres\\mob\\guessed.bsr\n" +
		"#section\tskillInfo\n"
	if err := os.WriteFile(filepath.Join(dir, "skilleffect.txt"), []byte(skillEffect), 0o644); err != nil {
		t.Fatal(err)
	}

	tigerGirl, ok := LoadMonsterRefs(dir)[1954]
	if !ok {
		t.Fatal("Tiger Girl ref missing")
	}
	if tigerGirl.RideModelPath != `res\mob\china\bluetiger.bsr` || tigerGirl.RiderTransformMode != 0 {
		t.Fatalf("joined Tiger Girl ride contract = %+v", tigerGirl)
	}
}

/*
================
TestLoadSpawnPointsParsesDungeonRegions
================
*/
// npcpos rows parse both mainland (unsigned) and dungeon (signed
// 0x8000-bit) region ids; the signed form silently dropped every dungeon
// spawn before the seq186 canary red.
func TestLoadSpawnPointsParsesDungeonRegions(t *testing.T) {
	dir := t.TempDir()
	rows := "1933\t25258\t812.68\t75.08\t392.90\n" +
		"1933\t-32767\t100.5\t-2.0\t200.5\n" + // uint16 0x8001, dungeon-sector bit set
		"1933\tnotanumber\t1\t2\t3\n" // unparseable region: skipped, not fatal
	if err := os.WriteFile(filepath.Join(dir, "npcpos.txt"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}

	points := LoadSpawnPoints(dir)
	if len(points) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(points))
	}
	if points[0].RegionID != 25258 {
		t.Fatalf("mainland region = %d, want 25258", points[0].RegionID)
	}
	if points[1].RegionID != 0x8001 {
		t.Fatalf("dungeon region = %#04x, want 0x8001 (sign-bit form of -32767)", points[1].RegionID)
	}
}

/*
================
row
================
*/
// row builds one synthetic characterdata line with the consumed columns
// placed at their real indices.
func row(service int, id int, codename string, tid1, tid2, tid3, col12 int, walk, run, scale string) string {
	cols := make([]string, 60)
	for i := range cols {
		cols[i] = "0"
	}
	cols[0] = itoa(service)
	cols[1] = itoa(id)
	cols[2] = codename
	cols[5] = "SN_" + codename
	cols[8] = "1" // char/bionic bit (every characterdata row ships 1)
	cols[9], cols[10], cols[11], cols[12] = itoa(tid1), itoa(tid2), itoa(tid3), itoa(col12)
	cols[46], cols[47], cols[48] = walk, run, scale
	cols[50] = "2"
	cols[52] = `mob\china\mangnyang.bsr`
	cols[57] = "1"
	cols[59] = "54"
	line := cols[0]
	for _, c := range cols[1:] {
		line += "\t" + c
	}
	return line + "\n"
}

/*
================
rowWithMetadata
================
*/
func rowWithMetadata(id int, codename, level, maxHP string) string {
	line := row(1, id, codename, 1, 2, 1, 1, "8", "22", "100")
	cols := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
	cols[57] = level
	cols[59] = maxHP
	return strings.Join(cols, "\t") + "\n"
}

/*
================
itoa
================
*/
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}

/*
================
TestShippedTemplateCanary
================
*/
// Canary against the REAL shipped v1.150 textdata (the leveldata canary
// posture): the counts under the ARBITER-CONFIRMED classification (RZ
// seq234) are pinned exactly, so a media re-extraction or a packing
// regression fails HERE, loudly, instead of silently reshaping the
// population.
func TestShippedTemplateCanary(t *testing.T) {
	dir := ""
	for _, candidate := range []string{
		licensed.RetailTextdataDir(t),
	} {
		if _, err := os.Stat(filepath.Join(candidate, "npcpos.txt")); err == nil {
			dir = candidate
			break
		}
	}
	if dir == "" {
		t.Skip("shipped textdata not present in this checkout")
	}

	template := LoadTemplate(dir)
	structures := 0
	for _, ref := range template.Refs {
		if ref.Structure {
			structures++
		}
	}
	if got := len(template.Refs) - structures; got != 5986 {
		t.Fatalf("monster refs = %d, want 5986 (RZ seq234 admit set over shipped characterdata)", got)
	}
	// The fortress structures (TID 1/2/5) with hit points or a placeholder.
	if structures != 52 {
		t.Fatalf("structure refs = %d, want 52", structures)
	}
	if got := len(template.Nests); got != 8753 {
		t.Fatalf("monster nest rows = %d, want 8753 (shipped npcpos)", got)
	}
	if got := template.EvidenceMatches; got != 8458 {
		t.Fatalf("combined population matches = %d, want 8458 native-float natural-key joins after the monster classifier", got)
	}
	for _, nest := range template.Nests {
		if nest.RetailEvidence && (!nest.HasControls || (nest.HasChampionTactics && !nest.ChampionTactics.HasControls)) {
			t.Fatalf("source-backed nest lost complete tactics: %+v", nest.SpawnPoint)
		}
	}
	// Every shipped unique anchor must carry the shared hive, including the
	// three half-tenth decimal spellings that formerly produced extra uniques.
	families := map[uint32][]NestRow{}
	for _, nest := range template.Nests {
		if template.Refs[nest.RefObjID].MonsterType&15 == 3 {
			families[nest.RefObjID] = append(families[nest.RefObjID], nest)
		}
	}
	for ref, nests := range families {
		if len(nests) == 0 {
			continue
		}
		for _, nest := range nests {
			if !nest.RetailEvidence || nest.HiveMaxCount != 1 || nest.HiveKey != nests[0].HiveKey {
				t.Fatalf("unique %d escaped shared policy: %+v", ref, nest)
			}
		}
		state := simulation.NewMonsterState(TemplateFromParts(template.Refs, nests))
		now := time.Unix(100, 0)
		state.SetTimeSource(func() time.Time { return now })
		state.StartDivision("audit")
		// Authored unique delays can be hours. Cross-region observation must
		// not bypass them; advance the world clock beyond every initial delay.
		now = now.Add(7 * 24 * time.Hour)
		for _, nest := range nests {
			state.StartDivision("audit")
			state.AdvancePopulation(state.CurrentTimeMillis())
			state.InstancesInRegions("audit", []uint16{nest.RegionID})
			now = now.Add(5 * time.Minute)
		}
		if got := len(state.MaterializedInstances("audit")); got != 1 {
			t.Fatalf("unique %d crossing created %d occupants", ref, got)
		}
		if got := len(state.DrainUniqueNotices("audit")); got != 1 {
			t.Fatalf("unique %d crossing emitted %d appearances", ref, got)
		}
	}
	// 178 includes MOB_DH_SOLDIEREARTHGHOST and its clone: the v1.150 client
	// places them and QNO_WC_SOLDIER_EA2_1 needs 1,600 kills, while the v1.188
	// shard backup caps every one of their nests at zero (laterDisabledCodenames).
	if got := len(template.SpawnableRefs()); got != 178 {
		t.Fatalf("spawnable refs = %d, want 178", got)
	}
	for _, codename := range []string{"MOB_DH_SOLDIEREARTHGHOST", "MOB_DH_SOLDIEREARTHGHOST_CLON"} {
		found := false
		for _, ref := range template.SpawnableRefs() {
			found = found || ref.Codename == codename
		}
		if !found {
			t.Fatalf("%s is a v1.150 quest target and must be spawnable", codename)
		}
	}
	monsterTypes := map[uint8]int{}
	for _, ref := range template.Refs {
		if !ref.Structure {
			monsterTypes[ref.MonsterType]++
		}
	}
	if monsterTypes[0] != 5967 || monsterTypes[3] != 19 || len(monsterTypes) != 2 {
		t.Fatalf("monster type distribution = %v, want 5967 ordinary / 19 unique", monsterTypes)
	}
	mangnyang, ok := template.Refs[1933]
	if !ok || mangnyang.Codename != "MOB_CH_MANGNYANG" || mangnyang.TidWord != 0x00C6 {
		t.Fatalf("1933 = %+v, want MOB_CH_MANGNYANG with TidWord 0x00C6", mangnyang)
	}
	if mangnyang.WalkSpeed != 8 || mangnyang.RunSpeed != 22 {
		t.Fatalf("1933 speeds = %v/%v, want 8/22", mangnyang.WalkSpeed, mangnyang.RunSpeed)
	}
	if mangnyang.NameStrID != "SN_MOB_CH_MANGNYANG" || mangnyang.Name != "Mangyang" ||
		mangnyang.Level != 1 || mangnyang.MaxHP != 54 || mangnyang.Country != 0 ||
		mangnyang.ModelPath != `mob\china\mangnyang.bsr` {
		t.Fatalf("1933 retail metadata = %+v", mangnyang)
	}
	scaleCanary, ok := template.Refs[7550]
	if !ok || scaleCanary.ScaleDenom != 135 {
		t.Fatalf("7550 = %+v, want shipped non-default scale denominator 135", scaleCanary)
	}
	// Dungeon spawns must be present (the seq186 red): the shipped file
	// carries 172 monster rows in sign-bit dungeon regions.
	dungeonRows := 0
	for _, nest := range template.Nests {
		if nest.RegionID&0x8000 != 0 {
			dungeonRows++
		}
	}
	if dungeonRows != 172 {
		t.Fatalf("dungeon-region nest rows = %d, want 172", dungeonRows)
	}
}
