/*
===========================================================================

killstats.go - kills and EXP per online hour by level band (port-only)

Port-only, not native (#1639, owner-approved from the #1604 P10 vote).
With SRO_KILL_STATS on, monster settlements count kills, character EXP
and skill EXP into a name-free rollup keyed by (UTC day, level band);
online time accrues on the tick. Nothing here ever holds an account or
character name, and the only per-character state - the set behind a
band's distinct-character count - lives in memory for its day and is
dropped at the day's last flush, so a flush carries counts only. The
cmd wiring drains the rollup into the economy history (economy.db),
which serves it to the staff console; off (default) nothing counts and
the route answers 404.

===========================================================================
*/

package action

import (
	"os"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

// EnvKillStats turns the port-only kill statistics on ("on", "1",
// "true"). Default off: no counting, no route.
const EnvKillStats = "SRO_KILL_STATS"

// killStatFlushEveryMs is the tick cadence at which the wiring's sink
// receives the rollup; online time accrues killStatAccrueEveryMs at a
// time, attributed to the characters entered at the accrual instant.
const (
	killStatFlushEveryMs   = 5 * 60 * 1000
	killStatAccrueEveryMs  = 60 * 1000
	killStatMsPerDay       = 24 * 60 * 60 * 1000
	killStatBandSpanLevels = 10
)

/*
================
KillStatsFromEnv
================
*/
func KillStatsFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvKillStats))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
KillStatRow

One (day, level band) flush row: the deltas since the last flush and
the band's distinct characters so far that day. DayS is the UTC day's
start in unix seconds. Characters is a count, never an identity.
================
*/
type KillStatRow struct {
	DayS       int64
	Band       int
	Kills      int64
	Exp        int64
	SkillExp   int64
	OnlineMs   int64
	Characters int
}

/*
================
killStatBandOf

The 10-level band a level counts in: band 0 is levels 1-10.
================
*/
func killStatBandOf(level int64) int {
	if level < 1 {
		level = 1
	}
	return int((level - 1) / killStatBandSpanLevels)
}

/*
================
killStatDayOf

The UTC day (its start, unix seconds) a millisecond stamp falls in.
================
*/
func killStatDayOf(nowMs int64) int64 {
	return nowMs / killStatMsPerDay * (killStatMsPerDay / 1000)
}

//============================================================================

type killStatKey struct {
	dayS int64
	band int
}

type killStatCell struct {
	kills, exp, skillExp, onlineMs int64
	// chars is the distinct characters seen in this band today. It
	// exists only in memory and only until the day's last flush; the
	// flush reads its size and no identity ever leaves this struct.
	chars map[int64]struct{}
}

/*
================
killStatTracker

The rollup. Counting runs inside character doors on request and tick
goroutines, so every entry takes mu; the accrual and flush run on the
tick only.
================
*/
type killStatTracker struct {
	mu            sync.Mutex
	cells         map[killStatKey]*killStatCell
	lastAccrualMs int64
	lastFlushMs   int64
}

/*
================
cell

The (day, band) cell, created on first use. Caller holds mu.
================
*/
func (t *killStatTracker) cell(dayS int64, band int) *killStatCell {
	if t.cells == nil {
		t.cells = map[killStatKey]*killStatCell{}
	}
	key := killStatKey{dayS: dayS, band: band}
	found, ok := t.cells[key]
	if !ok {
		found = &killStatCell{chars: map[int64]struct{}{}}
		t.cells[key] = found
	}
	return found
}

/*
================
accrue

elapsedMs of online time for every entered character, in its current
band; each is also seen today. Deleted-pending characters are leaving
and do not accrue.
================
*/
func (t *killStatTracker) accrue(dayS int64, online []*enterworld.Character, elapsedMs int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range online {
		if c == nil || c.DeletePending {
			continue
		}
		cell := t.cell(dayS, killStatBandOf(rewardLevel(c)))
		cell.onlineMs += elapsedMs
		cell.chars[c.ID] = struct{}{}
	}
}

//============================================================================

