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

	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/world/instance"
)

/*
================
Record

One fortress row: the CRefSiegeFortress identity joined to its live state.
================
*/
type Record struct {
	// _SiegeFortress +0x30: hired staff, independent of the war period.
	StaffFlags uint8
	ID         uint32
	CodeName   string
	// GuildID is the occupying guild (_SiegeFortress.GuildID); zero when
	// nobody holds the fortress.
	GuildID int64
	// TempGuildID holds the fortress between a war's capture and its end
	// (UPDATE _SiegeFortress SET TempGuildID).
	TempGuildID int64
	// 61DA40 serializes the signed tax ratio and accumulated tax gold.
	TaxRate int16
	TaxGold int64
	// savedTax is the TaxGold last written (6201A0's +0x80); the treasury
	// is written again once it has grown past it by taxFlushThreshold.
	savedTax int64
	// MaxEntrance, RequestFee and OfficialNpc copy the catalog row.
	MaxEntrance uint32
	RequestFee  uint64
	OfficialNpc string
	TownGate    string
	// Applicants are the guilds registered for the coming war
	// (_SiegeFortressRequest), each with its request kind.
	Applicants map[int64]RequestKind
	// EntryOpen is the siege world's +0x84 flag: open from the world's
	// construction (600C60), shut by a temporary capture and open again
	// five minutes later (CGameWorld_Siege_Tick case 3, 0x493E0 ms); it
	// lets attackers through the gates (slot 41, 601C20).
	EntryOpen bool
	// The capture state of a running war (capture.go).
	capture
	battles           map[int64]domain.FortressBattleRecord
	battleCheckpoints map[int64]domain.FortressBattleRecord
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
	// RequestFee is the row's u32 at +0x80 (column 12), the gold an
	// attacking guild pays to apply (633910, charged by the 0xA result).
	RequestFee uint64
	// OfficialNpc is the codename of the NPC that takes applications
	// (column 14; 63BBC0 matches the selected NPC against it).
	OfficialNpc string
	// TownGate is the teleport code of the fortress's town gate (column 6),
	// where the war's phases send the PCs that must leave (601690).
	TownGate string
}

/*
================
RequestKind

The application's kind byte (0x71E1 subtype 7/8): an attacker, or a guild
allied with the owner.
================
*/
type RequestKind uint8

const (
	RequestAttack RequestKind = 0
	RequestAlly   RequestKind = 1
)

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
	// store keeps the occupation and request rows (persist.go); nil keeps
	// them in memory only.
	store domain.FortressStore
}

type division struct {
	records map[uint32]*Record
	// warActive is the shard's fortress-war mode (MainProcess +0x42425):
	// one flag for every fortress, set while SiegeProgressing runs.
	warActive bool
	// requestPeriod is MainProcess +0x42424 (AllowSiegeRequest), taxPeriod
	// +0x42426 (AllowSiegeTaxJob); CSiegeFortressMgr_OnShardMessage (62EE90)
	// sets them on the 0x33/0x34 and 0x31/0x32 edges.
	requestPeriod, taxPeriod bool
}

/*
================
Period

The three shard-wide fortress periods, the flags byte of the fortress list
(CSiegeFortressMgr_WriteFortressList 62EBE0: war 1, request 2, tax 4).
================
*/
type Period uint8

const (
	PeriodWar     Period = 1
	PeriodRequest Period = 2
	PeriodTax     Period = 4
)

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
		state.records[row.ID] = &Record{ID: row.ID, CodeName: row.CodeName, MaxEntrance: row.MaxEntrance,
			RequestFee: row.RequestFee, OfficialNpc: row.OfficialNpc, TownGate: row.TownGate, EntryOpen: true}
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
	copied.battleCheckpoints = nil
	copied.battles = nil // Scores are read through BattleRecord, never a mutable map alias.
	copied.Applicants = make(map[int64]RequestKind, len(record.Applicants))
	for guild, kind := range record.Applicants {
		copied.Applicants[guild] = kind
	}
	return copied, true
}

