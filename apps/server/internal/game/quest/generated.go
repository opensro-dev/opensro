/*
===========================================================================

generated.go - composition of curated and generated quest definitions

The embedded projection contains data, not executable scripts. Every row
passes the same version-aware loader and reference validation as curated
native handlers. Research directories are never runtime dependencies.

===========================================================================
*/
package quest

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed catalog_generated.json
var generatedCatalog []byte

/*
================
catalogSpecs

Small protocol fixtures contain only their authored subset. Production's
complete media catalog admits every corresponding compiled contract.
================
*/
func catalogSpecs(catalog *Catalog) ([]QuestSpec, error) {
	specs := append([]QuestSpec(nil), curatedQuestSpecs...)
	var generated []QuestSpec
	decoder := json.NewDecoder(bytes.NewReader(generatedCatalog))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&generated); err != nil {
		return nil, fmt.Errorf("generated quest catalog: %w", err)
	}
	additional := append([]QuestSpec(nil), europeanTutorialSpecs...)
	additional = append(additional, captureQuestSpecs...)
	additional = append(additional, generated...)
	for _, spec := range additional {
		// The small protocol-test catalogs intentionally contain only their own
		// rows. Production loads the complete verified v1.150 media catalog.
		if _, ok := catalog.QuestByCodename(spec.Codename); ok {
			specs = append(specs, spec)
		}
	}
	return specs, nil
}
