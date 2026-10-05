/*
===========================================================================

union.go - the guild unions of every division

Package union owns which guilds stand together (CAlliance). A union is up
to eight guilds in fixed slots, the leading guild in slot 0
(CAlliance_SetGuildSlot 5B8C70, CAlliance_GetMasterGuildID 5B9260); a
guild sits in at most one (its CGuild +0x50 alliance id). The guild lane
proposes and answers, the fortress war asks who is allied with a holder
(CSiegeFortressMgr_IsGuildAlliedWithFortress 61D300), union chat asks
whose lines reach whom. Every change saves before its caller goes on.

The shard decides joins and removals; its rules are not in the
GameServer, so two are inferred here. A union left with one guild is
dissolved (the client clears its union list then: 762040 0x12 tests the
count below 2). A leading guild that leaves dissolves the union, as no
v1.150 message hands the lead to another guild. A union therefore lives
exactly as long as its leading guild leads it, so it takes that guild's
id, which the guild store never reuses.

===========================================================================
*/
package union

import (
	"errors"
	"fmt"
	"sync"

	"opensro.online/server/internal/domain"
)

// ErrFull is a join into a union whose eight slots are taken
// (UIIT_MSG_GUILDERR_ALLIANCE_FULL).
var ErrFull = errors.New("union: every slot is taken")

// ErrMember is a join of a guild already in a union.
var ErrMember = errors.New("union: guild already in a union")

/*
================
Authority
================
*/
type Authority struct {
	mu        sync.Mutex
	divisions map[string]*division
	store     domain.AllianceStore
}

type division struct {
	unions  map[int64]domain.AllianceRecord
	byGuild map[int64]int64
}

/*
================
New
================
*/
func New() *Authority {
	return &Authority{divisions: map[string]*division{}}
}

/*
================
divisionLocked
================
*/
func (a *Authority) divisionLocked(divisionID string) *division {
	state := a.divisions[divisionID]
	if state == nil {
		state = &division{unions: map[int64]domain.AllianceRecord{}, byGuild: map[int64]int64{}}
		a.divisions[divisionID] = state
	}
	return state
}

/*
================
Restore

Loads a division's stored unions and keeps the store for later saves.
================
*/
func (a *Authority) Restore(divisionID string, store domain.AllianceStore) error {
	if a == nil || store == nil {
		return nil
	}
	records, err := store.Alliances(divisionID)
	if err != nil {
		return fmt.Errorf("union: loading %s: %w", divisionID, err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	for _, record := range records {
		state.install(record)
	}
	a.store = store
	return nil
}

/*
================
install
================
*/
func (state *division) install(record domain.AllianceRecord) {
	state.unions[record.AllianceID] = record
	for _, guildID := range record.Guilds {
		if guildID != 0 {
			state.byGuild[guildID] = record.AllianceID
		}
	}
}

/*
================
Of

The union a guild stands in.
================
*/
func (a *Authority) Of(divisionID string, guildID int64) (domain.AllianceRecord, bool) {
	if a == nil || guildID == 0 {
		return domain.AllianceRecord{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	id, ok := state.byGuild[guildID]
	if !ok {
		return domain.AllianceRecord{}, false
	}
	return state.unions[id], true
}

/*
================
Allied

Two different guilds of one union (61D300 compares alliance ids).
================
*/
func (a *Authority) Allied(divisionID string, guildA, guildB int64) bool {
	if guildA == 0 || guildB == 0 || guildA == guildB {
		return false
	}
	record, ok := a.Of(divisionID, guildA)
	return ok && Holds(record, guildB)
}

/*
================
Holds
================
*/
func Holds(record domain.AllianceRecord, guildID int64) bool {
	for _, slot := range record.Guilds {
		if slot != 0 && slot == guildID {
			return true
		}
	}
	return false
}

/*
================
Count
================
*/
func Count(record domain.AllianceRecord) int {
	n := 0
	for _, slot := range record.Guilds {
		if slot != 0 {
			n++
		}
	}
	return n
}

/*
================
Join

Puts joiner in the leader's union, founding one with the leader in slot 0
and the joiner in slot 1 when the leader stands in none (5CA5A0 sets both
slots of a new union). The joiner takes the first free slot otherwise.
================
*/
func (a *Authority) Join(divisionID string, leader, joiner int64) (domain.AllianceRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	if _, taken := state.byGuild[joiner]; taken || leader == joiner {
		return domain.AllianceRecord{}, ErrMember
	}
	record := domain.AllianceRecord{AllianceID: leader}
	if id, ok := state.byGuild[leader]; ok {
		record = state.unions[id]
	} else {
		record.Guilds[0] = leader
	}
	slot := -1
	for i, guildID := range record.Guilds {
		if guildID == 0 {
			slot = i
			break
		}
	}
	if slot < 0 {
		return record, ErrFull
	}
	record.Guilds[slot] = joiner
	if err := a.saveLocked(divisionID, record, true); err != nil {
		return domain.AllianceRecord{}, err
	}
	state.install(record)
	return record, nil
}

/*
================
Remove

Takes a guild out of its union, returning the union as it stood before.
dissolved reports the union is gone: the guild led it, or one guild was
left (the inferences in the header).
================
*/
func (a *Authority) Remove(divisionID string, guildID int64) (before domain.AllianceRecord, dissolved bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	id, ok := state.byGuild[guildID]
	if !ok {
		return domain.AllianceRecord{}, false, ErrMember
	}
	before = state.unions[id]
	after := before
	for i, slot := range after.Guilds {
		if slot == guildID {
			after.Guilds[i] = 0
		}
	}
	if before.Guilds[0] == guildID || Count(after) < 2 {
		if err := a.saveLocked(divisionID, before, false); err != nil {
			return before, false, err
		}
		state.drop(before)
		return before, true, nil
	}
	if err := a.saveLocked(divisionID, after, true); err != nil {
		return before, false, err
	}
	delete(state.byGuild, guildID)
	state.unions[id] = after
	return before, false, nil
}

/*
================
drop
================
*/
func (state *division) drop(record domain.AllianceRecord) {
	delete(state.unions, record.AllianceID)
	for _, guildID := range record.Guilds {
		if guildID != 0 && state.byGuild[guildID] == record.AllianceID {
			delete(state.byGuild, guildID)
		}
	}
}

/*
================
saveLocked

A nil store (fixtures, a shard with no authority) keeps the state in
memory only.
================
*/
func (a *Authority) saveLocked(divisionID string, record domain.AllianceRecord, present bool) error {
	if a.store == nil {
		return nil
	}
	return a.store.SaveAlliance(divisionID, record, present)
}
