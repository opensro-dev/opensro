/*
===========================================================================
gearseal.go - pet seal identities and their white/blue stat multipliers

The tens digit within each slot's reserved reference range owns the seal.
Saved ordinary items keep their IDs and rolls; seals use separate references.
===========================================================================
*/
package companion

import "strings"

/*
================
SealTier
================
*/
func SealTier(id uint32) int {
	tier := int(id%100) / 10
	if id < 30100 || id > 30399 || tier > 3 || id%10 < 1 || id%10 > 9 {
		return 0
	}
	return tier
}

/*
================
sealedDegree

Clone only the selected piece so a lookup cannot mutate the shared design.
================
*/
func sealedDegree(d GearDegree, slot, tier int) GearDegree {
	if tier == 0 {
		return d
	}
	d.Items = append([]GearItem(nil), d.Items...)
	item := &d.Items[slot]
	item.ID += uint32(tier * 10)
	item.Codename += []string{"", "_STAR", "_MOON", "_SUN"}[tier]
	scale := 1 + float64(tier)*0.5
	d.ProcDamage = uint32(float64(d.ProcDamage) * scale)
	g := &item.BaseStats
	g.MaxHPPercent *= scale
	g.PhysicalAttack *= scale
	g.MagicalAttack *= scale
	g.PhysicalDefense *= scale
	g.MagicalDefense *= scale
	g.HitRatio *= scale
	g.ParryRatio *= scale
	return d
}

/*
================
sealCodename
================
*/
func sealCodename(codename string) (string, int) {
	for tier, suffix := range []string{"_STAR", "_MOON", "_SUN"} {
		if strings.HasSuffix(codename, suffix) {
			return strings.TrimSuffix(codename, suffix), tier + 1
		}
	}
	return codename, 0
}

/*
================
ScaleSealOptions

Scale saved magnitudes once at drop creation, never again on equip/relog.
Integer blue values round down; the global cooldown/chance caps still apply.
================
*/
func ScaleSealOptions(options []uint64, tier int) {
	if tier < 1 || tier > 3 {
		return
	}
	for i, option := range options {
		if affix, value, ok := DecodeAffix(option); ok {
			options[i] = EncodeAffix(affix.Code, uint32(uint64(value)*uint64(2+tier)/2))
		}
	}
}
