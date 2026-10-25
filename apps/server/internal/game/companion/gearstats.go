/*
===========================================================================

gearstats.go - pet equipment stats, including the explicit maximum-HP bonus

Port-only. Authored by the shared design exporter. Existing saved blue options
remain unchanged; maximum-HP bonuses are additive before passive HP modifiers.
===========================================================================
*/
package companion

/*
================
GearStats
================
*/
type GearStats struct {
	MaxHPPercent    float64 `json:"maxHPPercent"`
	PhysicalAttack  float64 `json:"physicalAttack"`
	MagicalAttack   float64 `json:"magicalAttack"`
	PhysicalDefense float64 `json:"physicalDefense"`
	MagicalDefense  float64 `json:"magicalDefense"`
	HitRatio        float64 `json:"hitRatio"`
	ParryRatio      float64 `json:"parryRatio"`
}

/*
================
add
================
*/
func (g *GearStats) add(b GearStats) {
	g.MaxHPPercent += b.MaxHPPercent
	g.PhysicalAttack += b.PhysicalAttack
	g.MagicalAttack += b.MagicalAttack
	g.PhysicalDefense += b.PhysicalDefense
	g.MagicalDefense += b.MagicalDefense
	g.HitRatio += b.HitRatio
	g.ParryRatio += b.ParryRatio
}
