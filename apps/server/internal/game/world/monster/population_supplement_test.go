package monster

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

func TestPopulationSupplementPreservesExistingPolicies(t *testing.T) {
	supplement := mustLoadPopulationEvidence(string(licensed.SourceDataFile(t, "server/"+PopulationSupplementFile)))
	if len(supplement) != 4229 {
		t.Fatalf("supplement has %d anchors, want 4229", len(supplement))
	}
	for key, original := range v1188PopulationEvidence(t) {
		if populationEvidenceRows(t)[key] != original {
			t.Fatalf("existing policy changed: %+v", key)
		}
	}
	counts := map[int]bool{}
	for key, row := range supplement {
		if _, exists := v1188PopulationEvidence(t)[key]; exists {
			t.Fatalf("supplement overlaps original: %+v", key)
		}
		counts[row.MaxCount] = true
	}
	if len(counts) < 2 {
		t.Fatal("supplement flattened authored population variation")
	}
}

// The later data's tactics 190 (Movia nest) is passive and names champion
// tactics 189, which is aggressive with sight 150. Promoted Movia champions
// and giants must therefore hunt while ordinary Movia stay passive.
func TestMoviaNestCarriesAggressiveChampionTactics(t *testing.T) {
	row, ok := populationEvidenceRows(t)[evidenceKey("MOB_EU_MOVOI", 27212, 539.909973, 0, 804.669983)]
	if !ok {
		t.Fatal("missing Movia population evidence")
	}
	if row.Aggressive || row.SightRange != 100 || row.ChampionGenPercentage != 10 {
		t.Fatalf("ordinary Movia tactics = %+v, want passive sight 100", row)
	}
	if !row.HasChampion || row.Champion != (ChampionTactics{Aggressive: true, SightRange: 150}) {
		t.Fatalf("Movia champion tactics = %+v / %+v, want aggressive sight 150", row.HasChampion, row.Champion)
	}
}

func TestPopulationEvidenceRejectsChampionFieldsWithoutChampionRow(t *testing.T) {
	const prefix = "MOB_TEST\t1\t0\t0\t0\t10\t10\t10\t1\t2\t1\t1\t0\t100\t0\t0\t"
	const champion = "\t1\t150\t0\t0\t16384\n"
	rows := mustLoadPopulationEvidence(prefix + "1" + champion)
	if row := rows[evidenceKey("MOB_TEST", 1, 0, 0, 0)]; !row.HasChampion || row.InitialDir != 16384 {
		t.Fatalf("champion control row = %+v", row)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("champion fields admitted without champion tactics")
		}
	}()
	mustLoadPopulationEvidence(prefix + "0" + champion)
}

func TestPopulationSupplementRejectsOverwrite(t *testing.T) {
	key := populationEvidenceKey{Codename: "MOB_TEST"}
	base := map[populationEvidenceKey]populationEvidence{key: {MaxCount: 1}}
	defer func() {
		if recover() == nil {
			t.Fatal("overlapping supplement admitted")
		}
		if base[key].MaxCount != 1 {
			t.Fatal("base map was mutated")
		}
	}()
	combinePopulationEvidence(base, map[populationEvidenceKey]populationEvidence{key: {MaxCount: 5}})
}
