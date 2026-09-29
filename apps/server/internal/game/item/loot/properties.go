/*
===========================================================================

properties.go - client-constrained equipment magic options for monster loot

The native constructor owns the chance, count and duplicate-slot behavior.
The v1.150 client owns option identities, degrees, values and applicability.

===========================================================================
*/
package loot

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/game/item/alchemy"
)

const (
	magicChanceDenominator = 101
	magicChanceThreshold   = 30
	magicCountDraws        = 4
	magicCountChoices      = 6
	nonRepairTag           = 0x6e726570
)

//go:embed .generated/properties.json
var propertiesJSON []byte

/*
================
propertyItem
================
*/
type propertyItem struct {
	Codename string
	Type     [4]uint8
	Country  uint8
	Rarity   uint8
	Class    int
	MaxMagic int
}

/*
================
propertyOption
================
*/
type propertyOption struct {
	alchemy.Magic
	Probability float32
}

/*
================
propertyAssignment
================
*/
type propertyAssignment struct {
	Country, Type3, Type4 uint8
	Options               []string
}

/*
================
propertyCatalog
================
*/
type propertyCatalog struct {
	Items       map[string]propertyItem
	Magic       []propertyOption
	Assignments []propertyAssignment
	pools       map[string][]propertyOption
	nonRepair   propertyOption
}

var properties = loadProperties()

/*
================
loadProperties
================
*/
func loadProperties() propertyCatalog {
	var c propertyCatalog
	if err := json.Unmarshal(propertiesJSON, &c); err != nil {
		panic(err)
	}
	c.pools = make(map[string][]propertyOption, len(c.Items))
	for _, option := range c.Magic {
		if !probabilityValid(option.Probability) || option.ID == 0 {
			panic("invalid loot magic option")
		}
		if option.Name == "MATTR_NOT_REPARABLE" && option.Degree == 1 {
			c.nonRepair = option
		}
	}
	if c.nonRepair.ID == 0 || c.nonRepair.Params[0] != 6 {
		panic("missing constant nonrepair option")
	}
	for code, item := range c.Items {
		if item.MaxMagic < 0 || item.MaxMagic > 12 || item.Class <= 0 {
			panic("invalid loot equipment property reference: " + code)
		}
		for _, assignment := range c.Assignments {
			if assignment.Country != item.Country || assignment.Type3 != item.Type[2] || assignment.Type4 != item.Type[3] {
				continue
			}
			for _, name := range assignment.Options {
				for _, option := range c.Magic {
					// Reconstruction: use the client equipment degree when joining
					// its named option assignment to the versioned option rows.
					if option.Name == name && option.Degree == (item.Class+2)/3 {
						c.pools[code] = append(c.pools[code], option)
						break
					}
				}
			}
		}
	}
	return c
}

/*
================
EquipmentMagic

7276A0: normal equipment enters at rand%%101 <= 30; rare equipment always
enters. Four six-way draws bound the number of slots. Duplicate choices spend
a slot; they do not select again. A failed gate discards special equipment
(725720), while the ordinary producer retains the item without blues.
================
*/
func EquipmentMagic(code string, roll func() (uint32, error)) ([]uint64, bool, error) {
	item, exists := properties.Items[code]
	if !exists || roll == nil {
		return nil, false, fmt.Errorf("missing loot properties for %s", code)
	}
	if item.Rarity != 2 {
		chance, err := roll()
		if err != nil {
			return nil, false, err
		}
		if chance%magicChanceDenominator > magicChanceThreshold {
			return nil, false, nil
		}
	}
	count := item.MaxMagic
	for i := 0; i < magicCountDraws; i++ {
		n, err := roll()
		if err != nil {
			return nil, false, err
		}
		count = min(count, int(n%magicCountChoices)+1)
	}
	count = max(1, min(count, magicCountChoices))
	pool := properties.pools[code]
	seen := make(map[int]bool, count)
	var options []uint64
	flags := uint16(item.Type[0])<<2 | uint16(item.Type[1])<<5 | uint16(item.Type[2])<<7 | uint16(item.Type[3])<<11
	for slot := 0; slot < count && len(pool) > 0; slot++ {
		n, err := roll()
		if err != nil {
			return nil, false, err
		}
		index := int(n % uint32(len(pool)))
		if seen[index] {
			continue
		}
		option := pool[index]
		chance, ok := rollMillion(roll)
		if !ok {
			return nil, false, fmt.Errorf("loot magic random draw failed")
		}
		if chance > uint32(float64(option.Probability)*float64(probabilityScale)) || !option.Allows(flags) {
			continue
		}
		if option.Tag == nonRepairTag {
			slot--
			continue
		}
		value, err := alchemy.RollMagicValue(option.Magic, alchemy.Roll(roll))
		if err != nil {
			return nil, false, err
		}
		options = append(options, uint64(value)<<32|uint64(option.ID))
		seen[index] = true
	}
	return options, true, nil
}

/*
================
NonRepairOption

726020 appends the degree-1 nonrepair option to eligible normal equipment.
Client magicoption.txt encodes its fixed value as [6,400,...]: +400% durability.
================
*/
func NonRepairOption(code string, currentOptions int) (uint64, uint32, bool) {
	item, exists := properties.Items[code]
	if !exists || item.Rarity == 2 || currentOptions >= item.MaxMagic {
		return 0, 0, false
	}
	option := properties.nonRepair
	value := option.Params[1]
	return uint64(value)<<32 | uint64(option.ID), value, true
}

/*
================
ValidateMagicReferences

The caller resolves the loaded client reference plane. A bad deployment fails
at startup, before an item can acquire an option the client cannot interpret.
================
*/
func ValidateMagicReferences(resolve func(uint32, string, int) bool) error {
	if resolve == nil {
		return fmt.Errorf("loot requires magic-option references")
	}
	for _, option := range properties.Magic {
		if !resolve(uint32(option.ID), option.Name, option.Degree) {
			return fmt.Errorf("loot magic reference missing or mismatched: %s degree %d", option.Name, option.Degree)
		}
	}
	return nil
}
