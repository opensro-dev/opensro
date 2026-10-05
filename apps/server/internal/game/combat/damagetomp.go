/*
===========================================================================
damagetomp.go - dgmp damage redirection before the recipient HP debit
The effect owner supplies the authored percentage; this function owns no state.
===========================================================================
*/
package combat

/*
================
DamageToMP

GameServer 5A13FE..5A14D6: truncate the redirected fraction, charge 1.5 MP
per redirected point, then truncate the uncovered MP cost / 1.5 back to HP.
The server uses FPCW 027F. Preserve each double operation and truncation;
combining this into one percentage changes low-MP and one-point outcomes.
This is hit post-processing, not the abnormal damage or HP-consumption path.
================
*/
func DamageToMP(damage, currentMP, percent uint32) (hpDamage, mpDamage uint32) {
	const (
		mpPerDamage = 1.5
		percentBase = 100
	)
	redirected := fistpLow(float64(percent) / percentBase * float64(damage))
	hpDamage = damage - redirected
	mpDamage = fistpLow(float64(int32(redirected)) * mpPerDamage)
	if mpDamage > currentMP {
		hpDamage += fistpLow(float64(int32(mpDamage-currentMP)) / mpPerDamage)
		mpDamage = currentMP
	}
	return
}

/*
================
RedirectDamageToMP

5A0B87..5A0BA7 skips damage post-processing when the hit or both original
lane accumulators are zero. Flat/periodic HP debits are not ordinary lanes.
Keep this guard with the shared mechanic so new hit producers cannot omit it.
================
*/
func RedirectDamageToMP(hit Result, currentMP, percent uint32) (Result, uint32) {
	if percent == 0 || hit.Blocked || hit.Damage == 0 || hit.PhysicalDamage == 0 && hit.MagicalDamage == 0 {
		return hit, 0
	}
	var spent uint32
	hit.Damage, spent = DamageToMP(hit.Damage, currentMP, percent)
	return hit, spent
}
