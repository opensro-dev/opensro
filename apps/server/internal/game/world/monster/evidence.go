/*
===========================================================================

evidence.go - the operator's server-side population evidence

The v1.150 client places every nest (npcpos) but not how it behaves: its
cap, respawn, radius, tactics and hive membership come from the server's
own nest, hive and tactics references. Those tables are the operator's,
read from their own server database and carried in the projection's
derived data (monster/...). Without them every nest keeps the
unmatched-anchor defaults and summons resolve no tactics.

The table parsers treat a bad row as a defect and panic; LoadEvidence is
the boundary where that becomes an error, since the tables are input now.

===========================================================================
*/
package monster

import (
	"fmt"

	"opensro.online/server/internal/gamedata"
)

// The evidence tables in the derived data.
const (
	PopulationEvidenceFile      = "monster/population-evidence.tsv"
	PopulationSupplementFile    = "monster/population-supplement.tsv"
	TacticsControlsFile         = "monster/tactics-controls.json"
	HiveCapsFile                = "monster/hive-caps.tsv"
	ZeroCapCapsFile             = "monster/zero-cap-caps.tsv"
	SummonTacticsFile           = "monster/summon-tactics.tsv"
	SummonTacticsSupplementFile = "monster/summon-tactics-supplement.json"
)

// evidenceFiles is the closed set: all or none.
var evidenceFiles = []string{
	PopulationEvidenceFile, PopulationSupplementFile, TacticsControlsFile, HiveCapsFile,
	ZeroCapCapsFile, SummonTacticsFile, SummonTacticsSupplementFile,
}

/*
================
Evidence

The parsed tables. A nil Evidence has none.
================
*/
type Evidence struct {
	population map[populationEvidenceKey]populationEvidence
	controls   map[populationEvidenceKey]tacticsControlPair
	hiveCaps   map[populationEvidenceKey]hiveCap
	zeroCaps   map[populationEvidenceKey]int
	summon     SummonTacticsTable
}

/*
================
LoadEvidence

The tables from derivedDir, or nil when none is there. A partial set is
refused: a nest matched without its controls or its hive would behave as
neither the evidence nor the defaults say.
================
*/
func LoadEvidence(derivedDir string) (*Evidence, error) {
	data := make(map[string][]byte, len(evidenceFiles))
	for _, name := range evidenceFiles {
		table, err := gamedata.ReadDerived(derivedDir, name)
		if err != nil {
			return nil, err
		}
		if table != nil {
			data[name] = table
		}
	}
	if len(data) == 0 {
		return nil, nil
	}
	return EvidenceFromTables(data)
}

/*
================
EvidenceFromTables

The evidence from its tables' bytes, keyed by their derived names
(PopulationEvidenceFile ...); every table of the set is required.
================
*/
func EvidenceFromTables(data map[string][]byte) (*Evidence, error) {
	for _, name := range evidenceFiles {
		if data[name] == nil {
			return nil, fmt.Errorf("monster evidence: %s is missing from the set", name)
		}
	}
	return parseEvidence(data)
}

/*
================
EvidenceFiles

The derived names of the evidence set, all or none of which must exist.
================
*/
func EvidenceFiles() []string {
	return append([]string(nil), evidenceFiles...)
}

/*
================
parseEvidence
================
*/
func parseEvidence(data map[string][]byte) (ev *Evidence, err error) {
	defer func() {
		if r := recover(); r != nil {
			ev, err = nil, fmt.Errorf("monster evidence: %v", r)
		}
	}()
	return &Evidence{
		population: combinePopulationEvidence(mustLoadPopulationEvidence(string(data[PopulationEvidenceFile])),
			mustLoadPopulationEvidence(string(data[PopulationSupplementFile]))),
		controls: loadTacticsControls(data[TacticsControlsFile]),
		hiveCaps: loadHiveCaps(string(data[HiveCapsFile])),
		zeroCaps: loadZeroCapCaps(string(data[ZeroCapCapsFile])),
		summon:   loadSummonTactics(string(data[SummonTacticsFile]), data[SummonTacticsSupplementFile]),
	}, nil
}

/*
================
SummonTactics

The default tactics of summoned and quest monsters; nil without evidence.
================
*/
func (ev *Evidence) SummonTactics() SummonTacticsTable {
	if ev == nil {
		return nil
	}
	return ev.summon
}

/*
================
LoadTemplateFrom

LoadTemplate over the evidence in derivedDir, for callers that hold only the
two folders.
================
*/
func LoadTemplateFrom(textdataDir, derivedDir string) (Template, error) {
	ev, err := LoadEvidence(derivedDir)
	if err != nil {
		return Template{}, err
	}
	return LoadTemplate(textdataDir, ev)
}
