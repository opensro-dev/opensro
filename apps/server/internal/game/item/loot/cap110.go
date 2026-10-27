/*
===========================================================================
cap110.go - startup installation of optional level-101 through 110 loot.

Port-only, not native. The verified projection carries an equipment catalog
compiled from the owner's database and installed references. InitCap110 is
called before gameplay owners start; the native catalogs remain the default.
===========================================================================
*/
package loot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const cap110EquipmentFile = "cap110-equipment.txt"
const cap110EquipmentVersion = 1

/*
================
cap110Catalog
================
*/
type cap110Catalog struct {
	Version    int
	Equipment  equipmentSource
	Properties propertyCatalog
}

/*
================
InitCap110

Startup only, before action/reference admission. Compile every plane (equipment,
properties, rewards) before publishing any, so an invalid runtime file leaves
native state untouched.
================
*/
func InitCap110(textdataDir string, enabled bool) error {
	if !enabled {
		cap110Installed = false
		return nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, cap110EquipmentFile))
	if err != nil {
		return fmt.Errorf("cap110 loot: %w", err)
	}
	nextEquipment, nextProperties, err := compileCap110(data)
	if err != nil {
		return err
	}
	rewardData, err := os.ReadFile(filepath.Join(textdataDir, cap110RewardsFile))
	if err != nil {
		return fmt.Errorf("cap110 rewards: %w", err)
	}
	nextConsumables, err := compileCap110Rewards(nativeConsumables, rewardData)
	if err != nil {
		return err
	}
	markExpansionBuckets(nextEquipment, equipment)
	equipment, properties, consumables = nextEquipment, nextProperties, nextConsumables
	cap110Installed = true
	return nil
}

/*
================
markExpansionBuckets

Flags every bucket of the cap-110 catalog that the native catalog leaves empty
(degree 10 and up), so selectEquipment keeps monsters of the native levels
away from it. Buckets the native catalog fills are untouched.
================
*/
func markExpansionBuckets(next, native equipmentCatalog) {
	for key, bucket := range next.buckets {
		if native.buckets[key] == nil {
			bucket.expansionOnly = true
		}
	}
}

/*
================
compileCap110
================
*/
func compileCap110(data []byte) (equipmentCatalog, propertyCatalog, error) {
	var source cap110Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return equipmentCatalog{}, propertyCatalog{}, fmt.Errorf("cap110 loot: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF || source.Version != cap110EquipmentVersion {
		return equipmentCatalog{}, propertyCatalog{}, fmt.Errorf("cap110 loot has invalid version or trailing data")
	}
	nextEquipment, err := compileEquipment(source.Equipment)
	if err != nil {
		return equipmentCatalog{}, propertyCatalog{}, fmt.Errorf("cap110 loot: %w", err)
	}
	nextProperties, err := compileProperties(source.Properties)
	if err != nil {
		return equipmentCatalog{}, propertyCatalog{}, fmt.Errorf("cap110 properties: %w", err)
	}
	for _, ref := range source.Equipment.Items {
		if _, ok := nextProperties.Items[ref.Codename]; !ok {
			return equipmentCatalog{}, propertyCatalog{}, fmt.Errorf("cap110 loot lacks properties for %s", ref.Codename)
		}
	}
	return nextEquipment, nextProperties, nil
}
