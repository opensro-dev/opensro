/*
===========================================================================

account_world.go - one account, one character in the world

The world seat: an account holds at most one character in the world.
EnterWorld claims the seat before admission (ClaimAccountWorld), so a
second session of the account entering a different character is
refused instead of joining the first in the world. However a claim
ends - its entry fails, its session retires, or it loses its gameplay
context - the seat returns to a live holder the claim replaced.

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
replaced, kept so an entry that ends before it publishes hands the
seat back to the holder it displaced; publishing cuts the link
(AccountWorldClaimBound).
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

Inference (no native source: the original answered the overlap
against a second live connection, and the port's resume grace has no
native counterpart): a holder whose socket has dropped is not a live
second connection. A fresh entry for a different character of the
account retires such a holder and takes the seat - the same end as a
lapsed grace, resume token included - while a holder with a live
socket still refuses. A resume that wins the race re-stamps the seat
or re-enters under it, and the loop below re-reads the seat either
way, so the two never stand in the world together.
================
*/
func (h *Hub) ClaimAccountWorld(s *Session, divisionID, characterName string) (*AccountWorldClaim, bool) {
	// Port-only, not native: with the closed beta's multi-character
	// switch an account has no single seat, so nothing is claimed or
	// refused. The same character entered twice is still one: its bind
	// key evicts the older session (BindExclusive).
	if h.cfg.AccountMultiWorld {
		return nil, false
	}
	account, _, ok := s.AdmissionIdentity()
	if !ok {
		return nil, false
	}
	key := divisionID + ":" + strings.ToLower(characterName)
	for {
		h.mu.Lock()
		current := h.accountWorld[account]
		switch {
		case current == nil || sessionClosed(current.session):
			claim := &AccountWorldClaim{session: s, account: account, key: key, previous: current}
			h.accountWorld[account] = claim
			h.mu.Unlock()
			return claim, false
		case current.session == s && current.key == key:
			h.mu.Unlock()
			return nil, false
		case current.key == key:
			// The same character takes the seat over; eviction at
			// bind handles the holder, detached or live.
			claim := &AccountWorldClaim{session: s, account: account, key: key, previous: current}
			h.accountWorld[account] = claim
			h.mu.Unlock()
			return claim, false
		case current.session == s:
			// Our own seat under another character (a re-entry that
			// never lost its gameplay context): take it over.
			claim := &AccountWorldClaim{session: s, account: account, key: key, previous: current}
			h.accountWorld[account] = claim
			h.mu.Unlock()
			return claim, false
		}
		// A different character holds the seat. Refuse a live holder;
		// retire a detached one and re-read the seat.
		holder := current.session
		h.mu.Unlock()
		if holder.Kind() != "detached" {
			return nil, true
		}
		holder.expireIfDetached()
	}
}

/*
================
AccountWorldClaimBound

Marks a claim's entry as published: the seat is the claim's
outright, and however the claim later ends it frees the seat instead
of handing it back to the holder it replaced (that holder's world
was displaced by the bind). A claim that never publishes keeps its
hand-back. A claim the seat has already moved past is untouched.
================
*/
func (h *Hub) AccountWorldClaimBound(claim *AccountWorldClaim) {
	if claim == nil {
		return
	}
	h.mu.Lock()
	if h.accountWorld[claim.account] == claim {
		claim.previous = nil
	}
	h.mu.Unlock()
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
	h.settleAccountWorldClaimLocked(claim, nil)
}

/*
================
settleAccountWorldClaimLocked

(h.mu held.) Settles the seat one claim holds: the seat returns to
the holder the claim replaced when that session still stands in the
world, and is freed otherwise. Still standing means live, not
closed, and not evicted: a completed takeover latches the loser
evicted at bind (BindExclusive), so its seat is gone even while its
teardown drains; a replacement that dies before its bind evicted
nobody, and the first holder's seat is still its. notTo, when
non-nil, is a session the seat must never return to: a teardown
hands the seat back to a displaced holder, never to the session
going away.
================
*/
func (h *Hub) settleAccountWorldClaimLocked(claim *AccountWorldClaim, notTo *Session) {
	if previous := claim.previous; previous != nil && previous.session != nil &&
		previous.session != notTo && !previous.session.Evicted() && !sessionClosed(previous.session) {
		h.accountWorld[claim.account] = previous
		return
	}
	delete(h.accountWorld, claim.account)
}

/*
================
releaseAccountWorldOf

Settles every world seat the session holds (its teardown, or the
loss of its gameplay context): handed back to a live holder the
claim replaced, freed otherwise. Never touches another session's
seat.
================
*/
func (h *Hub) releaseAccountWorldOf(s *Session) {
	h.mu.Lock()
	h.releaseAccountWorldOfLocked(s)
	h.mu.Unlock()
}

/*
================
releaseAccountWorldOfLocked

(h.mu held.) The locked half of releaseAccountWorldOf, for the
teardown that already holds the hub lock.
================
*/
func (h *Hub) releaseAccountWorldOfLocked(s *Session) {
	for _, claim := range h.accountWorld {
		if claim.session == s {
			h.settleAccountWorldClaimLocked(claim, s)
		}
	}
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
