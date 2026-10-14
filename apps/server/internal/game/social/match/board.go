package match

import "sync"

// PageRows is how many rows one listing page carries. Retail's page size
// is not pinned; 12 matches the window's slot-row count (candidateRows7e4
// / the mentor twin both bind 12 rows; the client scrolls past 12, so any
// u8 rowCount parses - this is server policy, not a wire pin).
const PageRows = 12

// PartyEntry is one party-match registration. Per the earlier finding
// the client's listing row (CPartyRegData, sub_75e5e0's per-row parse)
// IS the registration snapshot struct, so the registry holds exactly the
// row fields. MasterName / RaceByte are SERVER-derived from the bound
// character (identity is never client-supplied); MemberCount comes from
// the party registry seam when the owner is partied, 1 otherwise (the
// client's own ack writer uses the same no-active-party fallback,
// sub_80e6b0). The stored count is only the registration's; every page
// re-reads it live (handlePartyPage).
type PartyEntry struct {
	EntryID     uint32
	PartyNumber uint32
	MasterName  string
	RaceByte    uint8
	MemberCount uint8
	TypeBits    uint8
	Purpose     uint8
	MinLevel    uint8
	MaxLevel    uint8
	Title       string
}

// MentorEntry is one mentor-match registration - the 0xB701
// MatchingCandidateEntry row. Requester / RefObjID / Level are
// SERVER-derived from the bound character. The window renders byte09 /
// dword08 as the level pair "%d(%d)" (both carry the character level
// here), dword14 as the student count, dword18+dword1c+1 as the member
// count "x/8" and dword20 as the honor grade - the camp/honor scalars
// stay 0 until a training-camp lane holds real state.
type MentorEntry struct {
	EntryID   uint32
	Kind      uint8
	Detail    string
	Dword04   uint32
	LevelAlt  uint8
	Level     uint8
	RefObjID  uint32
	Requester string
	Dword14   uint32
	Grade     uint8
	Dword18   uint32
	Dword1C   uint32
}

// boardEntry pairs a registration with its owner key and division so the
// listing can scope to the requester's division and put the own row
// first.
type boardEntry[E any] struct {
	ownerKey string
	division string
	entry    E
}

// Board is the in-memory match board: both registries behind one mutex.
// Session-scoped by design - a process reboot clears it, and the e2e
// asserts exactly that. One registration per character per system
// (ownerKey = division + ":" + lowercased name, the hub bind-key shape).
type Board struct {
	mu     sync.Mutex
	nextID uint32
	party  []boardEntry[PartyEntry]
	mentor []boardEntry[MentorEntry]
}

// NewBoard returns an empty board. Entry ids are board-unique across
// both systems and start at 1 (0 is the client's "no registration"
// snapshot state).
func NewBoard() *Board {
	return &Board{nextID: 1}
}

// allocID hands out the next entry id. Caller holds b.mu.
func (b *Board) allocID() uint32 {
	id := b.nextID
	b.nextID++
	return id
}

// RegisterParty inserts a party-match registration for ownerKey,
// assigning the entry id. It refuses (ok=false) when the owner already
// holds one - the client modifies through 0x73DC instead, and the
// refusal stays silent because the flag-2 error codes are unpinned.
func (b *Board) RegisterParty(division, ownerKey string, entry PartyEntry) (PartyEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.party {
		if existing.ownerKey == ownerKey {
			return PartyEntry{}, false
		}
	}
	entry.EntryID = b.allocID()
	b.party = append(b.party, boardEntry[PartyEntry]{ownerKey: ownerKey, division: division, entry: entry})
	return entry, true
}

// ModifyParty overwrites the owner's registration in place, keeping its
// entry id. Refuses when the owner holds none.
func (b *Board) ModifyParty(ownerKey string, entry PartyEntry) (PartyEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, existing := range b.party {
		if existing.ownerKey == ownerKey {
			entry.EntryID = existing.entry.EntryID
			b.party[i].entry = entry
			return entry, true
		}
	}
	return PartyEntry{}, false
}

// DeleteParty removes the owner's registration when entryID names it.
// Refuses on a miss or a foreign id - only the owner deletes their row.
func (b *Board) DeleteParty(ownerKey string, entryID uint32) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, existing := range b.party {
		if existing.ownerKey == ownerKey && existing.entry.EntryID == entryID {
			b.party = append(b.party[:i], b.party[i+1:]...)
			return true
		}
	}
	return false
}

// PartyEntryByID resolves one party listing by entry id within the
// requester's division (the join lane's target resolve - a foreign
// division's id must NOT resolve, mirroring the page scoping). The
// owner's exact name is the entry's own MasterName (SERVER-derived at
// registration, never client-supplied).
func (b *Board) PartyEntryByID(division string, entryID uint32) (PartyEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.party {
		if existing.division == division && existing.entry.EntryID == entryID {
			return existing.entry, true
		}
	}
	return PartyEntry{}, false
}

