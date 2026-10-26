/*
===========================================================================

dependencies.go - what progression consumes from its composition

===========================================================================
*/

package progression

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// Dependencies is the character and authored progression-data surface
// consumed by progression.
type Dependencies interface {
	domain.CharacterSource
	Update(character *domain.Character, label string, update func() bool) bool
	ItemReferences() enterworld.ItemRefSource
	LevelData() enterworld.LevelDataSource
	SkillData() enterworld.SkillDataSource
	MagicOptionDefinitions() enterworld.MagicOptionSource
	SetCatalogue() enterworld.SetCatalogueSource
}
