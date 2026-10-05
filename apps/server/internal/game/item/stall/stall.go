/*
===========================================================================

stall.go - the street stalls of every division

Package stall owns who keeps a stall, what lies on it and who is looking
(CStallManager / CFleaMarket in the GameServer). A stall has ten slots
(CFleaMarket 472E20 refuses slot >= 10), a title and a greeting of at
most 64 characters (471D90, 472E20 cases 5 and 6), an open flag (case
4: visitors may buy only while it is open) and a mode byte (case 3, < 3).
A slot names one bag slot of the owner, the quantity on offer and its
price; the items stay in the owner's bag until a buyer takes them, so a
sale re-reads the bag. Stalls opened while the stall network is chosen
are listed on it by their items' network categories (fmntidgroupmapdata).

===========================================================================
*/
package stall

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

const (
	// Slots is CFleaMarket's slot count (472E20: slot < 0xA).
	Slots = 10
	// TextLimit is the title and greeting cap (471D90, 472E20).
	TextLimit = 64
	// ModeLimit bounds the mode byte (472E20 case 3: < 3).
	ModeLimit = 3
)

var (
	ErrNoStall     = errors.New("stall: no stall")
	ErrHasStall    = errors.New("stall: already keeping a stall")
	ErrBadSlot     = errors.New("stall: invalid slot")
	ErrFull        = errors.New("stall: every slot is taken")
	ErrOffered     = errors.New("stall: bag slot already offered")
	ErrOpen        = errors.New("stall: open for business")
	ErrClosed      = errors.New("stall: not open")
	ErrNotVisiting = errors.New("stall: not visiting")
)

/*
================
Slot

One offer: the owner's bag slot, what it held when offered, the count on
the table and its unit price.
================
*/
type Slot struct {
	BagSlot  uint8
	RefObjID uint32
	Quantity uint16
	Price    uint32
	Category uint32
	// Serial identifies this offer on the stall network (a network buy
	// names it; a changed offer gets a new one).
	Serial uint64
}

/*
================
Stall
================
*/
type Stall struct {
	Owner    string
	Title    string
	Greeting string
	Open     bool
	Network  bool
	Mode     uint8
	Slots    [Slots]*Slot
	Visitors []string
}

/*
================
Registry
================
*/
type Registry struct {
	mu       sync.Mutex
	stalls   map[string]*Stall
	visiting map[string]string
	serial   uint64
}

/*
================
New
================
*/
func New() *Registry {
	return &Registry{stalls: map[string]*Stall{}, visiting: map[string]string{}}
}

/*
================
key
================
*/
func key(divisionID, name string) string {
	return divisionID + "\x00" + strings.ToLower(name)
}

/*
================
copyStall
================
*/
func copyStall(s *Stall) Stall {
	out := *s
	for i, slot := range s.Slots {
		if slot != nil {
			copied := *slot
			out.Slots[i] = &copied
		}
	}
	out.Visitors = append([]string(nil), s.Visitors...)
	return out
}

//============================================================================

/*
================
Open

Starts a stall for owner, closed for business and empty.
================
*/
func (r *Registry) Open(divisionID, owner, title string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(divisionID, owner)
	if _, ok := r.stalls[k]; ok {
		return ErrHasStall
	}
	r.stalls[k] = &Stall{Owner: owner, Title: title}
	return nil
}

/*
================
Close

Ends owner's stall; returns its last state (the visitors to tell).
================
*/
func (r *Registry) Close(divisionID, owner string) (Stall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(divisionID, owner)
	s, ok := r.stalls[k]
	if !ok {
		return Stall{}, false
	}
	for _, visitor := range s.Visitors {
		delete(r.visiting, key(divisionID, visitor))
	}
	delete(r.stalls, k)
	return copyStall(s), true
}

/*
================
Get
================
*/
func (r *Registry) Get(divisionID, owner string) (Stall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stalls[key(divisionID, owner)]
	if !ok {
		return Stall{}, false
	}
	return copyStall(s), true
}

/*
================
Keeping
================
*/
func (r *Registry) Keeping(divisionID, owner string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.stalls[key(divisionID, owner)]
	return ok
}

/*
================
Visiting

The owner whose stall a player stands at.
================
*/
func (r *Registry) Visiting(divisionID, name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, ok := r.visiting[key(divisionID, name)]
	return owner, ok
}

/*
================
Participants

Everyone at the stall a player keeps or stands at, owner first
(CFleaMarket's participant list, server 473C60); false when the player is
at no stall.
================
*/
func (r *Registry) Participants(divisionID, name string) ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	owner := name
	if visited, ok := r.visiting[key(divisionID, name)]; ok {
		owner = visited
	}
	s, ok := r.stalls[key(divisionID, owner)]
	if !ok {
		return nil, false
	}
	return append([]string{s.Owner}, s.Visitors...), true
}

/*
================
Enter
================
*/
func (r *Registry) Enter(divisionID, visitor, owner string) (Stall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stalls[key(divisionID, owner)]
	if !ok {
		return Stall{}, ErrNoStall
	}
	if _, already := r.visiting[key(divisionID, visitor)]; !already {
		s.Visitors = append(s.Visitors, visitor)
	}
	r.visiting[key(divisionID, visitor)] = s.Owner
	return copyStall(s), nil
}

/*
================
Leave

Takes a visitor away; returns the stall left.
================
*/
func (r *Registry) Leave(divisionID, visitor string) (Stall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, ok := r.visiting[key(divisionID, visitor)]
	if !ok {
		return Stall{}, ErrNotVisiting
	}
	delete(r.visiting, key(divisionID, visitor))
	s, ok := r.stalls[key(divisionID, owner)]
	if !ok {
		return Stall{}, ErrNoStall
	}
	for i, name := range s.Visitors {
		if strings.EqualFold(name, visitor) {
			s.Visitors = append(s.Visitors[:i], s.Visitors[i+1:]...)
			break
		}
	}
	return copyStall(s), nil
}

//============================================================================

/*
================
Edit

Runs fn on owner's stall under the registry lock.
================
*/
func (r *Registry) Edit(divisionID, owner string, fn func(s *Stall) error) (Stall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stalls[key(divisionID, owner)]
	if !ok {
		return Stall{}, ErrNoStall
	}
	if err := fn(s); err != nil {
		return copyStall(s), err
	}
	return copyStall(s), nil
}

/*
================
NextSerial

A new network serial for an offer.
================
*/
func (r *Registry) NextSerial() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serial++
	return r.serial
}

/*
================
Listing

One stall network row.
================
*/
type Listing struct {
	Owner string
	Index uint8
	Slot  Slot
}

/*
================
Network

The offers of a category on every open, networked stall of a division,
in owner then slot order.
================
*/
func (r *Registry) Network(divisionID string, category uint32) []Listing {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := divisionID + "\x00"
	var out []Listing
	for k, s := range r.stalls {
		if !strings.HasPrefix(k, prefix) || !s.Open || !s.Network {
			continue
		}
		for i, slot := range s.Slots {
			if slot != nil && (category == 0 || slot.Category == category) {
				out = append(out, Listing{Owner: s.Owner, Index: uint8(i), Slot: *slot})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Index < out[j].Index
	})
	return out
}
