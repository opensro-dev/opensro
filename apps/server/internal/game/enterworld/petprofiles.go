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
		ref.MaxHP = uint32(float64(ref.MaxHP) * p.HPPercent / 100)
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
