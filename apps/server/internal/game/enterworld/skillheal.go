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

	heal +0x324  {hp, hp%, mp, mp%}
	mwhh +0x328  add the caster's weapon term to the HP amount
	mwmh +0x32C  add it to the MP amount
	nmh  +0x598  a cast heal's HP percent is a share of maximum HP

The server's skill heal reads it for a cast heal and
its heal-recovery application for the eshp aura. The two apply the
percent words differently.
==================
*/
type SkillHeal struct {
	Present                      bool
	HP, HPPercent, MP, MPPercent uint32
	WeaponHP, WeaponMP           bool
	WeaponHPWord, WeaponMPWord   uint32
	OfMaxHP                      bool // nmh
}
