/*
===========================================================================

character_online.go - the persisted time-online record

Port-only (not native): the public boards (opensro#2675) rank players by
time in the world, and no native record keeps that. The record lives on
the character so it survives restarts, and it is credited only through
AnchorOnline / CreditOnline below, always under a character-store door.

Crediting is anchored at the last credited stamp, never at a session
object, so an evicted or duplicate session cannot count the same span
twice: whoever credits first moves the anchor, and the next credit sees
only what is left. The wiring credits on a periodic pass over the
entered sessions and once more when a session closes, so a crash loses
at most one crediting interval.

Days are UTC calendar days (yyyymmdd). The record keeps today and the
previous six days; older buckets are pruned at every anchor and credit.

===========================================================================
*/

package domain

import (
	"time"
)

// onlineWindowDays is how many UTC day buckets the record keeps: today
// plus the previous six, the board's rolling week.
const onlineWindowDays = 7

// onlineCreditIntervalMs is the periodic pass's minimum owed span: a
// character is credited at most once a minute, so the tick's batched
// commit writes an online character's row once a minute, not once a
// tick. Session close ignores it and credits whatever is owed.
const onlineCreditIntervalMs = 60_000

/*
================
OnlineDay

One UTC day's credited seconds.
================
*/
type OnlineDay struct {
	Day     int32 `json:"day"`
	Seconds int64 `json:"seconds"`
}

/*
================
OnlineRecord

TotalSeconds is the all-time credited total. LastCreditMs is the anchor
the next credit measures from; it moves only inside AnchorOnline and
CreditOnline, under the caller's store door.
================
*/
type OnlineRecord struct {
	TotalSeconds int64       `json:"totalSeconds"`
	Days         []OnlineDay `json:"days,omitempty"`
	LastCreditMs int64       `json:"lastCreditMs"`
}

/*
================
onlineDayKey

The UTC yyyymmdd key of an instant.
================
*/
func onlineDayKey(ms int64) int32 {
	t := time.UnixMilli(ms).UTC()
	return int32(t.Year()*10000 + int(t.Month())*100 + t.Day())
}

/*
================
onlineCutoffKey

The oldest day key still inside the window at an instant: today minus
onlineWindowDays-1 days, as a yyyymmdd key.
================
*/
func onlineCutoffKey(ms int64) int32 {
	t := time.UnixMilli(ms).UTC().AddDate(0, 0, -(onlineWindowDays - 1))
	return int32(t.Year()*10000 + int(t.Month())*100 + t.Day())
}

/*
================
pruneDays

Drops the buckets older than the window at nowMs.
================
*/
func (r *OnlineRecord) pruneDays(nowMs int64) {
	cutoff := onlineCutoffKey(nowMs)
	kept := r.Days[:0]
	for _, day := range r.Days {
		if day.Day >= cutoff {
			kept = append(kept, day)
		}
	}
	r.Days = kept
}

/*
================
AnchorOnline

Starts (or restarts) the credit clock at nowMs without crediting
anything: login calls it, so time before this session never counts and
a stale anchor from a crashed session is not credited late. Reports
whether the record changed.
================
*/
func (c *Character) AnchorOnline(nowMs int64) bool {
	if c == nil {
		return false
	}
	if c.Online == nil {
		c.Online = &OnlineRecord{LastCreditMs: nowMs}
		return true
	}
	changed := c.Online.LastCreditMs != nowMs
	c.Online.LastCreditMs = nowMs
	before := len(c.Online.Days)
	c.Online.pruneDays(nowMs)
	return changed || len(c.Online.Days) != before
}

/*
================
CreditOnline

Credits the whole seconds between the anchor and nowMs into the total
and the day buckets, splitting at UTC midnight, then advances the
anchor by exactly the credited seconds so a sub-second remainder is
never lost. Reports whether anything was credited. A backwards clock
re-anchors without crediting, so skewed time is never counted twice.
================
*/
func (c *Character) CreditOnline(nowMs int64) bool {
	if c == nil || c.Online == nil {
		return false
	}
	r := c.Online
	if nowMs < r.LastCreditMs {
		r.LastCreditMs = nowMs
		return true
	}
	seconds := (nowMs - r.LastCreditMs) / 1000
	if seconds <= 0 {
		return false
	}
	cursor := r.LastCreditMs
	left := seconds
	for left > 0 {
		t := time.UnixMilli(cursor).UTC()
		dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		toMidnight := (dayStart + 24*60*60*1000 - cursor) / 1000
		share := left
		if share > toMidnight {
			share = toMidnight
		}
		key := onlineDayKey(cursor)
		found := false
		for i := range r.Days {
			if r.Days[i].Day == key {
				r.Days[i].Seconds += share
				found = true
				break
			}
		}
		if !found {
			r.Days = append(r.Days, OnlineDay{Day: key, Seconds: share})
		}
		cursor += share * 1000
		left -= share
	}
	r.TotalSeconds += seconds
	r.LastCreditMs += seconds * 1000
	r.pruneDays(nowMs)
	return true
}

/*
================
CreditOnlinePeriodic

The periodic pass's credit: nothing until onlineCreditIntervalMs is
owed, then the whole owed span. Reports whether anything was credited.
================
*/
func (c *Character) CreditOnlinePeriodic(nowMs int64) bool {
	if c == nil || c.Online == nil {
		return false
	}
	if nowMs-c.Online.LastCreditMs < onlineCreditIntervalMs {
		return false
	}
	return c.CreditOnline(nowMs)
}

/*
================
WeekSeconds

The rolling week's credited seconds at nowMs: the buckets inside the
window ending today. Pruning keeps stored buckets inside it, but a
record read long after its last credit (a player offline for weeks)
must not present those stale buckets as this week's time, so the read
applies the same cutoff.
================
*/
func (r *OnlineRecord) WeekSeconds(nowMs int64) int64 {
	if r == nil {
		return 0
	}
	cutoff := onlineCutoffKey(nowMs)
	var total int64
	for _, day := range r.Days {
		if day.Day >= cutoff {
			total += day.Seconds
		}
	}
	return total
}
