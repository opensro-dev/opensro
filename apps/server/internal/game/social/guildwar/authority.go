/*
===========================================================================

authority.go - active guild wars and their native settlement rules

Writers serialize durable changes. Combat reads an immutable published
snapshot, so a character transaction never waits for a writer that is
itself waiting for the character store. This is the sole active-war owner.

===========================================================================
*/
package guildwar

import (
	"errors"
	"sync"
	"sync/atomic"

	"opensro.online/server/internal/domain"
)

const (
	UnlimitedPeriod uint32 = 0x7fffffff
	MaximumStake    uint32 = 500000000
	MaximumEnemies         = 50
)

/*
================
Snapshot
================
*/
type Snapshot struct{ rows []domain.GuildWarRecord }

/*
================
Authority
================
*/
type Authority struct {
	mu       sync.Mutex
	division string
	store    domain.GuildWarStore
	current  atomic.Pointer[Snapshot]
}

/*
================
New
================
*/
func New(division string, store domain.GuildWarStore) (*Authority, error) {
	if store == nil {
		return nil, errors.New("guild war: missing durable owner")
	}
	rows, err := store.GuildWars(division)
	if err != nil {
		return nil, err
	}
	a := &Authority{division: division, store: store}
	a.current.Store(&Snapshot{rows: rows})
	return a, nil
}

/*
================
Wars
================
*/
func (a *Authority) Wars(division string, guildID int64) []domain.GuildWarRecord {
	if a == nil || division != a.division {
		return nil
	}
	var rows []domain.GuildWarRecord
	for _, row := range a.current.Load().rows {
		if row.Guilds[0] == guildID || row.Guilds[1] == guildID {
			rows = append(rows, row)
		}
	}
	return rows
}

/*
================
Find
================
*/
func (a *Authority) Find(division string, first, second int64) (domain.GuildWarRecord, bool) {
	if a == nil || division != a.division || first == 0 || second == 0 {
		return domain.GuildWarRecord{}, false
	}
	for _, war := range a.current.Load().rows {
		if war.Guilds == [2]int64{first, second} || war.Guilds == [2]int64{second, first} {
			return war, true
		}
	}
	return domain.GuildWarRecord{}, false
}

/*
================
ValidTerms

5C6B60 compares the packed fields without normalizing them; minutes stop
at 50, matching the six ten-minute options in client 618A60.
================
*/
func ValidTerms(terms domain.GuildWarTerms) bool {
	return terms.ScoreIndex < 8 &&
		(terms.Period >= UnlimitedPeriod || terms.Period>>10&31 <= 30 && terms.Period>>15&31 <= 23 && terms.Period>>20&63 <= 50)
}

/*
================
Deadline

435A10 adds the native duration fields to the current wall clock.
================
*/
func Deadline(period uint32, nowMs int64) int64 {
	if period >= UnlimitedPeriod {
		return 0
	}
	seconds := int64(period>>10&31)*24*60*60 + int64(period>>15&31)*60*60 + int64(period>>20&63)*60 + int64(period>>26)
	return nowMs/1000*1000 + seconds*1000
}

/*
================
ScoreLimit
================
*/
func ScoreLimit(index uint8) uint32 {
	limits := [...]uint32{0, 5000, 10000, 50000, 100000, 150000, 250000, 300000}
	if int(index) >= len(limits) {
		return 0
	}
	return limits[index]
}

/*
================
KillScore

5C7000 scores victim level minus actual striker level, using the COS level
when the striker is a companion. The result is a byte on the shard wire.
================
*/
func KillScore(victimLevel, strikerLevel int64) uint8 {
	delta := victimLevel - strikerLevel
	if delta <= -6 {
		return 1
	}
	if delta > 10 {
		return 251
	}
	return uint8(100 + 15*delta)
}

/*
================
Begin
================
*/
func (a *Authority) Begin(start domain.GuildWarStart) (domain.GuildWarRecord, uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	row, code, err := a.store.BeginGuildWar(a.division, start)
	if code != 0 || err != nil {
		return row, code, err
	}
	rows := append([]domain.GuildWarRecord{}, a.current.Load().rows...)
	rows = append(rows, row)
	a.current.Store(&Snapshot{rows: rows})
	return row, 0, nil
}

/*
================
End
================
*/
func (a *Authority) End(id uint32, winner int64) (uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.endLocked(id, winner)
}

/*
================
endLocked
================
*/
func (a *Authority) endLocked(id uint32, winner int64) (uint8, error) {
	code, err := a.store.EndGuildWar(a.division, id, winner)
	if code != 0 || err != nil {
		return code, err
	}
	var rows []domain.GuildWarRecord
	for _, row := range a.current.Load().rows {
		if row.ID != id {
			rows = append(rows, row)
		}
	}
	a.current.Store(&Snapshot{rows: rows})
	return 0, nil
}

/*
================
Combat

43C320 checks the deadline, then lets the scoring side's reached score
limit override that result. A deadline tie at a kill awards guild B.
================
*/
func (a *Authority) Combat(combat domain.GuildWarCombat, nowMs int64) (domain.GuildWarRecord, int64, uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	row, code, err := a.store.AccountGuildWarCombat(a.division, combat)
	if code != 0 || err != nil {
		return row, 0, code, err
	}
	var previous domain.GuildWarRecord
	rows := append([]domain.GuildWarRecord{}, a.current.Load().rows...)
	for i := range rows {
		if rows[i].ID == row.ID {
			previous = rows[i]
			rows[i] = row
		}
	}
	a.current.Store(&Snapshot{rows: rows})
	winner := int64(0)
	if row.EndMs != 0 && nowMs >= row.EndMs {
		winner = row.Guilds[1]
		if row.Scores[0] > row.Scores[1] {
			winner = row.Guilds[0]
		}
	}
	limit := ScoreLimit(row.ScoreIndex)
	for side := range row.Scores {
		if row.Scores[side] != previous.Scores[side] && limit != 0 && row.Scores[side] >= limit {
			winner = row.Guilds[side]
		}
	}
	if winner != 0 {
		code, err = a.endLocked(row.ID, winner)
		if code != 0 || err != nil {
			// Combat committed even when its separate settlement did not.
			return row, 0, 0, err
		}
	}
	return row, winner, 0, nil
}

/*
================
Expired

438430 explicitly leaves tied expired wars running.
================
*/
func (a *Authority) Expired(nowMs int64) []domain.GuildWarRecord {
	var rows []domain.GuildWarRecord
	for _, row := range a.current.Load().rows {
		if row.EndMs != 0 && nowMs >= row.EndMs && row.Scores[0] != row.Scores[1] {
			rows = append(rows, row)
		}
	}
	return rows
}

/*
================
RemainingSeconds

5CFD30 serializes the caller's remaining seconds, not a packed date.
The unlimited sentinel is 0x7FFFFFFF; an overdue tied war carries a signed
negative duration in the uint32 wire field until a later kill settles it.
================
*/
func RemainingSeconds(endMs, nowMs int64) uint32 {
	if endMs == 0 {
		return UnlimitedPeriod
	}
	return uint32((endMs - nowMs) / 1000)
}

/*
================
Division
================
*/
func (a *Authority) Division() string { return a.division }
