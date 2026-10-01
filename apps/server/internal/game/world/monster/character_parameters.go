/*
===========================================================================

character_parameters.go - shared RefObjChar combat and resistance columns

Monsters and summoned characters read the same authored parameter tail.
Keep the column mapping here so COS cannot accidentally use its owner's
equipment, and elemental resistance has one conversion into keeper order.

===========================================================================
*/

package monster

/*
================
CharacterParameters

Return the common parameter portion of a reference. Actor identity and
movement remain with the caller's loader. Missing combat columns remain
unpinned and cannot silently enter the damage formula.
================
*/
func CharacterParameters(columns []string) MonsterRef {
	ref := MonsterRef{ElementResist: elementResist(columns)}
	ref.BodyRadius, _ = nonNegativeColumnFloat(columns, colBodyRadius)
	if len(columns) <= colCriticalRate {
		return ref
	}
	entries := []struct {
		column int
		value  *float64
	}{
		{colPhysicalDef, &ref.PhysicalDefense}, {colMagicalDef, &ref.MagicalDefense},
		{colParryRate, &ref.ParryRate}, {colMagicalParry, &ref.MagicalParry},
		{colEvasionRate, &ref.EvasionRate}, {colBlockRate, &ref.BlockRate},
		{colHitRate, &ref.HitRate}, {colCriticalRate, &ref.CriticalRate},
	}
	ref.CombatPinned = true
	for _, entry := range entries {
		value, valid := nonNegativeColumnFloat(columns, entry.column)
		*entry.value = value
		ref.CombatPinned = ref.CombatPinned && valid
	}
	return ref
}
