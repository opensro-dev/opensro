/*
===========================================================================

petaffixes.go - apply the shard DB's blue roll policy to future pet loot

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/companion"
)

/*
================
loadPetGearAffixes
================
*/
func loadPetGearAffixes(authority *store.Store) ([]companion.AffixDef, error) {
	affixes := append([]companion.AffixDef(nil), companion.D.Affixes...)
	defaults := make([]store.PetAffixRule, 0, len(affixes))
	for _, a := range affixes {
		maximum := a.Max
		if a.Group == "Cooldown" && a.Tier == 3 {
			maximum = 50
		}
		defaults = append(defaults, store.PetAffixRule{Code: a.Code, Name: a.Name, Group: a.Group,
			Minimum: a.Min, Maximum: maximum, Weight: a.Weight, RollPower: 3})
	}
	rules, err := authority.PetAffixRules(defaults)
	if err != nil {
		return nil, err
	}
	for i, r := range rules {
		affixes[i].Min, affixes[i].Max, affixes[i].Weight, affixes[i].RollPower = r.Minimum, r.Maximum, r.Weight, r.RollPower
	}
	return affixes, nil
}
