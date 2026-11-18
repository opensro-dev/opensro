/*
===========================================================================

prepare.go - a PVP character, finished at creation

Port-only, not native. On the PVP shard (pvpserver.Enabled) the creation
door hands the new record here before the store saves it. The record
leaves at the shard's level cap with the experience the native level-up
would have given it, every racial mastery at the cap, every skill those
masteries reach, the shard's starting gold, and Samarkand as both its
position and its return point. The store's creation seeds fill only what
is still empty, so what this sets stands; the native starter inventory
and the racial base attacks are still granted.

===========================================================================
*/

package pvpcharacter

import (
	"slices"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/pvpserver"
)

/*
================
Prepare

Finishes c for the PVP shard. base is the native creation seed (the
racial base attacks, enterworld.DefaultSkillSeeder); those rows cost no
SP, so FullSkills never picks their groups and the two never overlap.

Only the automatic +1 STR and +1 INT of each level are applied; the three
free points of each level are left for the player to place, because the
build is what tells PVP players apart and the native game has no free way
to undo a placement.
================
*/
func Prepare(c *domain.Character, base []uint32, skills SkillSource, levelCap int64) {
	zero := int64(0)
	level, maxLevel := levelCap, levelCap
	experience, skillExp := zero, zero
	stat := progression.BaseStatAtLevel(levelCap)
	strength, intellect := stat, stat
	statPoints := progression.StatPointsPerLevel * (levelCap - 1)
	gold := pvpserver.StartGold
	c.Level, c.MaxLevel = &level, &maxLevel
	c.Experience, c.SkillExp = &experience, &skillExp
	c.Strength, c.Intellect = &strength, &intellect
	c.StatPoints = &statPoints
	c.Gold = &gold

	flags := int64(0)
	if c.VisualFlags != nil {
		flags = *c.VisualFlags &^ int64(domain.VisualFlagBeginner)
	}
	c.VisualFlags = &flags

	c.Masteries = domain.DefaultMasteries(domain.ResolveCharacterRaceKey(c))
	for i := range c.Masteries {
		c.Masteries[i].Level = levelCap
	}

	race := uint8(domain.NativeCountryByte9C(c))
	learned := slices.Clone(base)
	for _, id := range FullSkills(skills, race, c.Masteries) {
		if !slices.Contains(learned, id) {
			learned = append(learned, id)
		}
	}
	c.Skills = learned

	spawn, rebirth := pvpserver.SamarkandSpawn(), pvpserver.SamarkandSpawn()
	if c.World == nil {
		c.World = &domain.CharacterWorld{}
	}
	c.World.Spawn = &spawn
	c.World.RebirthPoint = &rebirth
	c.World.SpawnSet = true
}
