/*
===========================================================================

exportworld.go - the export-world population admission (port-only).

Port-only, not native. SRO_EXPORT_WORLD is off by default. The
build merges the pinned content archive's areas into the variant
(scripts/export_world_import.py) and writes their admission index
(world/exportworld); an area's own import PR then authors its
population as a Version-2 nest file (export-world-field.txt) whose
kinds and regions that index admits. Off never reads either file;
on fails before startup when a population row names a kind or a
region no packed area admits - a partial world must not reach
gameplay. With no population file yet (no area import has landed),
on boots the native population unchanged.

===========================================================================
*/
package monster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/exportworld"
)

const EnvExportWorld = "SRO_EXPORT_WORLD"
const exportWorldFieldFile = "export-world-field.txt"

/*
================
ExportWorldEnabled
================
*/
func ExportWorldEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvExportWorld))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
AppendExportWorld

Off never reads the optional files. On reads the packed areas'
admission index first; a population file is optional (an area can
be imported before its population lands), but every nest it holds
must stand in an admitted region and name an admitted kind.
================
*/
func AppendExportWorld(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	index, err := exportworld.Load(textdataDir, true)
	if err != nil {
		return Template{}, err
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, exportWorldFieldFile))
	if os.IsNotExist(err) {
		return template, nil
	}
	if err != nil {
		return Template{}, fmt.Errorf("export world population: %w", err)
	}
	if err := checkExportWorldRegions(data, index); err != nil {
		return Template{}, err
	}
	return appendNests(template, data, "export world population", func(ref MonsterRef, codename string) bool {
		return index.AdmitsKind(codename)
	}, nil, nil)
}

/*
================
checkExportWorldRegions

The region half of the admission, checked before the nest loader
runs: it refuses kinds, this refuses ground. A file whose version
is not the population version is left for the loader to refuse in
its own words.
================
*/
func checkExportWorldRegions(data []byte, index exportworld.Index) error {
	var source struct {
		Version int
		Rows    []struct {
			Codename string
			RegionID int
		}
	}
	if err := json.Unmarshal(data, &source); err != nil {
		return fmt.Errorf("export world population: %w", err)
	}
	if source.Version != cap110PopulationVersion {
		return nil
	}
	for _, row := range source.Rows {
		if !index.AdmitsRegion(row.RegionID) {
			return fmt.Errorf("export world population has nest %q in region %d, which no packed area admits", row.Codename, row.RegionID)
		}
	}
	return nil
}
