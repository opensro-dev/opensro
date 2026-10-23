/*
===========================================================================

evidence_load_test.go - the evidence boundary: all of the set or none

===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
syntheticSupplement

The sixteen supplement rows the closure demands, as the table's JSON
holds them: one normal control (which names its champion row) and one
champion control each.
================
*/
func syntheticSupplement() string {
	var text strings.Builder
	text.WriteString(`{"Rows":[`)
	for i := 0; i < 16; i++ {
		if i > 0 {
			text.WriteString(",")
		}
		fmt.Fprintf(&text, `{"Codename":"SUPP_%02d","PrimaryRefID":1,`+
			`"Normal":[{"ID":%d,"ObjectID":1,"SightRange":10,"ChampionID":%d,"HasAggroType":true}],`+
			`"Champion":[{"ID":%d,"ObjectID":1,"SightRange":10,"HasAggroType":true}],`+
			`"ObjectSourceOffset":1,"TacticsSourceOffsets":[1,2]}`,
			i, 100+i, 200+i, 200+i)
	}
	text.WriteString(`]}`)
	return text.String()
}

/*
================
writeEvidenceSet

The evidence files under dir, each holding bytes the parsers accept;
overrides replace a file's content, and a name in skip is not written.
================
*/
func writeEvidenceSet(t *testing.T, dir string, overrides map[string]string, skip map[string]bool) {
	t.Helper()
	content := map[string]string{
		PopulationEvidenceFile:      "",
		PopulationSupplementFile:    "",
		TacticsControlsFile:         "{}",
		HiveCapsFile:                "",
		ZeroCapCapsFile:             "",
		SummonTacticsFile:           "",
		SummonTacticsSupplementFile: syntheticSupplement(),
	}
	for name, text := range overrides {
		content[name] = text
	}
	for _, name := range EvidenceFiles() {
		if skip[name] {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content[name]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

/*
================
TestLoadEvidenceSetContract

No table at all loads nil evidence; a partial set is refused whole,
never half-loaded; the full set parses; a row the population parser
would panic on comes back as an error at this boundary.
================
*/
func TestLoadEvidenceSetContract(t *testing.T) {
	if ev, err := LoadEvidence(t.TempDir()); ev != nil || err != nil {
		t.Fatalf("absent set: %v, %v", ev, err)
	}

	dir := t.TempDir()
	writeEvidenceSet(t, dir, nil, map[string]bool{HiveCapsFile: true})
	if ev, err := LoadEvidence(dir); ev != nil || err == nil || !strings.Contains(err.Error(), "missing from the set") {
		t.Fatalf("partial set admitted: %v, %v", ev, err)
	}

	dir = t.TempDir()
	writeEvidenceSet(t, dir, nil, nil)
	ev, err := LoadEvidence(dir)
	if err != nil || ev == nil {
		t.Fatalf("complete set refused: %v, %v", ev, err)
	}
	if got := len(ev.SummonTactics()); got != 16 {
		t.Fatalf("supplement rows loaded: %d, want 16", got)
	}

	dir = t.TempDir()
	writeEvidenceSet(t, dir, map[string]string{PopulationEvidenceFile: "MOB_X\t1\n"}, nil)
	if ev, err := LoadEvidence(dir); ev != nil || err == nil || !strings.Contains(err.Error(), "monster evidence") {
		t.Fatalf("malformed set not refused as an error: %v, %v", ev, err)
	}
}
