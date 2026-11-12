/*
===========================================================================

exportworld.go - the export-world admission index (port-only).

Port-only, not native; the areas exist only while SRO_EXPORT_WORLD
is on (#3329). The build (scripts/export_world_import.py) writes
export-world.json beside the character tables: the areas the pinned
content archive packed, the regions each admits and the character
kinds its spawns and standings may name. This leaf owns only that
index: the monster catalog asks it before admitting a population
row, so a kind or region outside the archive's areas never reaches
gameplay. It reads one JSON file and nothing else.

With the switch off the file is never read and nothing is admitted;
with it on, a missing file admits nothing (no area packed yet is
the ordinary state until an area import lands), and a malformed
one fails the load by name. The character identity band 190000 is
reserved for the areas' kind conversions (the build records it);
this index carries codenames, not identities.

===========================================================================
*/
package exportworld

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// FileName is the admission index's name in the textdata directory.
const FileName = "export-world.json"

/*
================
manifest

The build's export-world.json: per area, the sector region ids it
admits (the (y << 8) | x the region tables name them by) and the
character codenames its population may use.
================
*/
type manifest struct {
	Areas map[string]struct {
		Regions []int    `json:"regions"`
		Kinds   []string `json:"kinds"`
	} `json:"areas"`
}

/*
================
Index

The admitted regions and kinds, each mapped to its owning area so
a refusal can name it. The zero Index admits nothing.
================
*/
type Index struct {
	regions map[int]string
	kinds   map[string]string
	areas   []string
}

/*
================
Load

Read and validate the admission index. Disabled returns the empty
index without touching the disk; enabled with no file is the empty
index too (no area packed yet). Anything malformed - unreadable
JSON, trailing data, an unnamed area, a region or kind claimed
twice - is an error: a partial index must not admit anything.
================
*/
func Load(textdataDir string, enabled bool) (Index, error) {
	if !enabled {
		return Index{}, nil
	}
	path := filepath.Join(textdataDir, FileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Index{}, nil
	}
	if err != nil {
		return Index{}, fmt.Errorf("export world index: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var source manifest
	if err := decoder.Decode(&source); err != nil {
		return Index{}, fmt.Errorf("export world index %s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Index{}, fmt.Errorf("export world index %s has trailing data", path)
	}
	index := Index{
		regions: make(map[int]string),
		kinds:   make(map[string]string),
	}
	for _, area := range sortedKeys(source.Areas) {
		entry := source.Areas[area]
		if area == "" {
			return Index{}, fmt.Errorf("export world index %s names an empty area", path)
		}
		index.areas = append(index.areas, area)
		for _, regionID := range entry.Regions {
			if regionID <= 0 || regionID > 0xFFFF {
				return Index{}, fmt.Errorf("export world index %s: area %q admits region %d, outside the sector id space", path, area, regionID)
			}
			if owner, taken := index.regions[regionID]; taken {
				return Index{}, fmt.Errorf("export world index %s: region %d is admitted by both %q and %q", path, regionID, owner, area)
			}
			index.regions[regionID] = area
		}
		for _, codename := range entry.Kinds {
			if codename == "" {
				return Index{}, fmt.Errorf("export world index %s: area %q admits an empty codename", path, area)
			}
			if owner, taken := index.kinds[codename]; taken {
				return Index{}, fmt.Errorf("export world index %s: kind %q is admitted by both %q and %q", path, codename, owner, area)
			}
			index.kinds[codename] = area
		}
	}
	return index, nil
}

/*
================
AdmitsRegion

Whether any packed area admits this region id.
================
*/
func (index Index) AdmitsRegion(regionID int) bool {
	_, ok := index.regions[regionID]
	return ok
}

/*
================
AdmitsKind

Whether any packed area admits this character codename.
================
*/
func (index Index) AdmitsKind(codename string) bool {
	_, ok := index.kinds[codename]
	return ok
}

/*
================
Areas

The packed areas' names, sorted (the load's own order).
================
*/
func (index Index) Areas() []string {
	return append([]string(nil), index.areas...)
}

/*
================
sortedKeys
================
*/
func sortedKeys[V any](entries map[string]V) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
