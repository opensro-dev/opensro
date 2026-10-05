/*
===========================================================================

premiumclock.go - the premium ticket's daily allotment

A premium time ticket (premiumticket.go) raises the EXP and skill-EXP
keepers for its whole period, but CTJ_PremiumKeeper (SR_GameServer:
CTJ_PremiumKeeper_Load 6524B0, CTJ_PremiumKeeper_Tick 6529D0) applies them
only while the day's allotment lasts. The ticket's Param3 is that
allotment in milliseconds a day (10,800,000: three hours; the PLUS ticket
five).

  - Days are counted on the wall clock from the ticket's use (0x15180
    seconds). On each new day the previous day's remainder becomes the
    carry and the day's grant is refilled; a second missed day leaves the
    whole grant as the carry (the load loop repeats the shift).
  - Time is spent only on the owner's ticks, carry first. When both are
    spent the keepers come off until the next day.
  - The keeper writes its state every 600 seconds (and on each change of
    state); the port keeps the spend in memory between commits the same
    way, and commits it when the owner leaves the world.

INFERENCE: the tick also tests an owner byte (+0x30 -> +0x0D) before
spending, and v1.188 can pause the ticket after a 20-second confirmation
(+0x80). No v1.150 client control writes either, so the port spends while
the owner is in the world and has no pause.

===========================================================================
*/

package action

import (
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

const (
	// premiumDayMs is the keeper's day (0x15180 seconds).
	premiumDayMs int64 = 86_400_000
	// premiumCommitMs is the keeper's 600-second state write.
	premiumCommitMs int64 = 600_000
)

/*
================
premiumClockRoll

Brings a clock to the day nowMs falls in (CTJ_PremiumKeeper_Load's loop):
each day passed shifts today's remainder into the carry and refills today.
================
*/
func premiumClockRoll(clock *domain.PremiumClock, nowMs int64) bool {
	if nowMs < clock.StartUnixMs {
		return false
	}
	day := (nowMs - clock.StartUnixMs) / premiumDayMs
	if day <= clock.Day {
		return false
	}
	// Two shifts already leave carry and today at the full grant.
	for step := min(day-clock.Day, 2); step > 0; step-- {
		clock.CarriedMs = clock.TodayMs
		clock.TodayMs = clock.DailyAllotmentMs
	}
	clock.Day = day
	return true
}

/*
================
premiumClockSpend

Spends elapsed online time, carry first (CTJ_PremiumKeeper_Tick).
================
*/
func premiumClockSpend(clock *domain.PremiumClock, elapsedMs int64) {
	if elapsedMs <= 0 {
		return
	}
	if clock.CarriedMs > 0 {
		clock.CarriedMs = max(0, clock.CarriedMs-elapsedMs)
		return
	}
	clock.TodayMs = max(0, clock.TodayMs-elapsedMs)
}

/*
================
premiumClockLive

The clock has allotment left at nowMs: its period runs and either the carry
or today's grant remains.
================
*/
func premiumClockLive(clock *domain.PremiumClock, nowMs int64) bool {
	if clock == nil || nowMs >= clock.EndUnixMs {
		return false
	}
	view := *clock
	premiumClockRoll(&view, nowMs)
	return view.CarriedMs > 0 || view.TodayMs > 0
}

/*
================
newPremiumClock

The clock a ticket starts: day 0 holds the full grant and no carry.
================
*/
func newPremiumClock(itemRefObjID uint32, nowMs, endMs, dailyMs int64) *domain.PremiumClock {
	return &domain.PremiumClock{ItemRefObjID: itemRefObjID, StartUnixMs: nowMs, EndUnixMs: endMs,
		DailyAllotmentMs: dailyMs, TodayMs: dailyMs}
}

//============================================================================

/*
================
premiumSpendRow

One online owner's uncommitted spend: the tick it was last seen on, the
time spent since the last commit, and when that commit was.
================
*/
type premiumSpendRow struct {
	seenMs, pendingMs, committedMs int64
}

/*
================
premiumSpendLedger
================
*/
type premiumSpendLedger struct {
	mu   sync.Mutex
	rows map[petOwnerKey]premiumSpendRow
}

/*
================
premiumSpendLedger.take

Advances an owner's row to nowMs and reports whether its spend is due for a
commit: the allotment it holds would run out, or 600 seconds passed.
================
*/
func (ledger *premiumSpendLedger) take(key petOwnerKey, clock *domain.PremiumClock, nowMs int64) (int64, bool) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.rows == nil {
		ledger.rows = map[petOwnerKey]premiumSpendRow{}
	}
	row, found := ledger.rows[key]
	if !found {
		ledger.rows[key] = premiumSpendRow{seenMs: nowMs, committedMs: nowMs}
		return 0, false
	}
	if nowMs > row.seenMs {
		row.pendingMs += nowMs - row.seenMs
	}
	row.seenMs = nowMs
	ledger.rows[key] = row
	left := clock.CarriedMs + clock.TodayMs
	return row.pendingMs, row.pendingMs >= left || nowMs-row.committedMs >= premiumCommitMs
}

/*
================
premiumSpendLedger.committed
================
*/
func (ledger *premiumSpendLedger) committed(key petOwnerKey, nowMs int64) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.rows != nil {
		ledger.rows[key] = premiumSpendRow{seenMs: nowMs, committedMs: nowMs}
	}
}

/*
================
premiumSpendLedger.drop

The owner left: returns its uncommitted spend for the departure commit.
================
*/
func (ledger *premiumSpendLedger) drop(key petOwnerKey, nowMs int64) int64 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	row, found := ledger.rows[key]
	if !found {
		return 0
	}
	delete(ledger.rows, key)
	if nowMs > row.seenMs {
		row.pendingMs += nowMs - row.seenMs
	}
	return row.pendingMs
}

/*
================
advancePremiumClock

One tick of an online owner's clock. Inside the param-job pass, so it runs
for the same tracked owners. Returns true when the clock was committed.
================
*/
func (rt *Runtime) advancePremiumClock(key petOwnerKey, character *enterworld.Character, nowMs int64) bool {
	clock := character.PremiumClock
	if clock == nil {
		return false
	}
	pending, due := rt.premiumSpend.take(key, clock, nowMs)
	rolled := (nowMs-clock.StartUnixMs)/premiumDayMs > clock.Day
	ended := nowMs >= clock.EndUnixMs
	if !due && !rolled && !ended {
		return false
	}
	committed := rt.deps.Update(character, "premium-clock", func() bool {
		if character.PremiumClock == nil {
			return false
		}
		if ended {
			character.PremiumClock = nil
			return true
		}
		next := *character.PremiumClock
		premiumClockSpend(&next, pending)
		premiumClockRoll(&next, nowMs)
		character.PremiumClock = &next
		return true
	})
	if committed {
		rt.premiumSpend.committed(key, nowMs)
	}
	return committed
}

/*
================
retirePremiumSpend

The owner leaves the world: its spend since the last commit is written,
and offline time is never spent.
================
*/
func (rt *Runtime) retirePremiumSpend(divisionID string, character *enterworld.Character, nowMs int64) {
	pending := rt.premiumSpend.drop(petOwnerKey{division: divisionID, name: character.Name}, nowMs)
	if pending <= 0 || character.PremiumClock == nil {
		return
	}
	rt.deps.Update(character, "premium-clock-departure", func() bool {
		if character.PremiumClock == nil {
			return false
		}
		next := *character.PremiumClock
		premiumClockSpend(&next, pending)
		character.PremiumClock = &next
		return true
	})
}
