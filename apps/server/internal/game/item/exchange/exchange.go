/*
===========================================================================

exchange.go - the player-to-player exchanges of every division

Package exchange owns who is trading with whom and what each side has
put on the table (ExchangeMgr in the GameServer). A request waits for the
target's answer (the 0x3393 kind-1 prompt, 30 s like every proposal); an
accepted request opens a session of two sides. Each side offers up to
twelve bag slots (CIFExchange holds 12 per side, 6B2DA0) and an amount of
gold, then confirms (locks) its offer and, once both have confirmed,
approves; the second approval commits the swap.

Offers name bag slots: the items stay in their owners' bags until the
swap, so the commit re-reads both bags. The package keeps no inventory.

===========================================================================
*/
package exchange

import (
	"errors"
	"strings"
	"sync"
)

const (
	// Slots is the offer capacity of one side (CIFExchange, 6B2DA0).
	Slots = 12
	// AnswerWindowMs is the proposal timeout every transaction shares
	// (Transaction_Construct 46C6C0 stores 30 s).
	AnswerWindowMs = 30 * 1000
)

var (
	ErrNoSession = errors.New("exchange: no session")
	ErrLocked    = errors.New("exchange: offer confirmed")
	ErrFull      = errors.New("exchange: offer full")
	ErrOffered   = errors.New("exchange: slot already offered")
	ErrNotOffer  = errors.New("exchange: no such offer")
	ErrNotReady  = errors.New("exchange: both sides must confirm first")
)

/*
================
Offer

One offered bag slot and what it held when offered; the commit refuses a
slot whose item changed since.
================
*/
type Offer struct {
	BagSlot  uint8
	RefObjID uint32
	Quantity uint16
}

/*
================
Side
================
*/
type Side struct {
	Name      string
	Offers    [Slots]*Offer
	Gold      uint64
	Confirmed bool
	Approved  bool
}

/*
================
Session

A and B are the requester and the target.
================
*/
type Session struct {
	A, B Side
}

/*
================
Request
================
*/
type Request struct {
	From        string
	expiresAtMs int64
}

/*
================
Registry
================
*/
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*Session
	requests map[string]Request
}

/*
================
New
================
*/
func New() *Registry {
	return &Registry{sessions: map[string]*Session{}, requests: map[string]Request{}}
}

/*
================
key
================
*/
func key(divisionID, name string) string {
	return divisionID + "\x00" + strings.ToLower(name)
}

//============================================================================

/*
================
Propose

Parks a request for target from requester.
================
*/
func (r *Registry) Propose(divisionID, requester, target string, nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests[key(divisionID, target)] = Request{From: requester, expiresAtMs: nowMs + AnswerWindowMs}
}

/*
================
Pending

A request still inside its answer window waits for target.
================
*/
func (r *Registry) Pending(divisionID, target string, nowMs int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.requests[key(divisionID, target)]
	if ok && nowMs > request.expiresAtMs {
		delete(r.requests, key(divisionID, target))
		return false
	}
	return ok
}

/*
================
TakeRequest
================
*/
func (r *Registry) TakeRequest(divisionID, target string, nowMs int64) (Request, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(divisionID, target)
	request, ok := r.requests[k]
	delete(r.requests, k)
	if !ok || nowMs > request.expiresAtMs {
		return Request{}, false
	}
	return request, true
}

/*
================
DropRequest
================
*/
func (r *Registry) DropRequest(divisionID, target string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(divisionID, target)
	_, ok := r.requests[k]
	delete(r.requests, k)
	return ok
}

//============================================================================

/*
================
Open

Starts the session of requester and target.
================
*/
func (r *Registry) Open(divisionID, requester, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := &Session{A: Side{Name: requester}, B: Side{Name: target}}
	r.sessions[key(divisionID, requester)] = session
	r.sessions[key(divisionID, target)] = session
}

/*
================
Trading

Whether a character sits in a session.
================
*/
func (r *Registry) Trading(divisionID, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.sessions[key(divisionID, name)]
	return ok
}

/*
================
Snapshot

A copy of the session and which side name holds (own, partner).
================
*/
func (r *Registry) Snapshot(divisionID, name string) (own, partner Side, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[key(divisionID, name)]
	if !ok {
		return Side{}, Side{}, false
	}
	mine, theirs := session.sides(name)
	return copySide(*mine), copySide(*theirs), true
}

/*
================
copySide
================
*/
func copySide(side Side) Side {
	out := side
	for i, offer := range side.Offers {
		if offer != nil {
			copied := *offer
			out.Offers[i] = &copied
		}
	}
	return out
}

/*
================
sides
================
*/
func (s *Session) sides(name string) (own, partner *Side) {
	if strings.EqualFold(s.A.Name, name) {
		return &s.A, &s.B
	}
	return &s.B, &s.A
}

/*
================
Close

Ends a character's session and returns the partner's name.
================
*/
func (r *Registry) Close(divisionID, name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[key(divisionID, name)]
	if !ok {
		return "", false
	}
	_, partner := session.sides(name)
	delete(r.sessions, key(divisionID, session.A.Name))
	delete(r.sessions, key(divisionID, session.B.Name))
	return partner.Name, true
}

//============================================================================

/*
================
mutate

Runs fn on the character's side while it may still change its offer.
================
*/
func (r *Registry) mutate(divisionID, name string, fn func(own *Side) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[key(divisionID, name)]
	if !ok {
		return ErrNoSession
	}
	own, _ := session.sides(name)
	if own.Confirmed {
		return ErrLocked
	}
	return fn(own)
}

/*
================
AddItem

Offers a bag slot in the first free exchange slot, returning that slot.
================
*/
func (r *Registry) AddItem(divisionID, name string, offer Offer) (uint8, error) {
	var slot uint8
	err := r.mutate(divisionID, name, func(own *Side) error {
		free := -1
		for i, offered := range own.Offers {
			if offered != nil && offered.BagSlot == offer.BagSlot {
				return ErrOffered
			}
			if offered == nil && free < 0 {
				free = i
			}
		}
		if free < 0 {
			return ErrFull
		}
		placed := offer
		own.Offers[free] = &placed
		slot = uint8(free)
		return nil
	})
	return slot, err
}

/*
================
RemoveItem
================
*/
func (r *Registry) RemoveItem(divisionID, name string, slot uint8) error {
	return r.mutate(divisionID, name, func(own *Side) error {
		if int(slot) >= Slots || own.Offers[slot] == nil {
			return ErrNotOffer
		}
		own.Offers[slot] = nil
		return nil
	})
}

/*
================
SetGold
================
*/
func (r *Registry) SetGold(divisionID, name string, gold uint64) error {
	return r.mutate(divisionID, name, func(own *Side) error {
		own.Gold = gold
		return nil
	})
}

/*
================
Confirm

Locks the character's offer.
================
*/
func (r *Registry) Confirm(divisionID, name string) error {
	return r.mutate(divisionID, name, func(own *Side) error {
		own.Confirmed = true
		return nil
	})
}

/*
================
Approve

Records the character's approval; both reports that the partner had
approved already, so the swap commits now.
================
*/
func (r *Registry) Approve(divisionID, name string) (both bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[key(divisionID, name)]
	if !ok {
		return false, ErrNoSession
	}
	own, partner := session.sides(name)
	if !own.Confirmed || !partner.Confirmed {
		return false, ErrNotReady
	}
	own.Approved = true
	return partner.Approved, nil
}
