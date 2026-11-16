/*
===========================================================================

jobhonor.go - scoring job honor from server-verified job events (#3530)

Port-only, not native (SRO_JOB_HONOR, owner-approved spec 2026-11-15; the
later reference server has no job honor, #3530 native check). With the
flag on the composition root installs platform/jobhonor as the sink; off
(the default) the sink is nil and nothing scores.

This slice scores the trade sale: the profit the native sale already
computes (tradeSaleValue's profit, the job EXP input), never the goods'
value, so buying the dearest stack scores nothing by itself. A thief's
sale at the thieves' merchant scores at a richer rate, since the goods
were taken at risk. One run scores at most honorRunPointCap: a run is
the sales between two loads of goods, so a run opens when its trader
buys goods or its thief picks some up. The account's daily cap lives in
the ledger, shared by every character of the account.

The hunter's escort damage, the job kill decay, the hot route and the
ambush are later slices of #3530.

===========================================================================
*/
package action

import (
	"os"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// EnvJobHonor turns the port-only job honor on ("on", "1", "true").
// Off, the native default, scores nothing.
const EnvJobHonor = "SRO_JOB_HONOR"

// The scoring rates. INFERENCE (#3530 left the numbers to the build):
// a level-cap three-star run in v1.150 clears a few million gold of
// profit, so 10,000 profit a point lets one good run reach the run cap
// and five runs the daily cap; a thief's point costs half, the spec's
// "more per steal than a trader's run".
const (
	honorTraderGoldPerPoint = 10_000
	honorThiefGoldPerPoint  = 5_000
	honorRunPointCap        = 250
)

/*
================
JobHonorSink

Where scored points go: platform/jobhonor's Service. It applies the
account's daily cap and returns the points it kept. Memory only: it runs
after a trade commits, outside the store door.
================
*/
type JobHonorSink interface {
	AwardJobHonor(account string, characterID int64, job int, points int, at time.Time) int
}

/*
================
JobHonorFromEnv

Reads EnvJobHonor. Off is the native default.
================
*/
func JobHonorFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobHonor))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
honorRuns

The points each character's current run has scored, by character id.
Its zero value is ready; the composition root never touches it.
================
*/
type honorRuns struct {
	mu     sync.Mutex
	scored map[int64]int
}

/*
================
tradeHonorPoints

The points one sale's profit scores for the suited job, before any cap.
Only a trader's or a thief's profitable sale scores.
================
*/
func tradeHonorPoints(job uint8, profit int64) int {
	if profit <= 0 {
		return 0
	}
	switch job {
	case domain.JobTrader:
		return int(min(profit/honorTraderGoldPerPoint, honorRunPointCap))
	case domain.JobThief:
		return int(min(profit/honorThiefGoldPerPoint, honorRunPointCap))
	}
	return 0
}

/*
================
openHonorRun

A load of goods opens a new run for the character.
================
*/
func (rt *Runtime) openHonorRun(c *enterworld.Character) {
	if rt == nil || rt.JobHonor == nil || c == nil {
		return
	}
	rt.jobHonorRuns.mu.Lock()
	defer rt.jobHonorRuns.mu.Unlock()
	delete(rt.jobHonorRuns.scored, c.ID)
}

/*
================
scoreTradeSale

Scores one committed trade sale: its points are cut to what the run has
left, then to what the account's day has left (the sink). The run is
charged only what the sink kept.
================
*/
func (rt *Runtime) scoreTradeSale(c *enterworld.Character, job uint8, profit int64, at time.Time) int {
	if rt == nil || rt.JobHonor == nil || c == nil || c.ID <= 0 || c.AccountID == "" {
		return 0
	}
	points := tradeHonorPoints(job, profit)
	if points == 0 {
		return 0
	}
	rt.jobHonorRuns.mu.Lock()
	defer rt.jobHonorRuns.mu.Unlock()
	if rt.jobHonorRuns.scored == nil {
		rt.jobHonorRuns.scored = map[int64]int{}
	}
	points = min(points, honorRunPointCap-rt.jobHonorRuns.scored[c.ID])
	if points <= 0 {
		return 0
	}
	kept := rt.JobHonor.AwardJobHonor(c.AccountID, c.ID, int(job), points, at)
	rt.jobHonorRuns.scored[c.ID] += kept
	return kept
}
