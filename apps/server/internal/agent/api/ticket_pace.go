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

Each request the pacer decides is counted once (noteTicket, #3307): a
shard-wide total per kind that the observatory capture shows, and -
when the census runs - the account's hour bucket in the connection
journal (connjournal/tickets.go). A refusal still writes no log line
and no history event; it costs one counter.

===========================================================================
*/

package agentapi

import (
	"net/http"
	"strconv"
	"sync/atomic"
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
TicketKind

Which of the two mints a request asked for.
================
*/
type TicketKind uint8

const (
	// TicketTransport is /auth/transport-token: one per connection.
	TicketTransport TicketKind = iota
	// TicketEnterWorld is /auth/enterworld-token: one per world entry.
	TicketEnterWorld
	ticketKinds
)

/*
================
TicketTotals

The shard's ticket outcomes since start, per kind: minted, and refused
by the pacer. No account appears here; the census keeps those.
================
*/
type TicketTotals struct {
	TransportMinted  uint64 `json:"transportMinted"`
	TransportPaced   uint64 `json:"transportPaced"`
	EnterWorldMinted uint64 `json:"enterWorldMinted"`
	EnterWorldPaced  uint64 `json:"enterWorldPaced"`
}

/*
================
ticketTally

The running totals behind TicketTotals, and the census's per-account
observer (nil while the census is off).
================
*/
type ticketTally struct {
	minted   [ticketKinds]atomic.Uint64
	paced    [ticketKinds]atomic.Uint64
	observer func(account string, kind TicketKind, minted bool)
}

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

Takes one of the account's tickets of kind. A refusal answers 429 with
Retry-After and the RATE_LIMITED code, is counted (noteTicket), writes
nothing else (no log line, no history: a loop must not buy those
either) and returns false. Runs before any other work of the mint.
================
*/
func (api *API) admitTicket(w http.ResponseWriter, kind TicketKind, accountID string) bool {
	tickets := api.transportTickets
	if kind == TicketEnterWorld {
		tickets = api.enterWorldTickets
	}
	allowed, wait := tickets.Take(domain.FoldAccountID(accountID))
	if allowed {
		return true
	}
	api.noteTicket(accountID, kind, false)
	seconds := int((wait + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "code": ticketRateLimitedCode})
	return false
}

/*
================
noteTicket

Counts one decided request: minted after the mint succeeded, or paced
when admitTicket refused it. A request refused for any other reason
(maintenance, an unknown character, a failed mint) mints nothing and is
not a ticket outcome; those keep their own log lines.
================
*/
func (api *API) noteTicket(accountID string, kind TicketKind, minted bool) {
	if minted {
		api.tickets.minted[kind].Add(1)
	} else {
		api.tickets.paced[kind].Add(1)
	}
	if api.tickets.observer != nil {
		api.tickets.observer(accountID, kind, minted)
	}
}

/*
================
InstallTicketObserver

The census's per-account count of every ticket outcome
(connjournal.RecordTicket through the composition root). Installed
before the listeners start, like the other Install hooks; it must not
block.
================
*/
func (api *API) InstallTicketObserver(observer func(account string, kind TicketKind, minted bool)) {
	api.tickets.observer = observer
}

/*
================
TicketTotals

The shard-wide ticket totals for the observatory capture.
================
*/
func (api *API) TicketTotals() TicketTotals {
	return TicketTotals{
		TransportMinted:  api.tickets.minted[TicketTransport].Load(),
		TransportPaced:   api.tickets.paced[TicketTransport].Load(),
		EnterWorldMinted: api.tickets.minted[TicketEnterWorld].Load(),
		EnterWorldPaced:  api.tickets.paced[TicketEnterWorld].Load(),
	}
}
