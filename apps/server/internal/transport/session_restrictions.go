/*
===========================================================================

session_restrictions.go - command restrictions: the session record and the
hub's by-character record

A command restriction (trade 3, chat 4; the native command-source record
selectors) rides the session that carries it, mirroring the server's setter and clearer, and
the session record is what the send gates read. Moderation must not end at
logoff, so installing or clearing on a character-bound session also records
against the character in the hub, where by-character readers
(Hub.CharacterRestricted; the site chat feed) find it after the session is
gone. Both records die with the process.

===========================================================================
*/
package transport

import (
	"strings"
	"sync"
)

// These IDs are the native CCmdSrcNet record selectors, not wire notice kinds.
const (
	CommandRestrictionTrade uint8 = 3
	CommandRestrictionChat  uint8 = 4
)

type sessionCommandRestrictions struct {
	mu      sync.RWMutex
	enabled [2]bool
	until   [2][8]uint16
}

// characterRestrictions is the hub's by-character form of the same record:
// one character's restriction flags and stamps, with no session attached.
type characterRestrictions struct {
	enabled [2]bool
	until   [2][8]uint16
}

/*
================
SetCommandRestriction

Mirrors the server's setter: retain the enabled flag and the exact eight SYSTEMTIME
words. No clock interpretation or normalization occurs. Only authenticated
server-side moderation should invoke this method. A character-bound
session also records the install against its character in the hub
(recordCharacterRestriction), so the restriction outlives the session.
================
*/
func (s *Session) SetCommandRestriction(kind uint8, until [8]uint16) bool {
	if kind < 3 || kind > 4 {
		return false
	}
	s.restrictions.mu.Lock()
	s.restrictions.until[kind-3] = until
	s.restrictions.enabled[kind-3] = true
	s.restrictions.mu.Unlock()
	if s.hub != nil {
		s.hub.recordCharacterRestriction(s, kind, until, true)
	}
	return true
}

/*
================
ClearCommandRestriction

Mirrors the server's clearer. Expiry is controlled by the owner that installs/clears
restrictions, not a request handler's local clock. A character-bound
session also clears the hub's by-character record.
================
*/
func (s *Session) ClearCommandRestriction(kind uint8) bool {
	if kind < 3 || kind > 4 {
		return false
	}
	s.restrictions.mu.Lock()
	s.restrictions.enabled[kind-3] = false
	s.restrictions.mu.Unlock()
	if s.hub != nil {
		s.hub.recordCharacterRestriction(s, kind, [8]uint16{}, false)
	}
	return true
}

/*
================
CommandRestriction

Returns a detached date only when its record is enabled.
================
*/
func (s *Session) CommandRestriction(kind uint8) (until [8]uint16, enabled bool) {
	if kind < 3 || kind > 4 {
		return until, false
	}
	s.restrictions.mu.RLock()
	defer s.restrictions.mu.RUnlock()
	if !s.restrictions.enabled[kind-3] {
		return until, false
	}
	return s.restrictions.until[kind-3], true
}

/*
================
characterRestrictionKey

The registry key: the session bind-key spelling (division:lower(name), as
worldBound builds it), so a record and its lookup fold a character name
the same way the binding does.
================
*/
func characterRestrictionKey(divisionID, characterName string) string {
	return divisionID + ":" + strings.ToLower(characterName)
}

/*
================
recordCharacterRestriction

Records one session restriction install or clear against the session's
bound character, so the restriction outlives the session. An unbound
session records nothing: with no character there is nobody to record
against, and moderation installs on named (bound) characters. The entry
is dropped once no kind stays enabled.
================
*/
func (h *Hub) recordCharacterRestriction(s *Session, kind uint8, until [8]uint16, enabled bool) {
	divisionID, characterName, ok := s.CharacterBinding()
	if !ok {
		return
	}
	key := characterRestrictionKey(divisionID, characterName)
	h.restrictionMu.Lock()
	defer h.restrictionMu.Unlock()
	rec := h.restrictions[key]
	if rec == nil {
		rec = &characterRestrictions{}
		h.restrictions[key] = rec
	}
	rec.until[kind-3] = until
	rec.enabled[kind-3] = enabled
	if !rec.enabled[0] && !rec.enabled[1] {
		delete(h.restrictions, key)
	}
}

/*
================
CharacterRestricted

Whether characterName in divisionID carries the given command restriction:
the hub's by-character record (an installed restriction outlives its
session) or any live session bound to the character. This is the read a
moderation consumer uses; it never reports the restriction to the player
(restriction.Report does, with its notice).
================
*/
func (h *Hub) CharacterRestricted(divisionID, characterName string, kind uint8) bool {
	if kind < 3 || kind > 4 {
		return false
	}
	key := characterRestrictionKey(divisionID, characterName)
	h.restrictionMu.RLock()
	rec := h.restrictions[key]
	restricted := rec != nil && rec.enabled[kind-3]
	h.restrictionMu.RUnlock()
	if restricted {
		return true
	}
	for _, s := range h.CharacterSessions(divisionID, characterName) {
		if _, enabled := s.CommandRestriction(kind); enabled {
			return true
		}
	}
	return false
}
