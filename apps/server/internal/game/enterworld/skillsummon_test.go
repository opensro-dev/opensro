package enterworld

import (
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"strconv"
	"testing"
)

func TestSummonTupleParserConsumesAllNineAndRejectsMalformedBody(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[66] = "80"
	fields[69] = "1936945013"
	for i := 0; i < 9; i++ {
		base := 70 + i*4
		fields[base] = strconv.Itoa(100 + i)
		fields[base+1] = "22"
		fields[base+2] = "0"
		fields[base+3] = "100"
	}
	row := encodedUniqueSummon(fields)
	if !row.Present || row.HPPercent != 80 || row.Entries[8].RefObjID != 108 || row.Entries[8].Grade != 6 || row.Entries[8].Maximum != 100 {
		t.Fatalf("tuple parse: %+v", row)
	}
	if encodedUniqueSummon(fields[:105]).Present {
		t.Fatal("truncated ninth tuple accepted")
	}
	fields[105] = "-1"
	if encodedUniqueSummon(fields).Present {
		t.Fatal("negative range accepted")
	}
	fields[105] = "0"
	fields[104] = "1"
	if encodedUniqueSummon(fields).Present {
		t.Fatal("inverted range accepted")
	}
}

func TestShippedUniqueSummonReferenceClosure(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	refs := monster.LoadMonsterRefs(dir)
	if len(refs) == 0 {
		t.Fatal("verified v1.150 projection is required")
	}
	skills := NewTextdataSkills(dir)
	template, err := WithMonsterSummonReferences(monster.TemplateFromParts(refs, nil), skills)
	if err != nil {
		t.Fatal(err)
	}
	covered := 0
	announcements := map[uint32]RefObjRow{}
	for _, row := range (MonsterSpawnConfig{Enabled: true}).MonsterRefObjSnapshot(simulation.NewMonsterState(template)) {
		announcements[row.RefObjID] = row
	}
	for _, ref := range refs {
		if ref.MonsterType != 3 {
			continue
		}
		covered++
		row, admitted := announcements[ref.RefObjID]
		if !admitted || row.Kind != "monster" || row.Name == "" || row.Name == "-" || row.NameStrID != ref.NameStrID {
			t.Fatalf("unique announcement cannot resolve its bootstrap reference: %s %+v", ref.Codename, row)
		}
		if monster.UniqueSummonPolicy(ref.Codename) == monster.NoSummonPolicy {
			t.Fatalf("unique has no reviewed policy: %s", ref.Codename)
		}
		count := 0
		for _, id := range ref.DefaultSkillIDs {
			row, ok := skills.SkillByID(id)
			if ok && row.Summon.Present {
				count++
				for _, entry := range row.Summon.Entries {
					if entry.RefObjID != 0 {
						if _, exists := refs[entry.RefObjID]; !exists {
							t.Fatalf("%s unresolved summon reference %d", ref.Codename, entry.RefObjID)
						}
					}
				}
			}
		}
		if count == 0 {
			t.Fatalf("%s lost all summon skills", ref.Codename)
		}
	}
	if covered != 19 {
		t.Fatalf("unique coverage = %d, want 19", covered)
	}
	if len(template.SpawnableRefs()) <= 19 {
		t.Fatal("summon-only children missing from initial reference mirror")
	}
	t.Logf("all %d unique rows covered; %d references in encounter closure", covered, len(template.SpawnableRefs()))
	tiger, _ := skills.SkillByID(3049)
	if !tiger.Summon.Present || tiger.Summon.Entries[0] != (monster.SummonEntry{RefObjID: 1953, Minimum: 3, Maximum: 6}) {
		t.Fatalf("Tiger Girl tuple: %+v", tiger.Summon)
	}
}
