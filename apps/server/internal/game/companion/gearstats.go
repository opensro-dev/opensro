/*
===========================================================================

gearstats.go - flat pet equipment stats, added before percentage modifiers

Port-only. Authored by the shared design exporter; no hidden percentage
implicit. Existing saved blue options remain unchanged.
===========================================================================
*/
package companion

/*
================
GearStats
================
*/
type GearStats struct {
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
	g.PhysicalAttack += b.PhysicalAttack
	g.MagicalAttack += b.MagicalAttack
	g.PhysicalDefense += b.PhysicalDefense
	g.MagicalDefense += b.MagicalDefense
	g.HitRatio += b.HitRatio
	g.ParryRatio += b.ParryRatio
}
