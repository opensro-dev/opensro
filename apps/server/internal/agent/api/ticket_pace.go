/*
===========================================================================

ticket_pace.go - how fast one account may be handed world tickets

Every world connection costs a transport admission ticket and every
world entry an EnterWorld ticket, and an admitted entry is the most
expensive thing a client can ask the GameWorld for: the character's
load, its spawn and everyone's view of it. A client that enters, drops
its socket and enters again as fast as it can would otherwise buy one
full entry per loop. Both mints are paced per account, so past the
allowance a loop costs one map read and a 429, and nothing reaches the
world.

Inference (no native source: the native gateway handed out no HTTP
tickets; its reconnect was a person at the login screen): the
allowance covers what a person can do. A page reload is one ticket of
each kind; a lost connection retries inside the 30 s resume grace at
the client's back-off (250 ms doubling to 4 s), at most a dozen
transport tickets. The bursts cover that, and the refill covers a
player who reconnects all evening. The client already retries a
refused mint that is not definitive (RATE_LIMITED is not), so a
player who does pass the allowance waits a few seconds, never fails.

===========================================================================
*/

package agentapi

import (
	"net/http"
	"strconv"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/pacer"
)

const (
	// transportTicketBurst and transportTicketRefill pace
	// /auth/transport-token per account: one per connection, resumes
	// included.
	transportTicketBurst  = 12
	transportTicketRefill = 5 * time.Second

	// enterWorldTicketBurst and enterWorldTicketRefill pace
	// /auth/enterworld-token per account: one per world entry. The
	// sustained rate bounds the world entries a looping client buys to
	// one every ten seconds.
	enterWorldTicketBurst  = 6
	enterWorldTicketRefill = 10 * time.Second

	// maxTicketAccounts bounds the accounts each pacer remembers.
	maxTicketAccounts = 8192

	// ticketRateLimitedCode is the refusal's code. The client's retry
	// rule keeps retrying it (it is not in DEFINITIVE_REFUSALS).
	ticketRateLimitedCode = "RATE_LIMITED"
)

/*
================
newTicketPacers

The two mints' pacers, on the API's clock.
================
*/
func newTicketPacers(now func() time.Time) (transport, enterWorld *pacer.Keyed) {
	transport = pacer.New(pacer.Config{
		Burst:   transportTicketBurst,
		Refill:  transportTicketRefill,
		MaxKeys: maxTicketAccounts,
		Now:     now,
	})
	enterWorld = pacer.New(pacer.Config{
		Burst:   enterWorldTicketBurst,
		Refill:  enterWorldTicketRefill,
		MaxKeys: maxTicketAccounts,
		Now:     now,
	})
	return transport, enterWorld
}

/*
================
admitTicket

Takes one of the account's tickets from tickets. A refusal answers 429
with Retry-After and the RATE_LIMITED code, writes nothing else (no
log line, no history: a loop must not buy those either) and returns
false. Runs before any other work of the mint.
================
*/
func admitTicket(w http.ResponseWriter, tickets *pacer.Keyed, accountID string) bool {
	allowed, wait := tickets.Take(domain.FoldAccountID(accountID))
	if allowed {
		return true
	}
	seconds := int((wait + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "code": ticketRateLimitedCode})
	return false
}
