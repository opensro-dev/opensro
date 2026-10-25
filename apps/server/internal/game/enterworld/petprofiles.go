/*
===========================================================================

petprofiles.go - startup-only pet reference balance (port-only, not native)

One catalog projection keeps summon, growth, healing and combat maxima in
agreement. Called once before gameplay admission, only with expansion on.

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/game/companion"

/*
================
ApplyAttackPetExpansion

The startup gate: rewrites the pet references only when
SRO_ATTACK_PET_EXPANSION is on, so the default catalog stays native.
Reports whether it applied.
================
*/
func (t *TextdataItems) ApplyAttackPetExpansion() bool {
	if !companion.AttackPetExpansionEnabled() {
		return false
	}
	t.ApplyAttackPetProfiles()
	return true
}

/*
================
ApplyAttackPetProfiles
================
*/
func (t *TextdataItems) ApplyAttackPetProfiles() {
	t.once.Do(t.load)
	for _, ref := range t.charactersByCodename {
		if ref.TidWord>>11 != 3 || companion.PetKind(ref.Codename) == "" {
			continue
		}
		p := companion.Profile(ref.Codename)
		ref.MaxHP = companion.BaseHealth(ref.Codename, ref.Level, ref.MaxHP)
		ref.Parameters.MaxHP = ref.MaxHP
		ref.Parameters.PhysicalDefense *= p.DefensePercent / 100
		ref.Parameters.MagicalDefense *= p.MagicDefensePercent / 100
	}
	for i := range t.summonableCharacters {
		if ref, ok := t.charactersByCodename[t.summonableCharacters[i].Codename]; ok {
			t.summonableCharacters[i] = *ref
		}
	}
}
