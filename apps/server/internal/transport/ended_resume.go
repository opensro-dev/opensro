/*
===========================================================================

ended_resume.go - a resume of an evicted session hears why it ended

A session evicted by a newer login (single-session bind, the account
session limit) or by an account eviction may be detached when it loses:
then no BYE can reach its client, which is still in its reconnect loop.
Even an attached loser's BYE can lose the race against its drain cap.
Either way the client came back with its token, found it unknown, got a
fresh session and evicted the winner: on 11-03 most of the live
"resume token presented but unknown" lines were this ping-pong between
two devices or two tabs of one account. The hub now remembers why such a
token ended and answers its resume with that BYE, which the client
treats as final ("This session was replaced by another login"). Native
has no resume; its second login simply disconnects the first, and the
first stays disconnected - this keeps that outcome.

===========================================================================
*/
package transport

import (
	"crypto/subtle"
	"time"
)

// endedTokenGraces is how many resume grace periods an evicted token is
// remembered: a client keeps retrying for one grace period after its last
// contact, so two cover a loser evicted while detached near its end.
const endedTokenGraces = 2

type endedToken struct {
	reason uint8
	until  time.Time
}

/*
================
rememberEndedLocked

Record an evicted session's token at teardown, and drop the expired
records so the map stays bounded by the evictions of the last window.
Caller holds h.mu.
================
*/
func (h *Hub) rememberEndedLocked(s *Session, now time.Time) {
	for token, e := range h.ended {
		if now.After(e.until) {
			delete(h.ended, token)
		}
	}
	h.ended[s.resumeToken] = endedToken{
		reason: uint8(s.evictReason.Load()),
		until:  now.Add(endedTokenGraces * h.cfg.GracePeriod),
	}
}

/*
================
endedResume

The BYE reason a presented token ended with, when it belonged to an
evicted session: one still draining, or one torn down within the window.
================
*/
func (h *Hub) endedResume(token [ResumeTokenLen]byte, now time.Time) (uint8, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if sess, ok := h.byToken[token]; ok && sess.evicted.Load() &&
		subtle.ConstantTimeCompare(sess.resumeToken[:], token[:]) == 1 {
		return uint8(sess.evictReason.Load()), true
	}
	if e, ok := h.ended[token]; ok && !now.After(e.until) {
		return e.reason, true
	}
	return 0, false
}
