/*
===========================================================================

skillheal.go - the heal block

Amounts are computed by action/skillheal.go.

===========================================================================
*/

package enterworld

/*
==================
SkillHeal

	heal  {hp, hp%, mp, mp%}
	mwhh  add the caster's weapon term to the HP amount
	mwmh  add it to the MP amount
	nmh   a cast heal's HP percent is a share of maximum HP

The native skill heal reads it for a cast heal and the native heal recovery
for the eshp aura. The two apply the percent words differently.
==================
*/
type SkillHeal struct {
	Present                      bool
	HP, HPPercent, MP, MPPercent uint32
	WeaponHP, WeaponMP           bool
	WeaponHPWord, WeaponMPWord   uint32
	OfMaxHP                      bool // nmh
}