// MentorEntryByID resolves one mentor listing by entry id within the
// requester's division (PartyEntryByID's twin; the owner's exact name
// is the entry's Requester).
func (b *Board) MentorEntryByID(division string, entryID uint32) (MentorEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.mentor {
		if existing.division == division && existing.entry.EntryID == entryID {
			return existing.entry, true
		}
	}
	return MentorEntry{}, false
}

// PartyPage composes one listing page for the requester: the own row
// first (the client's row-0 own split turns it into the own snapshot),
// then the division's other rows in registration order, sliced into
// PageRows pages. page is 1-based; 0 and out-of-range clamp.
func (b *Board) PartyPage(division, ownerKey string, page uint8) (curPage, pageCount uint8, rows []PartyEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ordered := make([]PartyEntry, 0, len(b.party))
	for _, existing := range b.party {
		if existing.division == division && existing.ownerKey == ownerKey {
			ordered = append(ordered, existing.entry)
			break
		}
	}
	for _, existing := range b.party {
		if existing.division == division && existing.ownerKey != ownerKey {
			ordered = append(ordered, existing.entry)
		}
	}
	curPage, pageCount, lo, hi := pageBounds(len(ordered), page)
	return curPage, pageCount, ordered[lo:hi]
}

// RegisterMentor inserts a mentor-match registration for ownerKey,
// assigning the entry id. Refuses when the owner already holds one.
func (b *Board) RegisterMentor(division, ownerKey string, entry MentorEntry) (MentorEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.mentor {
		if existing.ownerKey == ownerKey {
			return MentorEntry{}, false
		}
	}
	entry.EntryID = b.allocID()
	b.mentor = append(b.mentor, boardEntry[MentorEntry]{ownerKey: ownerKey, division: division, entry: entry})
	return entry, true
}

// ModifyMentor overwrites the owner's registration in place, keeping its
// entry id. Refuses when the owner holds none.
func (b *Board) ModifyMentor(ownerKey string, entry MentorEntry) (MentorEntry, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, existing := range b.mentor {
		if existing.ownerKey == ownerKey {
			entry.EntryID = existing.entry.EntryID
			b.mentor[i].entry = entry
			return entry, true
		}
	}
	return MentorEntry{}, false
}

// DeleteMentor removes the owner's registration when entryID names it.
func (b *Board) DeleteMentor(ownerKey string, entryID uint32) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, existing := range b.mentor {
		if existing.ownerKey == ownerKey && existing.entry.EntryID == entryID {
			b.mentor = append(b.mentor[:i], b.mentor[i+1:]...)
			return true
		}
	}
	return false
}

// PurgeOwner removes the owner's rows from BOTH the party board and the
// mentor board - the disconnect/rebind cleanup leg (the party registry's
// Leave twin). No frames ride a purge: other clients see the row gone on
// their next page request, and no removal push is pinned. Reports
// whether anything was removed.
func (b *Board) PurgeOwner(ownerKey string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	removed := false
	for i, existing := range b.party {
		if existing.ownerKey == ownerKey {
			b.party = append(b.party[:i], b.party[i+1:]...)
			removed = true
			break
		}
	}
	for i, existing := range b.mentor {
		if existing.ownerKey == ownerKey {
			b.mentor = append(b.mentor[:i], b.mentor[i+1:]...)
			removed = true
			break
		}
	}
	return removed
}

// MentorPage composes one mentor listing page for the requester - the
// same own-row-first policy as PartyPage (the client's row-0 own split
// compares the requester name).
func (b *Board) MentorPage(division, ownerKey string, page uint8) (curPage, pageCount uint8, rows []MentorEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ordered := make([]MentorEntry, 0, len(b.mentor))
	for _, existing := range b.mentor {
		if existing.division == division && existing.ownerKey == ownerKey {
			ordered = append(ordered, existing.entry)
			break
		}
	}
	for _, existing := range b.mentor {
		if existing.division == division && existing.ownerKey != ownerKey {
			ordered = append(ordered, existing.entry)
		}
	}
	curPage, pageCount, lo, hi := pageBounds(len(ordered), page)
	return curPage, pageCount, ordered[lo:hi]
}

// pageBounds clamps a 1-based page request against total rows and
// returns the slice window. An empty board answers page 1 of 1 with no
// rows (the client's empty install clears rows AND the own snapshot).
func pageBounds(total int, page uint8) (curPage, pageCount uint8, lo, hi int) {
	pages := (total + PageRows - 1) / PageRows
	if pages < 1 {
		pages = 1
	}
	requested := int(page)
	if requested < 1 {
		requested = 1
	}
	if requested > pages {
		requested = pages
	}
	lo = (requested - 1) * PageRows
	hi = lo + PageRows
	if hi > total {
		hi = total
	}
	if lo > total {
		lo = total
	}
	return uint8(requested), uint8(pages), lo, hi
}
