/*
===========================================================================

fortress.go - who holds each fortress, and whether its war is running

Package fortress is the CSiegeFortressMgr state the rest of the server asks
about a fortress: the occupying guild (_SiegeFortress.GuildID), the guild
holding it temporarily during a war (TempGuildID), and the war mode the
entry rules branch on (MainProcess_IsMode42425). It owns no wire and no
schedule; the war lane drives it through its functions.

A fresh shard has every fortress unoccupied and no war running, which is
the state New builds. Occupation changes only when a war ends with a
capture, and that writer persists it.

===========================================================================
*/
package fortress

import (
	"sync"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
Record

One fortress row: the CRefSiegeFortress identity joined to its live state.
================
*/
type Record struct {
	ID       uint32
	CodeName string
	// GuildID is the occupying guild (_SiegeFortress.GuildID); zero when
	// nobody holds the fortress.
	GuildID int64
	// TempGuildID holds the fortress between a war's capture and its end
	// (UPDATE _SiegeFortress SET TempGuildID).
	TempGuildID int64
	// MaxEntrance copies the catalog row.
	MaxEntrance uint32
	// Applicants are the guilds registered for the coming war
	// (_SiegeFortressRequest).
	Applicants map[int64]bool
	// EntryOpen is the siege world's +0x84 flag: set five minutes after a
	// temporary capture (CGameWorld_Siege_Tick case 3, 0x493E0 ms), it lets
	// attackers through the gates again (slot 41, 601C20).
	EntryOpen bool
}

/*
================
Catalog

The fortress identities a shard serves: siegefortress.txt's enabled rows.
================
*/
type Catalog struct {
	ID       uint32
	CodeName string
	// MaxEntrance is the row's u16 at +0x7A (siegefortress.txt column 8):
	// how many PCs one side may bring into the fortress during its war.
	MaxEntrance uint32
}

/*
================
Authority

The per-division fortress table. One lock orders every read and change.
================
*/
type Authority struct {
	mu        sync.Mutex
	catalog   []Catalog
	divisions map[string]*division
}

type division struct {
	records map[uint32]*Record
	// warActive is the shard's fortress-war mode (MainProcess +0x42425):
	// one flag for every fortress, set while SiegeProgressing runs.
	warActive bool
}

/*
================
New
================
*/
func New(catalog []Catalog) *Authority {
	return &Authority{catalog: append([]Catalog(nil), catalog...), divisions: map[string]*division{}}
}

/*
================
divisionLocked

Opens a division's table on first use, every fortress unoccupied.
================
*/
func (a *Authority) divisionLocked(divisionID string) *division {
	if state, ok := a.divisions[divisionID]; ok {
		return state
	}
	state := &division{records: make(map[uint32]*Record, len(a.catalog))}
	for _, row := range a.catalog {
		state.records[row.ID] = &Record{ID: row.ID, CodeName: row.CodeName, MaxEntrance: row.MaxEntrance}
	}
	a.divisions[divisionID] = state
	return state
}

/*
================
ForWorld

The fortress a siege world belongs to: CGameWorld_Siege_ResolveFortress
(6019F0), bound at world creation (5F5B60) by joining the RefGameWorld's
fortress codename (its first string column) to REF_SIEGE_FORTRESS.
================
*/
func (a *Authority) ForWorld(world instance.Definition) (uint32, bool) {
	if a == nil || !world.Siege() {
		return 0, false
	}
	for _, row := range a.catalog {
		if row.CodeName == world.Strings[0] {
			return row.ID, true
		}
	}
	return 0, false
}

/*
================
Get
================
*/
func (a *Authority) Get(divisionID string, fortressID uint32) (Record, bool) {
	if a == nil {
		return Record{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return Record{}, false
	}
	copied := *record
	copied.Applicants = make(map[int64]bool, len(record.Applicants))
	for guild := range record.Applicants {
		copied.Applicants[guild] = true
	}
	return copied, true
}

/*
================
GuildOwns

CSiegeFortressMgr_IsGuildOwner (6353F0): the fortress's occupying guild is
this guild. A guildless player never owns one.
================
*/
func (a *Authority) GuildOwns(divisionID string, fortressID uint32, guildID int64) bool {
	if guildID == 0 {
		return false
	}
	record, ok := a.Get(divisionID, fortressID)
	return ok && record.GuildID == guildID
}

/*
================
WarActive
================
*/
func (a *Authority) WarActive(divisionID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.divisionLocked(divisionID).warActive
}
