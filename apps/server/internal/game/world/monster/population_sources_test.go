/*
===========================================================================

population_sources_test.go - the operator's evidence for the provenance tests

The evidence is source-data, never in the repository: each helper reads it
once per test binary and skips the test without it (or fails it under
SRO_REQUIRE_GAME_DATA=1). Tests of a single rule build their own rows.

===========================================================================
*/
package monster

import (
	"sync"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

var sourceEvidenceOnce struct {
	once     sync.Once
	evidence *Evidence
	v1188    map[populationEvidenceKey]populationEvidence
	missing  string
	err      error
}

/*
================
loadSourceEvidence

Reads and parses the set once per test binary; a test only pays for the
skip or the failure.
================
*/
func loadSourceEvidence(t testing.TB) {
	t.Helper()
	sourceEvidenceOnce.once.Do(func() {
		data := map[string][]byte{}
		for _, name := range evidenceFiles {
			table, ok, err := licensed.ReadSourceData("server/" + name)
			if err != nil {
				sourceEvidenceOnce.err = err
				return
			}
			if !ok {
				sourceEvidenceOnce.missing = name
				return
			}
			data[name] = table
		}
		sourceEvidenceOnce.evidence, sourceEvidenceOnce.err = EvidenceFromTables(data)
		if sourceEvidenceOnce.err == nil {
			sourceEvidenceOnce.v1188 = mustLoadPopulationEvidence(string(data[PopulationEvidenceFile]))
		}
	})
	if sourceEvidenceOnce.err != nil {
		t.Fatal(sourceEvidenceOnce.err)
	}
	if sourceEvidenceOnce.missing != "" {
		// Skips, or fails under SRO_REQUIRE_GAME_DATA=1, with the usual reason.
		licensed.SourceDataFile(t, "server/"+sourceEvidenceOnce.missing)
		t.Skip("monster evidence is not in source-data")
	}
}

/*
================
sourceEvidence
================
*/
func sourceEvidence(t testing.TB) *Evidence {
	t.Helper()
	loadSourceEvidence(t)
	return sourceEvidenceOnce.evidence
}

/*
================
v1188PopulationEvidence

The primary population table alone, before the supplement joins it.
================
*/
func v1188PopulationEvidence(t testing.TB) map[populationEvidenceKey]populationEvidence {
	t.Helper()
	loadSourceEvidence(t)
	return sourceEvidenceOnce.v1188
}

/*
================
populationEvidenceRows
================
*/
func populationEvidenceRows(t testing.TB) map[populationEvidenceKey]populationEvidence {
	return sourceEvidence(t).population
}

/*
================
populationTacticsControls
================
*/
func populationTacticsControls(t testing.TB) map[populationEvidenceKey]tacticsControlPair {
	return sourceEvidence(t).controls
}

/*
================
hiveCaps
================
*/
func hiveCaps(t testing.TB) map[populationEvidenceKey]hiveCap {
	return sourceEvidence(t).hiveCaps
}

/*
================
sourceTemplate

The shipped template over textdataDir with the operator's evidence.
================
*/
func sourceTemplate(t testing.TB, textdataDir string) Template {
	t.Helper()
	template, err := LoadTemplate(textdataDir, sourceEvidence(t))
	if err != nil {
		t.Fatal(err)
	}
	return template
}
