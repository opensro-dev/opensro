package quest

import "opensro.online/server/internal/domain"

// Dependencies is the character authority surface consumed by quests.
type Dependencies interface {
	domain.CharacterSource
	Update(character *domain.Character, label string, update func() bool) bool
	// UpdateDeferred joins the tick's batched commit; tick-only paths only.
	UpdateDeferred(character *domain.Character, label string, update func() bool) bool
}
