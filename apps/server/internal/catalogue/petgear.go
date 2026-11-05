/*
===========================================================================

petgear.go - the attack-pet gear derived drop, as the site states it

Port-only, not native (SRO_ATTACK_PET_EXPANSION): pet gear has no drop
table of its own. While the expansion runs, a monster's equipment drop
derives one same-degree pet piece at the petGearDropFactor setting
(action.preparePetGearDrop): the piece is drawn evenly from the parent
degree's table, and a sealed parent yields the sealed variant of the
piece its tier names. This file publishes that rule once in
droptables.json and names it on every pet gear item's own row, at the
factor the shard runs by default.

===========================================================================
*/
package catalogue

import (
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
PetGearDrop

The derived-drop source of one attack-pet gear piece: it drops with
equipment of its degree, this share of those drops. A sealed piece drops
only with a sealed parent of the piece's own tier; Seal is empty for the
normal piece.
================
*/
type PetGearDrop struct {
	Degree int     `json:"degree"`
	Seal   string  `json:"seal,omitempty" enum:"rarity"`
	Chance float64 `json:"chance" doc:"Share of equipment drops of this gear's degree with the same seal state that also yield this piece: the petGearDropFactor setting (database default 0.10) spread evenly over the degree's pieces."`
}

/*
================
petGearDropFor

The source of ref's row at the running factor, or nil: the expansion is
off, the factor is zero, or the item is no pet gear piece.
================
*/
func petGearDropFor(ref *enterworld.ItemRef, factor float64) *PetGearDrop {
	if factor <= 0 || ref == nil {
		return nil
	}
	degree, _, ok := companion.GearByID(ref.RefObjID)
	if !ok {
		return nil
	}
	drop := &PetGearDrop{Degree: degree.Degree, Chance: factor / float64(len(degree.Items))}
	if tier := companion.SealTier(ref.RefObjID); tier > 0 {
		// Companion tiers run 1..3; sealNames (enterworld) runs 0..2.
		drop.Seal = sealNames[tier-1]
	}
	return drop
}

/*
================
PetGearDegree

The pieces one derived drop of this degree can yield.
================
*/
type PetGearDegree struct {
	Degree int      `json:"degree"`
	Items  []uint32 `json:"items"`
}

/*
================
PetGearRule

The pet gear derivation as one rule: the factor and, per degree, the
pieces a drop can yield. droptables.json carries it only while the
attack pet expansion runs at a positive factor.
================
*/
type PetGearRule struct {
	Factor  float64         `json:"factor" doc:"The petGearDropFactor setting the export publishes at: the database default 0.10 unless the composition root says otherwise. An operator can retune the setting while the shard is stopped; the live rule follows the database."`
	Degrees []PetGearDegree `json:"degrees"`
}

/*
================
BuildPetGearRule

The derivation rule at the given factor, or nil while the attack pet
expansion is off or the factor is zero. Item ids resolve through itemID;
a piece the shipped bundle does not carry is left out of its degree.
================
*/
func BuildPetGearRule(factor float64, itemID func(string) uint32) *PetGearRule {
	if factor <= 0 || !companion.AttackPetExpansionEnabled() {
		return nil
	}
	rule := &PetGearRule{Factor: factor, Degrees: make([]PetGearDegree, 0, len(companion.D.Gear))}
	for _, degree := range companion.D.Gear {
		row := PetGearDegree{Degree: degree.Degree}
		for _, item := range degree.Items {
			if id := itemID(item.Codename); id != 0 {
				row.Items = append(row.Items, id)
			}
		}
		rule.Degrees = append(rule.Degrees, row)
	}
	return rule
}
