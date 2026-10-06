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
	if country, valid := columnUint(columns, colCountry); valid && country <= 3 {
		ref.Country = uint8(country)
	}
	ref.DefaultSkillIDs, ref.RewardActionPinned = characterDefaultSkills(columns)
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

/*
================
characterDefaultSkills

RefObjChar+210..234: the ten authored actions used by both monster and
companion tactics. Zero is an empty slot; malformed rows remain unpinned.
================
*/
func characterDefaultSkills(columns []string) ([10]uint32, bool) {
	var skills [10]uint32
	valid := len(columns) > colDefaultSkillN
	if !valid {
		return skills, false
	}
	for i := range skills {
		value, ok := columnUint32(columns, colDefaultSkill1+i)
		skills[i] = value
		valid = valid && ok
	}
	return skills, valid
}
