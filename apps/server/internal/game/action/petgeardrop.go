/*
===========================================================================

petgeardrop.go - monsters drop attack-pet gear with rolled blue options

Port-only, not native (companion/gear.go, SRO_ATTACK_PET_EXPANSION). Pet
gear has no shop: actual normal equipment drops independently yield an
extra same-degree pet item at the DB-configured factor. Native tables own
the monster/degree/rank/level-gap probabilities. The summoned pet raises blue odds with
Lucky Feather or "of Fortune" blues.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petGearShopOffer

Port-only pet equipment is monster loot, including when a shop package
or operator-added offer accidentally references it.
================
*/
func petGearShopOffer(offer commerce.Offer) bool {
	if offer.Ref != nil && companion.IsGearRef(offer.Ref.RefObjID) {
		return true
	}
	for _, content := range offer.Contents {
		if content.Ref != nil && companion.IsGearRef(content.Ref.RefObjID) {
			return true
		}
	}
	return false
}

/*
================
dropRoller

The drop RNG as a companion.Roller; a failed draw reads as the top of the
range, which never rolls a drop or a blue.
================
*/
type dropRoller struct{ rt *Runtime }

func (r dropRoller) Float() float64 {
	if r.rt.DropRoll == nil {
		return 1
	}
	draw, err := r.rt.DropRoll()
	if err != nil {
		return 1
	}
	return float64(draw%dropRollDomain) / dropRollDomain
}

/*
================
preparePetGearDrop
================
*/
func (rt *Runtime) preparePetGearDrop(killer *enterworld.Character, parent grounditem.Item, at simulation.Spawn, now time.Time) (grounditem.Item, bool) {
	items := rt.deps.ItemReferences()
	if !rt.AttackPetExpansion || items == nil || rt.PetGearDropFactor <= 0 {
		return grounditem.Item{}, false
	}
	parentRef, found := items.ItemRefByID(parent.RefObjID)
	if !found || parentRef == nil || parentRef.TypeIDs[0] != 3 || parentRef.TypeIDs[1] != 1 {
		return grounditem.Item{}, false
	}
	class := int(parentRef.NativeFields.Get("itemClass"))
	if class < 1 || class > 27 {
		return grounditem.Item{}, false
	}
	degree := companion.D.Gear[(class-1)/3]
	if len(degree.Items) == 0 {
		return grounditem.Item{}, false
	}
	roll := dropRoller{rt}
	if roll.Float() >= rt.PetGearDropFactor {
		return grounditem.Item{}, false
	}
	find := 0.0
	if killer.ActiveCOS != nil && killer.ActiveCOS.Summoned {
		find = companion.ModifiersFor(killer.ActiveCOS).FindPct
	}
	slot := min(int(roll.Float()*float64(len(degree.Items))), len(degree.Items)-1)
	// Reuse the parent equipment's native seal roll and seal tier.
	tier := 0
	if parentRef.NativeFields.Get("rarity") == 2 {
		tier = (class-1)%3 + 1
		sealed, sealedSlot, found := companion.GearByID(degree.Items[slot].ID + uint32(tier*10))
		if !found {
			// A piece outside the seal family has no sealed variant: skip
			// the pet drop instead of indexing the empty lookup result.
			return grounditem.Item{}, false
		}
		degree, slot = sealed, sealedSlot
	}
	ref, found := items.ItemRefByCodename(degree.Items[slot].Codename)
	if !found || ref == nil {
		return grounditem.Item{}, false
	}
	affixes := rt.PetGearAffixes
	if affixes == nil {
		affixes = companion.D.Affixes
	}
	row := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1,
		MagicOptions: companion.RollGearWithAffixes(degree, slot, find, roll, affixes)}
	companion.ScaleSealOptions(row.MagicOptions, tier)
	return PlanItemDrop(row, 1, at, killer.Name, now), true
}
