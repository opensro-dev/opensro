/*
===========================================================================
cap110_test.go - optional population admission and native-off boundaries.
===========================================================================
*/
package monster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/alexandria"
)

/*
================
TestCap110PopulationJoinsInstalledReferences
================
*/
func TestCap110PopulationJoinsInstalledReferences(t *testing.T) {
	const id = 1234
	ref := MonsterRef{RefObjID: id, Codename: "MOB_SD_TEST", Level: 105}
	native := TemplateFromParts(map[uint32]MonsterRef{id: ref}, []NestRow{{MaxCount: 1}})
	control := TacticsControls{ObjectID: 9999, ID: 77, ChangeTarget: 2, SightRange: 130, ChampionID: 78}
	champion := control
	champion.ID, champion.ChampionID = 78, 0
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: ref.Codename,
		NestID: 9, WorldID: cap110FieldWorldID, RegionID: 0x534b, X: 100, Y: 25, Z: 200, Radius: 350, GenerateRadius: 300,
		MaxCount: 4, RespawnDelayMinSec: 8, RespawnDelayMaxSec: 10, Respawn: true,
		ChampionGenPercentage: 10, Controls: control, Champion: &champion}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := appendCap110Data(native, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(native.Nests) != 1 || len(got.Nests) != 2 {
		t.Fatal("population did not append immutably")
	}
	nest := got.Nests[1]
	if nest.RefObjID != id || nest.MaxCount != 4 || !nest.HasChampionTactics || nest.ChampionTactics.Controls.ID != 78 || nest.TargetPolicy != 2 || !nest.Aggressive {
		t.Fatalf("authored population/tactics lost: %+v", nest)
	}
	if _, err := AppendCap110(native, t.TempDir(), false); err != nil {
		t.Fatalf("off read optional data: %v", err)
	}
	t.Setenv(alexandria.EnvAlexandria, "on")
	if _, err := AppendCap110(native, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	// The nests stand in Alexandria's regions: valid data is refused while
	// the city switch is off.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cap110PopulationFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendCap110(native, dir, true); err != nil {
		t.Fatalf("on with the city: %v", err)
	}
	t.Setenv(alexandria.EnvAlexandria, "")
	if _, err := AppendCap110(native, dir, true); err == nil || !strings.Contains(err.Error(), alexandria.EnvAlexandria) {
		t.Fatalf("on without the city admitted: %v", err)
	}
	source.Rows = append(source.Rows, source.Rows[0])
	data, err = json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendCap110Data(native, data); err == nil {
		t.Fatal("duplicate source nest admitted")
	}
}

