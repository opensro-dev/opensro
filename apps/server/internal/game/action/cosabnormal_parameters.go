/*
===========================================================================

cosabnormal_parameters.go - summoned characters use their own native keeper

Reference data supplies the COS base; its independent abnormal block supplies
status writes. Neither the rider's equipment nor depleted current vitals are
valid substitutes for authored defenses, resistance or maximum gauges.

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
cosCharacterRef

The immutable catalog can be queried from an authority transaction. Bind both
codename and numeric identity so stale or mismatched rows cannot supply stats.
================
*/
func (rt *Runtime) cosCharacterRef(character *enterworld.Character) (*enterworld.CharacterRef, bool) {
	if character == nil || character.ActiveCOS == nil {
		return nil, false
	}
	return rt.cosReference(character.ActiveCOS)
}

/*
================
cosReference

Resolve the selected canonical companion without assuming it is the rider.
================
*/
func (rt *Runtime) cosReference(pet *enterworld.CharacterCOS) (*enterworld.CharacterRef, bool) {
	if pet == nil {
		return nil, false
	}
	source, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil, false
	}
	ref, exists := source.CharacterRefByCodename(pet.Codename)
	return ref, exists && ref != nil && ref.RefObjID == pet.RefObjID
}

/*
================
cosParameter

The RefObjChar keeper and abnormal algebra are shared with monsters. Recovery
reductions and disease begin at zero; movement/action speed have explicit bases.
================
*/
func cosParameter(ref *enterworld.CharacterRef, pet *enterworld.CharacterCOS, block *abnormal.Block, id uint16) float32 {
	var base float32
	if ref != nil {
		switch {
		case id == abnormalMaxHPParam:
			base = float32(ref.MaxHP)
		case id == abnormalMaxMPParam:
			base = float32(ref.MaxMP)
		case id >= 5 && id <= 12:
			p := ref.Parameters
			base = float32([...]float64{p.PhysicalDefense, p.MagicalDefense, p.ParryRate, p.MagicalParry,
				p.EvasionRate, p.BlockRate, p.HitRate, p.CriticalRate}[id-5])
		case id == movementWalkParameter:
			base = ref.WalkSpeed
		case id == movementRunParameter:
			base = ref.RunSpeed
		case id >= abnormalElementResistBase && id < abnormalElementResistBase+6:
			base = float32(ref.Parameters.ElementResist[id-abnormalElementResistBase])
		}
	}
	if id == 0x8c {
		base = 100
	}
	satiety := uint16(companion.MaximumSatiety)
	if pet != nil && ref != nil && ref.TidWord>>11 == 3 {
		satiety = pet.Satiety
	}
	value, err := companion.Parameter(id, base, satiety, block)
	if pet != nil && ref != nil && ref.TidWord>>11 == 5 {
		value, err = companion.MercenaryParameter(id, base, pet.MercenaryAttributes, block)
	}
	if err != nil {
		log.WithError(err).WithField("param", id).Error("COS abnormal parameter projection failed")
		return 0
	}
	return value
}

/*
================
cosCombatStats

Use the common non-player combat projection, including all defense/evasion
and incoming-damage status modifiers. Catalog validation precedes admission.
================
*/
func cosCombatStats(ref *enterworld.CharacterRef, pet *enterworld.CharacterCOS, block *abnormal.Block) (combat.Stats, error) {
	base := ref.Parameters
	base.RefObjID, base.Codename, base.Level = ref.RefObjID, ref.Codename, pet.Level
	stats, err := combat.MonsterInstanceStats(monster.Instance{Ref: base, Abnormal: block})
	if err == nil && ref.TidWord>>11 == 5 {
		stats.EvasionRate = float64(cosParameter(ref, pet, block, 9))
		stats.HitRate = float64(cosParameter(ref, pet, block, 11))
		stats.PhysicalBasicRate = float64(cosParameter(ref, pet, block, 0x80))
		stats.PhysicalSkillRate = float64(cosParameter(ref, pet, block, 0x81))
		stats.MagicalBasicRate = float64(cosParameter(ref, pet, block, 0x82))
		stats.MagicalSkillRate = float64(cosParameter(ref, pet, block, 0x83))
	}
	if err != nil || ref.TidWord>>11 != 3 {
		return stats, err
	}
	for _, entry := range []struct {
		id    uint16
		value *float64
		base  float64
	}{
		{5, &stats.PhysicalDefense, base.PhysicalDefense},
		{6, &stats.MagicalDefense, base.MagicalDefense},
		{9, &stats.EvasionRate, base.EvasionRate},
		{11, &stats.HitRate, base.HitRate},
	} {
		value, err := companion.Parameter(entry.id, float32(entry.base), pet.Satiety, block)
		if err != nil {
			return combat.Stats{}, err
		}
		*entry.value = float64(value)
	}
	return stats, nil
}
