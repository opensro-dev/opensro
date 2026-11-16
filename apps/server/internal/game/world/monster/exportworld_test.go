/*
===========================================================================

exportworld_test.go - the export-world population's admission.

The index decides: a nest joins only when its kind and its region
are both admitted by a packed area, and everything else - the file
absent, the switch off - leaves the template exactly as it was.
===========================================================================
*/
package monster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const exportWorldRegion = 24758

/*
================
writeExportWorld
================
*/
func writeExportWorld(t *testing.T, manifest string, field []byte) string {
	t.Helper()
	dir := t.TempDir()
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "export-world.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if field != nil {
		if err := os.WriteFile(filepath.Join(dir, exportWorldFieldFile), field, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

/*
================
exportWorldSource

One export-world nest row on the default world, encoded as an area
import writes it.
================
*/
func exportWorldSource(t *testing.T, codename string, regionID uint16) []byte {
	t.Helper()
	source := struct {
		Version int
		Rows    []cap110Nest
	}{cap110PopulationVersion, []cap110Nest{{Codename: codename, NestID: 43001, WorldID: 1, RegionID: regionID,
		X: 100, Y: 0, Z: 200, Radius: 0, GenerateRadius: 0, MaxCount: 1,
		Respawn: true, RespawnDelayMinSec: 1, RespawnDelayMaxSec: 3,
		Controls: TacticsControls{SightRange: 100}}}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
TestAppendExportWorldOffAndAbsent
================
*/
func TestAppendExportWorldOffAndAbsent(t *testing.T) {
	template := tombTemplate("MOB_EXP_ONE", 100, 0)
	dir := writeExportWorld(t, "{not json", []byte("{not json"))
	got, err := AppendExportWorld(template, dir, false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("off read the files: %v", err)
	}
	got, err = AppendExportWorld(template, t.TempDir(), true)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("on with no files changed the template: %v", err)
	}
}

/*
================
TestAppendExportWorldAdmitsOnlyTheIndex
================
*/
func TestAppendExportWorldAdmitsOnlyTheIndex(t *testing.T) {
	manifest := `{"areas": {"alpha": {"regions": [24758], "kinds": ["MOB_EXP_ONE"]}}}`
	template := tombTemplate("MOB_EXP_ONE", 100, 0)

	dir := writeExportWorld(t, manifest, exportWorldSource(t, "MOB_EXP_ONE", exportWorldRegion))
	got, err := AppendExportWorld(template, dir, true)
	if err != nil {
		t.Fatalf("admitted nest refused: %v", err)
	}
	if len(got.Nests) != len(template.Nests)+1 {
		t.Fatalf("population did not append: %d nests", len(got.Nests))
	}

	dir = writeExportWorld(t, manifest, exportWorldSource(t, "MOB_EXP_ONE", 25000))
	if _, err := AppendExportWorld(template, dir, true); err == nil {
		t.Fatal("a nest outside the admitted regions was appended")
	}

	dir = writeExportWorld(t, manifest, exportWorldSource(t, "MOB_EXP_STRANGER", exportWorldRegion))
	if _, err := AppendExportWorld(template, dir, true); err == nil {
		t.Fatal("a nest of an unknown kind was appended")
	}

	// A kind the template knows, in an admitted region, but outside the
	// area's kinds: only the index's kind half can refuse it (the unknown
	// kind above stops at the reference lookup before admission runs).
	known := TemplateFromParts(map[uint32]MonsterRef{
		4321: {RefObjID: 4321, Codename: "MOB_EXP_ONE", Level: 100},
		4322: {RefObjID: 4322, Codename: "MOB_EXP_TWO", Level: 100},
	}, []NestRow{{MaxCount: 1}})
	dir = writeExportWorld(t, manifest, exportWorldSource(t, "MOB_EXP_TWO", exportWorldRegion))
	if _, err := AppendExportWorld(known, dir, true); err == nil {
		t.Fatal("a known kind outside the area's kinds was appended")
	}

	dir = writeExportWorld(t, "{not json", nil)
	if _, err := AppendExportWorld(template, dir, true); err == nil {
		t.Fatal("a malformed index was read without an error")
	}
}