/*
================
countKillStatAward

One recipient's monster-settlement award: character EXP and skill EXP
count in the recipient's own band, and the recipient was here today.
================
*/
func (rt *Runtime) countKillStatAward(c *enterworld.Character, exp, skillExp, nowMs int64) {
	if !rt.KillStats || c == nil {
		return
	}
	dayS, band := killStatDayOf(nowMs), killStatBandOf(rewardLevel(c))
	rt.killStats.mu.Lock()
	cell := rt.killStats.cell(dayS, band)
	cell.exp += exp
	cell.skillExp += skillExp
	cell.chars[c.ID] = struct{}{}
	rt.killStats.mu.Unlock()
}

/*
================
countKillStatKill

One settled monster kill in the killer's band. Placed where the
settlement can no longer refuse, so a kill that pays nothing (a
training dummy's early-out, once #1463 lands) never counts.
================
*/
func (rt *Runtime) countKillStatKill(killer *enterworld.Character, nowMs int64) {
	if !rt.KillStats || killer == nil {
		return
	}
	dayS, band := killStatDayOf(nowMs), killStatBandOf(rewardLevel(killer))
	rt.killStats.mu.Lock()
	cell := rt.killStats.cell(dayS, band)
	cell.kills++
	cell.chars[killer.ID] = struct{}{}
	rt.killStats.mu.Unlock()
}

/*
================
advanceKillStats

The tick step: once a minute, every entered character accrues the
elapsed minute in its current band; once a flush interval passes, the
rollup drains to the wiring's sink. A character is charged for a whole
interval if it is entered at the accrual instant - joining or leaving
mid-interval rounds to that instant, the usual rollup approximation.
================
*/
func (rt *Runtime) advanceKillStats(nowMs int64) {
	if !rt.KillStats {
		return
	}
	if rt.killStats.lastAccrualMs == 0 {
		rt.killStats.lastAccrualMs = nowMs
		rt.killStats.lastFlushMs = nowMs
	}
	if elapsed := nowMs - rt.killStats.lastAccrualMs; elapsed >= killStatAccrueEveryMs {
		rt.killStats.lastAccrualMs = nowMs
		if rt.Ground != nil && rt.deps != nil {
			var online []*enterworld.Character
			for _, division := range rt.Ground.DivisionIDs() {
				online = append(online, rt.deps.CharactersForDivision(division)...)
			}
			rt.killStats.accrue(killStatDayOf(nowMs), online, elapsed)
		}
	}
	if nowMs-rt.killStats.lastFlushMs >= killStatFlushEveryMs {
		rt.killStats.lastFlushMs = nowMs
		if err := rt.FlushKillStats(); err != nil {
			// The deltas survive (FlushKillStats only clears what the
			// sink accepted), so the next cadence retries.
		}
	}
}

/*
================
FlushKillStats

Drains the rollup to the wiring's sink (KillStatsFlush); a no-op when
unwired or empty. Only what the sink accepts is cleared: the additive
columns zero, and a past day's cells - character sets included - are
dropped once their final numbers are written.
================
*/
func (rt *Runtime) FlushKillStats() error {
	if rt.KillStatsFlush == nil {
		return nil
	}
	rt.killStats.mu.Lock()
	rows := make([]KillStatRow, 0, len(rt.killStats.cells))
	for key, cell := range rt.killStats.cells {
		if cell.kills == 0 && cell.exp == 0 && cell.skillExp == 0 && cell.onlineMs == 0 {
			continue
		}
		rows = append(rows, KillStatRow{DayS: key.dayS, Band: key.band, Kills: cell.kills,
			Exp: cell.exp, SkillExp: cell.skillExp, OnlineMs: cell.onlineMs, Characters: len(cell.chars)})
	}
	rt.killStats.mu.Unlock()
	if len(rows) == 0 {
		return nil
	}
	if err := rt.KillStatsFlush(rows); err != nil {
		return err
	}
	todayS := killStatDayOf(time.Now().UnixMilli())
	if rt.Now != nil {
		todayS = killStatDayOf(rt.Now().UnixMilli())
	}
	rt.killStats.mu.Lock()
	for _, row := range rows {
		key := killStatKey{dayS: row.DayS, band: row.Band}
		cell, ok := rt.killStats.cells[key]
		if !ok {
			continue
		}
		cell.kills -= row.Kills
		cell.exp -= row.Exp
		cell.skillExp -= row.SkillExp
		cell.onlineMs -= row.OnlineMs
		if row.DayS < todayS {
			delete(rt.killStats.cells, key)
		}
	}
	rt.killStats.mu.Unlock()
	return nil
}
