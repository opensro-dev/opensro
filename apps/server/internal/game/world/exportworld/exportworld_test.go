/*
===========================================================================

exportworld_test.go - the export-world admission index's load rules.

Off never reads, an absent file admits nothing, and a malformed
index fails by name: the three legs every consumer of the index
relies on before it admits a kind or a region.
===========================================================================
*/
package exportworld

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
writeIndex
================
*/
func writeIndex(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

/*
================
TestLoadDisabledNeverReads
================
*/
func TestLoadDisabledNeverReads(t *testing.T) {
	dir := writeIndex(t, "{not json")
	index, err := Load(dir, false)
	if err != nil {
		t.Fatalf("disabled load read the file: %v", err)
	}
	if index.AdmitsRegion(24758) || index.AdmitsKind("MOB_ANY") || len(index.Areas()) != 0 {
		t.Fatalf("disabled load admitted something: %+v", index)
	}
}

/*
================
TestLoadAbsentAdmitsNothing
================
*/
func TestLoadAbsentAdmitsNothing(t *testing.T) {
	index, err := Load(t.TempDir(), true)
	if err != nil {
		t.Fatalf("absent index refused: %v", err)
	}
	if index.AdmitsRegion(1) || index.AdmitsKind("MOB_ANY") {
		t.Fatal("absent index admitted something")
	}
}

/*
================
TestLoadAdmitsWhatTheManifestNames
================
*/
func TestLoadAdmitsWhatTheManifestNames(t *testing.T) {
	dir := writeIndex(t, `{"areas": {
		"beta": {"regions": [25014], "kinds": ["NPC_EXP_TWO"]},
		"alpha": {"regions": [24758], "kinds": ["MOB_EXP_ONE"]}
	}}`)
	index, err := Load(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !index.AdmitsRegion(24758) || !index.AdmitsRegion(25014) || index.AdmitsRegion(1) {
		t.Fatal("region admission does not match the manifest")
	}
	if !index.AdmitsKind("MOB_EXP_ONE") || !index.AdmitsKind("NPC_EXP_TWO") || index.AdmitsKind("MOB_OTHER") {
		t.Fatal("kind admission does not match the manifest")
	}
	areas := index.Areas()
	if len(areas) != 2 || areas[0] != "alpha" || areas[1] != "beta" {
		t.Fatalf("areas = %v, want sorted [alpha beta]", areas)
	}
}

/*
================
TestLoadRefusesAMalformedIndex
================
*/
func TestLoadRefusesAMalformedIndex(t *testing.T) {
	for name, content := range map[string]string{
		"not json":         "{not json",
		"trailing data":    `{"areas": {}} {}`,
		"region twice":     `{"areas": {"a": {"regions": [7], "kinds": []}, "b": {"regions": [7], "kinds": []}}}`,
		"kind twice":       `{"areas": {"a": {"regions": [], "kinds": ["MOB_X"]}, "b": {"regions": [], "kinds": ["MOB_X"]}}}`,
		"region too large": `{"areas": {"a": {"regions": [65536], "kinds": []}}}`,
		"region zero":      `{"areas": {"a": {"regions": [0], "kinds": []}}}`,
		"empty codename":   `{"areas": {"a": {"regions": [], "kinds": [""]}}}`,
	} {
		if _, err := Load(writeIndex(t, content), true); err == nil {
			t.Fatalf("%s: malformed index loaded without an error", name)
		}
	}
}
