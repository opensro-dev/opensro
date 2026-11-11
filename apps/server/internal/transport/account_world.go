/*
===========================================================================

account_world.go - one account, one character in the world

The world seat: an account holds at most one character in the world.
EnterWorld claims the seat before admission (ClaimAccountWorld), so a
second session of the account entering a different character is
refused instead of joining the first in the world. The seat ends when
its session retires or loses its gameplay context; a claim whose
entry fails is released back to the holder it replaced.

===========================================================================
*/

package transport

import "strings"

/*
================
AccountWorldClaim

One account's character in the world: the session that entered it and
the bind key (division:character) it entered under. The session that
holds the seat claims it at EnterWorld, before admission, so a second
session of the account entering a different character is refused
instead of joining the first in the world - the gateway's
already-connected refusal. previous is the same-key claim this one
replaced, kept so a failed entry hands the seat back to the holder it
displaced (a replacement bind whose build then fails).
================
*/
type AccountWorldClaim struct {
	session  *Session
	account  string
	key      string
	previous *AccountWorldClaim
}

/*
================
ClaimAccountWorld

Claims the account's one world seat for the entering character.
Refused (the second result) only when another session of the account
holds the seat for a different character; the same character takes
the seat over, and the holder claiming again is a no-op. A nil claim
means no claim was needed: the session carries no admission identity,
or it already holds the seat for this character. Release a non-nil
claim with ReleaseAccountWorld when the entry fails; a live claim
ends when its session retires or loses its gameplay context.
================
*/
func (h *Hub) ClaimAccountWorld(s *Session, divisionID, characterName string) (*AccountWorldClaim, bool) {
	account, _, ok := s.AdmissionIdentity()
	if !ok {
		return nil, false
	}
	key := divisionID + ":" + strings.ToLower(characterName)
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.accountWorld[account]
	if current != nil && current.session == s && current.key == key {
		return nil, false
	}
	if current != nil && current.session != s && current.key != key {
		return nil, true
	}
	claim := &AccountWorldClaim{session: s, account: account, key: key, previous: current}
	h.accountWorld[account] = claim
	return claim, false
}

/*
================
ReleaseAccountWorld

Ends a claim taken by ClaimAccountWorld: the seat returns to the
holder this claim replaced when that session is still live, and is
freed otherwise. Releasing a claim the seat has already moved past
(the holder retired, a later claim took over) changes nothing.
================
*/
func (h *Hub) ReleaseAccountWorld(claim *AccountWorldClaim) {
	if claim == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.accountWorld[claim.account] != claim {
		return
	}
	if previous := claim.previous; previous != nil && previous.session != nil && !sessionClosed(previous.session) {
		h.accountWorld[claim.account] = previous
		return
	}
	delete(h.accountWorld, claim.account)
}

/*
================
releaseAccountWorldOf

Drops every world seat the session holds (its teardown, or the loss
of its gameplay context). Never touches another session's seat.
================
*/
func (h *Hub) releaseAccountWorldOf(s *Session) {
	h.mu.Lock()
	for account, claim := range h.accountWorld {
		if claim.session == s {
			delete(h.accountWorld, account)
		}
	}
	h.mu.Unlock()
}

/*
================
sessionClosed

Whether the session's close has begun (its Done channel closed).
================
*/
func sessionClosed(s *Session) bool {
	select {
	case <-s.Done():
		return true
	default:
		return false
	}
}