/*
================
TestCap110Level100SliceFollowsItsFlag

#1741: with SRO_CAP110_LEVEL100 off, the level-100 rows are held back
(validated, not joined) while the rest of the population loads; on,
they join. A reference outside the 100-110 window is a bad row in
both settings, and duplicate nest ids fail across the held-back slice.
================
*/
func TestCap110Level100SliceFollowsItsFlag(t *testing.T) {
	at100 := MonsterRef{RefObjID: 2001, Codename: "MOB_SD_AT100", Level: 100}
	at105 := MonsterRef{RefObjID: 2002, Codename: "MOB_SD_AT105", Level: 105}
	template := TemplateFromParts(map[uint32]MonsterRef{at100.RefObjID: at100, at105.RefObjID: at105}, nil)
	row := func(ref MonsterRef, nestID uint32) cap110Nest {
		return cap110Nest{Codename: ref.Codename, NestID: nestID, WorldID: cap110FieldWorldID, RegionID: 0x534b, X: 100, Y: 25, Z: 200,
			Radius: 350, GenerateRadius: 300, MaxCount: 4, RespawnDelayMinSec: 8, RespawnDelayMaxSec: 10, Respawn: true,
			Controls: TacticsControls{ObjectID: 9999, ID: 77, ChangeTarget: 2, SightRange: 130}}
	}
	marshal := func(rows []cap110Nest) []byte {
		data, err := json.Marshal(struct {
			Version int
			Rows    []cap110Nest
		}{cap110PopulationVersion, rows})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	pair := marshal([]cap110Nest{row(at100, 9), row(at105, 10)})

	t.Setenv(EnvCap110Level100, "")
	got, err := appendCap110Data(template, pair)
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	if len(got.Nests) != 1 || got.Nests[0].RefObjID != at105.RefObjID {
		t.Fatalf("off joined %+v, want only the level-105 nest", got.Nests)
	}

	t.Setenv(EnvCap110Level100, "on")
	got, err = appendCap110Data(template, pair)
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	if len(got.Nests) != 2 {
		t.Fatalf("on joined %d nests, want both", len(got.Nests))
	}

	// A reference outside the window is a bad row in both settings.
	at99 := MonsterRef{RefObjID: 2003, Codename: "MOB_SD_AT99", Level: 99}
	below := TemplateFromParts(map[uint32]MonsterRef{at99.RefObjID: at99}, nil)
	bad := marshal([]cap110Nest{row(at99, 11)})
	if _, err := appendCap110Data(below, bad); err == nil {
		t.Fatal("level-99 row admitted with the flag on")
	}
	t.Setenv(EnvCap110Level100, "")
	if _, err := appendCap110Data(below, bad); err == nil {
		t.Fatal("level-99 row admitted with the flag off")
	}

	// Duplicate nest ids fail even across the held-back slice.
	dup := marshal([]cap110Nest{row(at100, 9), row(at105, 9)})
	if _, err := appendCap110Data(template, dup); err == nil {
		t.Fatal("duplicate nest id admitted across the held-back level-100 row")
	}

	// A field row naming another world is refused: the field joins the
	// default world only (#1750).
	foreign := row(at105, 12)
	foreign.WorldID = 57
	if _, err := appendCap110Data(template, marshal([]cap110Nest{foreign})); err == nil {
		t.Fatal("field row in a tomb world admitted")
	}
}

/*
================
TestCap110PopulationRejectsMalformedData
================
*/
func TestCap110PopulationRejectsMalformedData(t *testing.T) {
	for _, data := range []string{`{"Version":3,"Rows":[]}`, `{"Version":1,"Rows":[],"Unknown":1}`, `{"Version":1,"Rows":[]} {}`} {
		if _, err := appendCap110Data(Template{}, []byte(data)); err == nil {
			t.Fatal("malformed optional data admitted")
		}
	}
}

/*
================
TestCap110ApisKeepsSourceGradeAndRespawn
================
*/
func TestCap110ApisKeepsSourceGradeAndRespawn(t *testing.T) {
	ref := MonsterRef{RefObjID: 1234, Codename: "MOB_SD_APIS", Level: 103, MonsterType: 8, MaxHP: 21068995}
	template := TemplateFromParts(map[uint32]MonsterRef{ref.RefObjID: ref}, nil)
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: ref.Codename, NestID: 29205, WorldID: cap110FieldWorldID, RegionID: 19017,
		X: 1578.03, Y: 184, Z: 1780.5601, Radius: 600, GenerateRadius: 400, MaxCount: 1,
		Respawn: true, RespawnDelayMinSec: 10800, RespawnDelayMaxSec: 21600,
		Controls: TacticsControls{ChangeTarget: 2, SightRange: 130}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := appendCap110Data(template, data)
	if err != nil {
		t.Fatal(err)
	}
	nest := got.Nests[0]
	if got.Refs[ref.RefObjID].MonsterType != 8 || nest.MaxCount != 1 || nest.HasChampionTactics ||
		nest.RespawnDelayMinSec != 10800 || nest.RespawnDelayMaxSec != 21600 {
		t.Fatalf("Apis source values lost: %+v", nest)
	}
	ref.Codename = "MOB_SD_OTHER_UNIQUE"
	source.Rows[0].Codename = ref.Codename
	data, err = json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendCap110Data(TemplateFromParts(map[uint32]MonsterRef{ref.RefObjID: ref}, nil), data); err == nil {
		t.Fatal("unselected special monster admitted")
	}
}