/*
================
AppliedFortress

The fortress a guild has applied to, if any (5C3380 scans every
fortress's requests).
================
*/
func (a *Authority) AppliedFortress(divisionID string, guildID int64) (uint32, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, record := range a.divisionLocked(divisionID).records {
		if _, ok := record.Applicants[guildID]; ok {
			return id, true
		}
	}
	return 0, false
}

/*
================
OwnedFortress

The fortress a guild occupies, if any (5C5730).
================
*/
func (a *Authority) OwnedFortress(divisionID string, guildID int64) (uint32, bool) {
	if guildID == 0 {
		return 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, record := range a.divisionLocked(divisionID).records {
		if record.GuildID == guildID {
			return id, true
		}
	}
	return 0, false
}

/*
================
SetApplication

Adds (apply) or removes (cancel) a guild's request; reports whether the
table changed.
================
*/
func (a *Authority) SetApplication(divisionID string, fortressID uint32, guildID int64, kind RequestKind, applied bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok || guildID == 0 {
		return false
	}
	previous, present := record.Applicants[guildID]
	if applied == present {
		return false
	}
	if applied {
		if record.Applicants == nil {
			record.Applicants = map[int64]RequestKind{}
		}
		record.Applicants[guildID] = kind
	} else {
		delete(record.Applicants, guildID)
	}
	if err := a.saveRequestLocked(divisionID, fortressID, guildID, kind, applied); err != nil {
		// The official must not confirm what the store does not hold.
		if applied {
			delete(record.Applicants, guildID)
		} else {
			record.Applicants[guildID] = previous
		}
		return false
	}
	return true
}

/*
================
Occupy

Hands the fortress to guildID (0 leaves it unoccupied) and ends any
temporary capture: the occupation a war's end settles. Reports whether
the fortress exists.
================
*/
func (a *Authority) Occupy(divisionID string, fortressID uint32, guildID int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return false
	}
	record.GuildID, record.TempGuildID = guildID, 0
	_ = a.saveRecordLocked(divisionID, record)
	return true
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
SetPeriod

Turns one period on or off; reports whether it changed.
================
*/
func (a *Authority) SetPeriod(divisionID string, period Period, on bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	var flag *bool
	switch period {
	case PeriodWar:
		flag = &state.warActive
	case PeriodRequest:
		flag = &state.requestPeriod
	case PeriodTax:
		flag = &state.taxPeriod
	default:
		return false
	}
	if *flag == on {
		return false
	}
	*flag = on
	return true
}

/*
================
Periods
================
*/
func (a *Authority) Periods(divisionID string) Period {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	var out Period
	if state.warActive {
		out |= PeriodWar
	}
	if state.requestPeriod {
		out |= PeriodRequest
	}
	if state.taxPeriod {
		out |= PeriodTax
	}
	return out
}

/*
================
Records

Every fortress of a division, in catalog order.
================
*/
func (a *Authority) Records(divisionID string) []Record {
	if a == nil {
		return nil
	}
	out := make([]Record, 0, len(a.catalog))
	for _, row := range a.catalog {
		if record, ok := a.Get(divisionID, row.ID); ok {
			out = append(out, record)
		}
	}
	return out
}

/*
================
GuildInWar

A guild fighting a running war: it holds, temporarily holds or applied
to a fortress (SiegeManager_IsGuildOutOfWar 635470 asks every fortress
whether the guild takes part). Union changes are refused then.
================
*/
func (a *Authority) GuildInWar(divisionID string, guildID int64) bool {
	if a == nil || guildID == 0 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	if !state.warActive {
		return false
	}
	for _, record := range state.records {
		if _, applied := record.Applicants[guildID]; applied || record.GuildID == guildID || record.TempGuildID == guildID {
			return true
		}
	}
	return false
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
