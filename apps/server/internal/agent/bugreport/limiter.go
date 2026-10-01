/*
===========================================================================

limiter.go - per-account bug report pacing

A report can carry a 10 MiB video, and every accepted one lands in a
channel people read, so each account gets one report in flight, a minimum
gap between reports and a rolling daily cap. A failed delivery does not
count against the player: it was the channel, not the player, that failed.

===========================================================================
*/
package bugreport

import (
	"sync"
	"time"
)

const (
	reportInterval    = time.Minute
	reportWindow      = 24 * time.Hour
	reportsPerWindow  = 10
	maxLimiterEntries = 4096
)

/*
================
accountHistory
================
*/
type accountHistory struct {
	sent     []time.Time
	inFlight bool
	lastSeen time.Time
}

/*
================
limiter
================
*/
type limiter struct {
	mu       sync.Mutex
	now      func() time.Time
	accounts map[string]*accountHistory
}

/*
================
newLimiter
================
*/
func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, accounts: make(map[string]*accountHistory)}
}

/*
================
begin

Reserves the account's single in-flight slot. On refusal it returns how
long the player should wait. On success the caller must call finish once.
================
*/
func (l *limiter) begin(account string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	history := l.accounts[account]
	if history == nil {
		if len(l.accounts) >= maxLimiterEntries {
			l.evictOldest()
		}
		history = &accountHistory{}
		l.accounts[account] = history
	}
	history.lastSeen = now
	history.sent = pruneBefore(history.sent, now.Add(-reportWindow))
	if history.inFlight {
		return false, reportInterval
	}
	if count := len(history.sent); count > 0 {
		if wait := history.sent[count-1].Add(reportInterval).Sub(now); wait > 0 {
			return false, wait
		}
		if count >= reportsPerWindow {
			return false, history.sent[0].Add(reportWindow).Sub(now)
		}
	}
	history.inFlight = true
	return true, 0
}

/*
================
finish

Releases the in-flight slot; only a delivered report is counted.
================
*/
func (l *limiter) finish(account string, delivered bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	history := l.accounts[account]
	if history == nil {
		return
	}
	history.inFlight = false
	if delivered {
		history.sent = append(history.sent, l.now())
	}
}

/*
================
evictOldest

Drops the least recently seen idle account. An account with a report in
flight is never evicted, or finish would lose its slot.
================
*/
func (l *limiter) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, history := range l.accounts {
		if history.inFlight {
			continue
		}
		if oldestKey == "" || history.lastSeen.Before(oldest) {
			oldestKey = key
			oldest = history.lastSeen
		}
	}
	if oldestKey != "" {
		delete(l.accounts, oldestKey)
	}
}

/*
================
pruneBefore
================
*/
func pruneBefore(times []time.Time, cutoff time.Time) []time.Time {
	keep := 0
	for keep < len(times) && !times[keep].After(cutoff) {
		keep++
	}
	return times[keep:]
}
