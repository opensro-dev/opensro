/*
===========================================================================

tables.go - the loot catalogue's tables, loaded once at startup

The equipment, magic-property and consumable tables are compiled by
scripts/build/generate_loot_catalog.py from the operator's source-data and
reach the server through the projection's derived data (gamedata
DerivedDir). The repository never carries them. Load runs once, before any
gameplay owner starts and before InitCap110; without the tables the catalogue
stays empty and monsters drop no generated loot.

===========================================================================
*/
package loot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// TableFiles are the catalogue's tables under the derived data, in load order.
var TableFiles = [3]string{"loot/properties.json", "loot/equipment.json", "loot/consumables.json"}

/*
================
Load

The tables from derivedDir, or false when there is no derived folder or any
table is absent (the catalogue stays empty). Every table compiles before any
is published, so a bad file leaves the previous state untouched.
================
*/
func Load(derivedDir string) (bool, error) {
	if derivedDir == "" {
		return false, nil
	}
	var data [len(TableFiles)][]byte
	for i, name := range TableFiles {
		bytes, err := os.ReadFile(filepath.Join(derivedDir, filepath.FromSlash(name)))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("loot: %s: %w", name, err)
		}
		data[i] = bytes
	}
	return true, LoadTables(data[0], data[1], data[2])
}

/*
================
LoadTables

Compile and publish the three tables (TableFiles order). Tests load the
operator's copies through it (internal/testsupport/loottest).
================
*/
func LoadTables(propertiesData, equipmentData, consumablesData []byte) error {
	nextProperties, err := parseProperties(propertiesData)
	if err != nil {
		return err
	}
	nextEquipment, err := parseEquipment(equipmentData)
	if err != nil {
		return err
	}
	nextConsumables, err := parseConsumables(consumablesData)
	if err != nil {
		return err
	}
	properties, equipment, consumables = nextProperties, nextEquipment, nextConsumables
	nativeConsumables = nextConsumables
	sealBandRows = buildSealBandRows(nextEquipment)
	sunClassGroup = deriveSunClassGroup(nextEquipment)
	return nil
}

/*
================
Loaded

True once the tables are published.
================
*/
func Loaded() bool {
	return len(equipment.buckets) > 0
}
