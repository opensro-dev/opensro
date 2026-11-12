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
