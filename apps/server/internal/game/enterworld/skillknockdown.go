package enterworld

// ko is the authored rank/chance pair; capability belongs to the
// victim's RefObjChar.Knockdown bit 0, not to its name or monster rarity.
type SkillKnockdown struct {
	Present      bool
	Rank, Chance uint32
}

// kb: the server indexes two unsigned words. Its knockback roll requires signed-positive
// chance, while its displacement converts the distance as unsigned. Unlike ko, no
// target-level scaling or 75-percent clamp is applied.
type SkillKnockback struct {
	Present          bool
	Chance, Distance uint32
}
