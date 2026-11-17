/*
===========================================================================

roc_test.go - the optional Roc population's admission rules.

The committed evidence (scripts/data/roc/quest-nests.json) is the file
the Roc import writes as roc-monsters.txt, so the first test loads it
as the server will.

===========================================================================
*/
package monster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// rocEvidencePath is the committed evidence, from this package's folder.
var rocEvidencePath = filepath.Join("..", "..", "..", "..", "..", "..", "scripts", "data", "roc", "quest-nests.json")

/*
================
rocTemplate

A template holding the two quest kinds at the given level.
================
*/
func rocTemplate(level uint8) Template {
	refs := map[uint32]MonsterRef{
		24010: {RefObjID: 24010, Codename: "MOB_QT_RM_FEATHERMASK_01", Level: level},
		24011: {RefObjID: 24011, Codename: "MOB_QT_RM_ROCKY_CLON_01", Level: level},
	}
	return TemplateFromParts(refs, []NestRow{{MaxCount: 1}})
}

/*
================
rocPopulation

The population the Roc import stages: the committed evidence's.
================
*/
func rocPopulation(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(rocEvidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Population json.RawMessage `json:"population"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document.Population
}

/*
================
TestRocAdmitsTheCommittedQuestNests

The six v1.188 nests join the default world with their shared hive cap:
one key and a limit of 20 across all six.
================
*/
func TestRocAdmitsTheCommittedQuestNests(t *testing.T) {
	template := rocTemplate(rocQuestKindLevel)
	got, err := appendRocData(template, rocPopulation(t))
	if err != nil {
		t.Fatalf("committed population refused: %v", err)
	}
	added := got.Nests[len(template.Nests):]
	if len(added) != 6 {
		t.Fatalf("appended %d nests, want 6", len(added))
	}
	for _, nest := range added {
		if nest.HiveKey != added[0].HiveKey || nest.HiveKey == "" || nest.HiveMaxCount != 20 {
			t.Fatalf("nest in region %d hive %q/%d, want one shared key capped at 20", nest.RegionID, nest.HiveKey, nest.HiveMaxCount)
		}
	}
}

/*
================
TestRocRefusesOtherKindsAndLevels

A kind outside the chain's two, or one at another level than the client
authors, refuses the whole file.
================
*/
func TestRocRefusesOtherKindsAndLevels(t *testing.T) {
	if _, err := appendRocData(rocTemplate(rocQuestKindLevel+1), rocPopulation(t)); err == nil {
		t.Fatal("quest kinds at another level admitted")
	}
	template := tombTemplate("MOB_RM_ROC", rocQuestKindLevel, 3)
	if _, err := appendRocData(template, skytempleSource(t, "MOB_RM_ROC", 24690)); err == nil {
		t.Fatal("a kind outside the chain admitted")
	}
}

/*
================
TestRocOffAndOn

Off never reads the optional file; on refuses missing input. A valid
file appends.
================
*/
func TestRocOffAndOn(t *testing.T) {
	template := rocTemplate(rocQuestKindLevel)
	if _, err := AppendRoc(template, t.TempDir(), false); err != nil {
		t.Fatalf("off read optional data: %v", err)
	}
	if _, err := AppendRoc(template, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, rocPopulationFile), rocPopulation(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := AppendRoc(template, dir, true); err != nil || len(got.Nests) != 7 {
		t.Fatalf("installed population refused: %v", err)
	}
}

/*
================
TestRocFlagWords
================
*/
func TestRocFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvRoc, word)
		if got := RocEnabled(); got != want {
			t.Fatalf("%q = %t, want %t", word, got, want)
		}
	}
}
