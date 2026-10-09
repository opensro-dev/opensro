/*
===========================================================================

session_context.go - authenticated character binding and world readiness

===========================================================================
*/
package transport

import "sync"

// sessionPlayerContext owns the game identity and world-snapshot attachment
// whose lifetime is exactly one bound session. The snapshot is intentionally
// opaque to transport; only worldsession interprets its package-owned port.
/*
================
sessionPlayerContext
================
*/
type sessionPlayerContext struct {
	mu            sync.RWMutex
	accountID     string
	accountShard  string
	divisionID    string
	characterName string
	objectID      uint32
	worldReady    bool
	worldSnapshot any
	// rebound: the last BindCharacter named the character this session
	// already carried (a resumed transport's repeated EnterWorld).
	rebound bool
	// releaseProtocol is the browser release protocol the EnterWorld token
	// bound (auth.MintProtocol); 0 until a verified EnterWorld sets it.
	releaseProtocol int
}

// SetReleaseProtocol records the release protocol a verified EnterWorld
// token bound. The game owner picks its protocol-dependent encodings by it;
// transport only carries the number.
/*
================
SetReleaseProtocol
================
*/
func (s *Session) SetReleaseProtocol(protocol int) {
	s.player.mu.Lock()
	defer s.player.mu.Unlock()
	s.player.releaseProtocol = protocol
}

// ReleaseProtocol returns the recorded release protocol, or 0 before a
// verified EnterWorld.
/*
================
ReleaseProtocol
================
*/
func (s *Session) ReleaseProtocol() int {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	return s.player.releaseProtocol
}

// BindAdmissionIdentity installs the HELLO account/shard identity or
// confirms that a resumed session presented the same identity. A mismatch is
// refused without modifying the existing session.
/*
================
BindAdmissionIdentity
================
*/
func (s *Session) BindAdmissionIdentity(accountID, shardID string) bool {
	s.player.mu.Lock()
	defer s.player.mu.Unlock()
	if s.player.accountID == "" && s.player.accountShard == "" {
		s.player.accountID = accountID
		s.player.accountShard = shardID
		return true
	}
	return s.player.accountID == accountID && s.player.accountShard == shardID
}

// AdmissionIdentity returns the authenticated HELLO identity.
/*
================
AdmissionIdentity
================
*/
func (s *Session) AdmissionIdentity() (accountID, shardID string, ok bool) {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	if s.player.accountID == "" || s.player.accountShard == "" {
		return "", "", false
	}
	return s.player.accountID, s.player.accountShard, true
}

// BindCharacter publishes the server-authorized character identity selected by
// EnterWorld. Client-supplied names on later frames are never authoritative.
// objectID is the character's world object id: delivery resolves a
// character's sessions and visibility from the binding alone, so it never
// reads the character store and is safe inside a character door.
/*
================
BindCharacter
================
*/
func (s *Session) BindCharacter(divisionID, characterName string, objectID uint32) {
	s.mu.Lock()
	s.player.mu.Lock()
	s.player.rebound = s.player.divisionID == divisionID && s.player.characterName == characterName
	s.player.divisionID = divisionID
	s.player.characterName = characterName
	s.player.objectID = objectID
	s.player.worldReady = false
	s.player.worldSnapshot = nil
	s.player.mu.Unlock()
	if !s.closed {
		s.recordHistoryLocked("character_selected", nil)
	}
	s.mu.Unlock()
	s.hub.reindexDivision(s)
}

// CharacterBinding returns a coherent copy of the bound identity.
/*
================
CharacterBinding
================
*/
func (s *Session) CharacterBinding() (divisionID, characterName string, ok bool) {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	if s.player.divisionID == "" || s.player.characterName == "" {
		return "", "", false
	}
	return s.player.divisionID, s.player.characterName, true
}

// CharacterObjectID returns the bound character's world object id.
/*
================
CharacterObjectID
================
*/
func (s *Session) CharacterObjectID() (uint32, bool) {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	if s.player.divisionID == "" || s.player.characterName == "" {
		return 0, false
	}
	return s.player.objectID, true
}

// TryMarkWorldReady performs the character-bound -> world-ready transition.
// It returns true exactly once for each BindCharacter lifecycle. Duplicate
// client 0x3012 frames are ignored so they cannot replay bootstrap output or
// re-run world-owned bind hooks such as pending-invite invalidation.
/*
================
TryMarkWorldReady
================
*/
func (s *Session) TryMarkWorldReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.player.mu.Lock()
	if s.player.divisionID == "" || s.player.characterName == "" || s.player.worldReady {
		s.player.mu.Unlock()
		return false
	}
	s.player.worldReady = true
	s.player.mu.Unlock()
	if !s.closed {
		s.recordHistoryLocked("world_entered", nil)
	}
	return true
}

/*
================
Rebound

Whether the current binding repeated the character this session already
carried: a resumed transport re-enters the same character on the same
session, while a first entry or another character is a new admission.
Lanes that keep per-character state across a session (the party) tell
the two apart with this; the client keeps that state across the same
resume.
================
*/
func (s *Session) Rebound() bool {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	return s.player.rebound
}

// WorldReady reports whether this bound character completed scene admission.
/*
================
WorldReady
================
*/
func (s *Session) WorldReady() bool {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	return s.player.worldReady
}

// DivisionID returns the session's current division membership.
/*
================
DivisionID
================
*/
func (s *Session) DivisionID() (string, bool) {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	return s.player.divisionID, s.player.divisionID != ""
}

// SetWorldSnapshot publishes the mission-owned snapshot provider and keeps the
// division index coherent with the provider's world.
/*
================
SetWorldSnapshot
================
*/
func (s *Session) SetWorldSnapshot(divisionID string, snapshot any) {
	s.player.mu.Lock()
	s.player.divisionID = divisionID
	s.player.worldSnapshot = snapshot
	s.player.mu.Unlock()
	s.hub.reindexDivision(s)
}

// WorldSnapshot returns the opaque mission attachment.
/*
================
WorldSnapshot
================
*/
func (s *Session) WorldSnapshot() (any, bool) {
	s.player.mu.RLock()
	defer s.player.mu.RUnlock()
	return s.player.worldSnapshot, s.player.worldSnapshot != nil
}

// ClearGameplayContext removes identity and world visibility together. It is
// used when an exclusive bind loses ownership and during close cleanup.
/*
================
ClearGameplayContext
================
*/
func (s *Session) ClearGameplayContext() {
	s.mu.Lock()
	if !s.closed {
		s.recordHistoryLocked("world_left", nil)
	}
	s.player.mu.Lock()
	s.player.divisionID = ""
	s.player.characterName = ""
	s.player.objectID = 0
	s.player.worldReady = false
	s.player.worldSnapshot = nil
	s.player.rebound = false
	s.player.mu.Unlock()
	s.mu.Unlock()
	s.hub.reindexDivision(s)
}

/*
================
effectiveDivision
================
*/
func (s *Session) effectiveDivision() string {
	divisionID, _ := s.DivisionID()
	return divisionID
}
