/*
===========================================================================

profile.go - design species roles (port-only attack-pet expansion)

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
BaseHealth

Port-only survival curve; native HP remains the fallback for unconfigured rows.
================
*/
func BaseHealth(codename string, level uint8, native uint32) uint32 {
	p := Profile(codename)
	if level == 0 || len(p.HealthByLevel) == 0 {
		return uint32(float64(native) * p.HPPercent / 100)
	}
	previous := p.HealthByLevel[0]
	for _, point := range p.HealthByLevel[1:] {
		if level <= point.Level {
			return previous.HP + uint32(uint64(point.HP-previous.HP)*uint64(level-previous.Level)/uint64(point.Level-previous.Level))
		}
		previous = point
	}
	return previous.HP
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
