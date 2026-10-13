package movement

import "opensro.online/server/internal/domain"

// Dependencies is the character authority surface consumed by movement.
type Dependencies interface {
	domain.CharacterSource
	CharacterModelRef(character *domain.Character) uint32
	CharacterBodyRadius(character *domain.Character) (float64, bool)
	Mutate(character *domain.Character, label string, fn func())
	Update(character *domain.Character, label string, update func() bool) bool
	// UpdateDeferred joins the tick's batched commit; tick-only paths only.
	UpdateDeferred(character *domain.Character, label string, update func() bool) bool
	Read(divisionID string, fn func())
	GuildAuthority() domain.GuildStore
}
