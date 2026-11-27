package community

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/transport"
)

// Dependencies is the complete authority surface the community lane consumes.
// The lane cannot see bootstrap-only catalogs, movement hooks, or unrelated
// social stores.
type Dependencies interface {
	domain.CharacterSource
	Update(character *domain.Character, label string, update func() bool) bool
	UpdateMany(characters []*domain.Character, label string, update func() bool) bool
	Read(divisionID string, fn func())
	LetterAuthority() domain.LetterStore
	GuildAuthority() domain.GuildStore
	CharacterModelRef(character *domain.Character) uint32
}

// Presence is the live-session directory consumed by community wire adapters.
// It lives at the consumer boundary; the shared implementation is owned by
// internal/game/social.
type Presence interface {
	SessionByName(divisionID, name string) (*transport.Session, bool)
	OnlineByName(divisionID, name string) bool
}

func presenceSession(presence Presence, divisionID, name string) (*transport.Session, bool) {
	if presence == nil {
		return nil, false
	}
	return presence.SessionByName(divisionID, name)
}

func presenceOnline(presence Presence, divisionID, name string) bool {
	return presence != nil && presence.OnlineByName(divisionID, name)
}
