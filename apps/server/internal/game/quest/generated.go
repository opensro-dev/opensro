/*
===========================================================================

generated.go - composition of curated and generated quest definitions

The generated catalogues are data, not executable scripts, and come from the
projection's derived data (the operator's source-data), never from the
repository. Every row passes the same version-aware loader and reference
validation as curated native handlers. Without them only the curated quests
load. Research directories are never runtime dependencies.

===========================================================================
*/
package quest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/gamedata"
)

// The quest catalogues in the projection's derived data:
//   - generated: quests projected from the recovered v1.150 quest scripts;
//   - achieved-now: codename -> ACHIEVED_NOW symbol from the same scripts;
//   - compiled: scripts/build/generate_compiled_quests.py, the quests whose
//     v1.150 script did not survive.
const (
	GeneratedCatalogFile   = "quest/catalog_generated.json"
	AchievedNowCatalogFile = "quest/achieved_now_generated.json"
	CompiledCatalogFile    = "quest/compiled_quests.json"
)

// CatalogFiles are the derived quest catalogues, for loaders and tests.
var CatalogFiles = [3]string{GeneratedCatalogFile, AchievedNowCatalogFile, CompiledCatalogFile}

/*
================
MissingCatalogFiles

The derived quest catalogues absent from derivedDir, for the startup
warning: without them only the curated quests load.
================
*/
func MissingCatalogFiles(derivedDir string) ([]string, error) {
	var missing []string
	for _, name := range CatalogFiles {
		data, err := gamedata.ReadDerived(derivedDir, name)
		if err != nil {
			return nil, err
		}
		if data == nil {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

/*
================
readCatalogFile

One derived quest catalogue, or an empty JSON value when it is absent.
================
*/
func readCatalogFile(catalog *Catalog, name, empty string) ([]byte, error) {
	data, err := gamedata.ReadDerived(catalog.derivedDir, name)
	if err != nil || data != nil {
		return data, err
	}
	return []byte(empty), nil
}

/*
================
catalogSpecs

Small protocol fixtures contain only their authored subset. Production's
complete media catalog admits every corresponding compiled contract.
================
*/
func catalogSpecs(catalog *Catalog) ([]QuestSpec, error) {
	specs := append([]QuestSpec(nil), curatedQuestSpecs...)
	generatedCatalog, err := readCatalogFile(catalog, GeneratedCatalogFile, "[]")
	if err != nil {
		return nil, err
	}
	compiledCatalog, err := readCatalogFile(catalog, CompiledCatalogFile, "[]")
	if err != nil {
		return nil, err
	}
	achievedNowCatalog, err := readCatalogFile(catalog, AchievedNowCatalogFile, "{}")
	if err != nil {
		return nil, err
	}
	generated, err := decodeSpecs("generated quest catalog", generatedCatalog)
	if err != nil {
		return nil, err
	}
	compiled, err := decodeSpecs("compiled quest catalog", compiledCatalog)
	if err != nil {
		return nil, err
	}
	additional := append([]QuestSpec(nil), europeanTutorialSpecs...)
	additional = append(additional, captureQuestSpecs...)
	additional = append(additional, rahidChainSpecs...)
	additional = append(additional, generated...)
	authored := map[string]bool{}
	for _, spec := range append(specs, additional...) {
		authored[spec.Codename] = true
	}
	// A compiled class stands only for a quest nothing else authors: a
	// curated spec already ports its custom behaviour.
	for _, spec := range compiled {
		if !authored[spec.Codename] {
			additional = append(additional, spec)
		}
	}
	for _, spec := range additional {
		// The small protocol-test catalogs intentionally contain only their own
		// rows. Production loads the complete verified v1.150 media catalog.
		if _, ok := catalog.QuestByCodename(spec.Codename); ok {
			specs = append(specs, spec)
		}
	}
	if err := applyAchievedNow(specs, achievedNowCatalog); err != nil {
		return nil, err
	}
	return specs, nil
}

/*
================
decodeSpecs

One embedded spec table; an unknown field is a generator defect.
================
*/
func decodeSpecs(name string, raw []byte) ([]QuestSpec, error) {
	var specs []QuestSpec
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&specs); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return specs, nil
}

/*
================
applyAchievedNow

The recovered script's ACHIEVED_NOW symbol fills every spec it names. A
curated spec keeps its own symbol only where no script survived; one that
disagrees with its script is an authoring error.
================
*/
func applyAchievedNow(specs []QuestSpec, achievedNowCatalog []byte) error {
	var symbols map[string]string
	if err := json.Unmarshal(achievedNowCatalog, &symbols); err != nil {
		return fmt.Errorf("achieved-now catalog: %w", err)
	}
	for i := range specs {
		symbol, ok := symbols[specs[i].Codename]
		if !ok {
			continue
		}
		if specs[i].AchievedNowSymbol != "" && specs[i].AchievedNowSymbol != symbol {
			return fmt.Errorf("quest %s achieved-now %s contradicts its script's %s", specs[i].Codename, specs[i].AchievedNowSymbol, symbol)
		}
		specs[i].AchievedNowSymbol = symbol
	}
	return nil
}
