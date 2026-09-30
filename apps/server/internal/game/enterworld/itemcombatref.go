/*
================================================================================
itemcombatref.go - typed v1.150 item ranges for the shared combat projection

These values are the v1.150 itemdata inputs consumed by
CSOItem_ApplyOneVarianceStat. They are reference ranges, not live character
stats. The combat package combines them with one persisted item's variance
bits and plus value.
================================================================================
*/

package enterworld

// ItemStatRange is one RefItemData minimum/maximum/per-plus triplet.
/*
================
ItemStatRange
================
*/
type ItemStatRange struct {
	Min     float64
	Max     float64
	PerPlus float64
}

// ItemAttackRange holds the two independently-derived ends of an attack
// interval. Minimum derives from the first itemdata pair, Maximum from the
// second pair, and both use PerPlus.
/*
================
ItemAttackRange
================
*/
type ItemAttackRange struct {
	Minimum ItemStatRange
	Maximum ItemStatRange
}

// ItemCombatRef is the complete set of itemdata columns used by the native
// variance-stat dispatcher for combat. A nil *ItemCombatRef means the row did
// not carry a complete numeric source and combat must refuse rather than
// derive a partial item.
/*
================
ItemCombatRef
================
*/
type ItemCombatRef struct {
	ActionRange float64

	PhysicalDefense ItemStatRange
	EvasionRate     ItemStatRange
	ParryRate       ItemStatRange
	BlockRate       ItemStatRange
	MagicalDefense  ItemStatRange
	MagicalParry    ItemStatRange

	PhysicalAttack ItemAttackRange
	MagicalAttack  ItemAttackRange
	HitRate        ItemStatRange
	CriticalRate   ItemStatRange

	PhysicalReinforcement        ItemAttackRange
	MagicalReinforcement         ItemAttackRange
	PhysicalDefenseReinforcement ItemStatRange
	MagicalDefenseReinforcement  ItemStatRange
}
