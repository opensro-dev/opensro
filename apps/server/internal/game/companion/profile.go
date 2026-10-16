/*
===========================================================================

profile.go - workbook species roles (port-only attack-pet expansion)

===========================================================================
*/
package companion

/*
================
Profile
================
*/
func Profile(codename string) PetDef {
	if p := petByName(D, PetKind(codename)); p != nil {
		return *p
	}
	return PetDef{HPPercent: 100, PhysicalPercent: 100, MagicalPercent: 100, DefensePercent: 100, MagicDefensePercent: 100}
}

/*
================
AttackScales

Species and equipment/passive percentages multiply, rather than letting a
low base erase equipment improvements.
================
*/
func AttackScales(codename string, m Modifiers) (float64, float64) {
	p := Profile(codename)
	return p.PhysicalPercent * (100 + m.AttackPct + m.PhysicalPct) / 10000,
		p.MagicalPercent * (100 + m.AttackPct + m.MagicPct) / 10000
}
