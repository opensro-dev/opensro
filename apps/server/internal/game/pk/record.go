/*
===========================================================================

record.go - the durable PK record: daily count, total count, penalty

Package pk owns the player-killing rules that are pure arithmetic over a
character's domain.PKRecord: the three clamped counters
(daily, total and penalty points, as the server adds them), the murder
bookkeeping of the server's PvP kill rewards, the reliefs of a death and
of a monster kill, the daily reset and the penalty keeper's total decay. The
action runtime owns when they run and what they publish.

Every mutator returns the Changes it made, so the caller publishes exactly
the fields that moved (v1.150 0x33C4 daily, 0x3647 total, 0x30F2 penalty)
and refreshes the PvP state when the penalty crosses zero.

===========================================================================
*/

package pk

import (
	"time"

	"opensro.online/server/internal/domain"
)

const (
	// MaxDaily and MaxTotal clamp +0xB0 and +0xB2.
	MaxDaily = 15
	MaxTotal = 15
	// MaxPenalty clamps +0xB4 (0x30D40).
	MaxPenalty = 200000
	// totalDecaySeconds is the penalty keeper's period (0x2A300 s, 48 h).
	totalDecaySeconds = 0x2a300
	// murderPenaltyUnit is the 0x4B0 of the murder bookkeeping.
	murderPenaltyUnit = 1200
)

/*
================
Changes

Which record fields a mutation moved.
================
*/
type Changes uint8

const (
	ChangedDaily Changes = 1 << iota
	ChangedTotal
	ChangedPenalty
)

/*
================
ensure
================
*/
func ensure(c *domain.Character) *domain.PKRecord {
	if c.PK == nil {
		c.PK = &domain.PKRecord{}
	}
	return c.PK
}

/*
================
AddDaily

The server's daily add: the delta is capped at 15 first, then the sum is clamped to
0..15.
================
*/
func AddDaily(c *domain.Character, delta int32, now time.Time) Changes {
	if delta == 0 {
		return 0
	}
	r := ensure(c)
	next := int32(r.DailyCount) + min(delta, MaxDaily)
	next = max(0, min(next, MaxDaily))
	if next == int32(r.DailyCount) {
		return 0
	}
	r.DailyCount = uint8(next)
	r.DailyDay = localDay(now)
	return ChangedDaily
}

/*
================
AddTotal

The server's total add: the sum is clamped to 0..15.
================
*/
func AddTotal(c *domain.Character, delta int32) Changes {
	if delta == 0 {
		return 0
	}
	r := ensure(c)
	next := max(0, min(int32(r.TotalCount)+delta, MaxTotal))
	if next == int32(r.TotalCount) {
		return 0
	}
	r.TotalCount = uint16(next)
	if r.TotalCount == 0 {
		r.TotalDecayAt = 0
	}
	return ChangedTotal
}

/*
================
AddPenalty

The server's penalty add: the sum is clamped to 0..200000. A penalty that reaches zero
while the total is positive starts the penalty keeper (type 2, 48 h of
wall time). Inferred: a keeper already running keeps its deadline - the
native create path only fails on a database insert error.
================
*/
func AddPenalty(c *domain.Character, delta int32, now time.Time) Changes {
	if delta == 0 {
		return 0
	}
	r := ensure(c)
	next := max(0, min(int64(r.Penalty)+int64(delta), MaxPenalty))
	if next == int64(r.Penalty) {
		return 0
	}
	r.Penalty = uint32(next)
	if r.Penalty == 0 && r.TotalCount > 0 && r.TotalDecayAt == 0 {
		r.TotalDecayAt = now.Unix() + totalDecaySeconds
	}
	return ChangedPenalty
}

/*
================
RecordMurder

The murder bookkeeping, the killer's side of an ordinary-relation kill: total +1,
penalty += ((total + 1) / 2) * 1200 with the incremented total, daily +=
the level-gap weight.
================
*/
func RecordMurder(killer *domain.Character, killerLevel, victimLevel uint8, now time.Time) Changes {
	changed := AddTotal(killer, 1)
	total := int32(ensure(killer).TotalCount)
	changed |= AddPenalty(killer, (total+1)/2*murderPenaltyUnit, now)
	return changed | AddDaily(killer, DailyWeight(killerLevel, victimLevel), now)
}

/*
================
DailyWeight

The murder bookkeeping's daily weight: one, or |gap| / 5 * 2 + 1 when the victim is the lower.
================
*/
func DailyWeight(killerLevel, victimLevel uint8) int32 {
	gap := int32(victimLevel) - int32(killerLevel)
	if gap >= 0 {
		return 1
	}
	return -gap/5*2 + 1
}

/*
================
MonsterKillRelief

The server's monster-kill penalty relief, by
the extended level-difference classifier over monster - player level.
================
*/
func MonsterKillRelief(monsterLevel, playerLevel uint8) int32 {
	switch gap := int32(monsterLevel) - int32(playerLevel); {
	case gap >= 4:
		return -10
	case gap >= 1:
		return -7
	case gap >= -3:
		return -5
	case gap >= -6:
		return -3
	default:
		return -1
	}
}

/*
================
DecayTotal

The penalty keeper's timed-job tick and its resume after load
together: each elapsed 48 h period of wall time, offline included, drops
the total by one and re-arms while the total stays positive.
================
*/
func DecayTotal(c *domain.Character, now time.Time) Changes {
	r := c.PK
	if r == nil || r.TotalDecayAt == 0 {
		return 0
	}
	var changed Changes
	for r.TotalDecayAt != 0 && now.Unix() >= r.TotalDecayAt {
		r.TotalDecayAt += totalDecaySeconds
		changed |= AddTotal(c, -1)
	}
	return changed
}

/*
================
RepairKeeper

The server's entry check: a positive total with no penalty keeper is repaired to
penalty 1 (the "Penalty ( record empty )" path), which makes the player a
murderer until the next relief re-arms the keeper.
================
*/
func RepairKeeper(c *domain.Character, now time.Time) Changes {
	r := c.PK
	if r == nil || r.Penalty != 0 || r.TotalCount == 0 || r.TotalDecayAt != 0 {
		return 0
	}
	r.Penalty = 1
	return ChangedPenalty
}

/*
================
ResetDailyIfStale

The server's player tick resets every online player's daily count when the local
day changes. Inferred: an offline player's
count is reset the same way when it next enters or ticks, standing in for
the database's day rollover.
================
*/
func ResetDailyIfStale(c *domain.Character, now time.Time) Changes {
	r := c.PK
	if r == nil || r.DailyCount == 0 || r.DailyDay == localDay(now) {
		return 0
	}
	r.DailyCount = 0
	r.DailyDay = localDay(now)
	return ChangedDaily
}

/*
================
ClockDue

True when DecayTotal or ResetDailyIfStale would change the record now.
================
*/
func ClockDue(c *domain.Character, now time.Time) bool {
	r := c.PK
	return r != nil && (r.TotalDecayAt != 0 && now.Unix() >= r.TotalDecayAt ||
		r.DailyCount != 0 && r.DailyDay != localDay(now))
}

/*
================
localDay
================
*/
func localDay(now time.Time) int32 {
	y, m, d := now.Local().Date()
	return int32(y*10000 + int(m)*100 + d)
}
