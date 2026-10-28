// Package social provides the process-wide live-character directory shared by
// the independent social feature packages below it.
package social

import (
	"opensro.online/server/internal/platform/namekey"
	"opensro.online/server/internal/transport"
)

// Directory is a stateless view over the Hub's exclusive character bindings.
// It owns no parallel session map, so transport lifecycle remains the single
// source of truth for whether a character is online.
type Directory struct {
	hub *transport.Hub
}

// NewDirectory exposes live character bindings from hub.
func NewDirectory(hub *transport.Hub) *Directory {
	return &Directory{hub: hub}
}

// BindKey composes the identity claimed by Hub.BindExclusive; the key's
// shape is defined once in platform/namekey so bindings and the
// live-world store can never drift apart.
func BindKey(divisionID, characterName string) string {
	return namekey.Key(divisionID, characterName)
}

// SessionByName returns the session currently driving a character.
func (directory *Directory) SessionByName(
	divisionID string,
	characterName string,
) (*transport.Session, bool) {
	if directory == nil || directory.hub == nil {
		return nil, false
	}
	return directory.hub.BoundSession(BindKey(divisionID, characterName))
}

// EndSessionByName ends the session driving a character the way a logout
// does (ByeReasonNormal: the departure owner saves and removes them).
// Reports whether a session was found.
func (directory *Directory) EndSessionByName(divisionID, characterName string) bool {
	session, ok := directory.SessionByName(divisionID, characterName)
	if !ok {
		return false
	}
	session.CloseWhenDrained(transport.ByeReasonNormal)
	return true
}

// OnlineByName reports whether a character has a live exclusive binding.
func (directory *Directory) OnlineByName(divisionID, characterName string) bool {
	_, online := directory.SessionByName(divisionID, characterName)
	return online
}
