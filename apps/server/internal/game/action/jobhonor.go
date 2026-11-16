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

A thief's steal decays per victim (the spec's per-victim decay): the
thief's account scores a victim trader's goods in full the first time in
an honor day, at half the second, and not at all after. A thieves'
merchant pays for the goods' whole value, so without the decay a
player's own trader alt on another account could buy a dear stack, have
the player's thief take it and score a fresh run four times a day at no
risk. The victim is the account behind the goods' trade owner (the native
owner alias), not the alias: one account holds four characters, and
four trader aliases would be four victims. Several stacks of one load
picked up into one unsold run are one steal.
A steal also needs a real loss (owner, 2026-11-16): the thief's account
must have killed the trader, or the trader's transport, whose death
scattered the goods. Goods a monster's kill, a logout or another
player dropped score nothing, so no alt can hand goods over for free.
The steal counts are memory only: a restart forgets them, as it forgets
the open runs. INFERENCE: worth a restart's reset, which the operator
alone can cause.

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
	"opensro.online/server/internal/platform/jobhonor"
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
	// honorStealsScored is how many steals of one victim in one honor
	// day score at all; each after the first scores half the one before.
	honorStealsScored = 2
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

Each character's current run, by character id, and the day's steal
counts. Its zero value is ready; the composition root never touches it.
================
*/
type honorRuns struct {
	mu     sync.Mutex
	runs   map[int64]*honorRun
	steals map[honorSteal]int
}

// honorRun is one run: the points it scored, and for a thief's haul the
// victim and the halvings its steal decay applies (honorStealsScored or
// more scores nothing). sold marks a haul that has reached a sale, so a
// later pickup opens a new steal even when the sale scored nothing.
type honorRun struct {
	sold   bool
	scored int
	victim string
	decay  int
}

// honorSteal keys the per-victim decay: the thief's account, the
// victim's account (goodsVictim), the honor day.
type honorSteal struct {
	account string
	victim  string
	day     int64
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
	delete(rt.jobHonorRuns.runs, c.ID)
}

/*
================
goodsVictim

The decay key of goods owned by alias: the account of the division's
character carrying that job alias. An alias no character carries any
more (deleted, or renamed since the goods were bought) keys by itself,
so it still decays. The walk reads live character records, so it runs
inside the division's read door (the roster contract), and is called
after the pickup's update door has closed.
================
*/
func (rt *Runtime) goodsVictim(division, alias string) string {
	if alias == "" {
		return ""
	}
	victim := "alias:" + strings.ToLower(alias)
	rt.deps.Read(division, func() {
		for _, other := range rt.deps.CharactersForDivision(division) {
			if other != nil && other.AccountID != "" && strings.EqualFold(other.Job.Alias, alias) {
				victim = honorAccountVictim(other.AccountID)
				return
			}
		}
	})
	return victim
}

/*
================
honorAccountVictim

The decay key of an account's goods.
================
*/
func honorAccountVictim(account string) string {
	return "account:" + account
}

/*
================
openStealRun

A thief's haul of victim's goods (goodsVictim's key), lost to the
account lostTo (grounditem.Item.LostTo; "" when no player's kill
dropped them). More of the same victim's goods into a run that has not
sold yet are the same steal; otherwise a new run opens and the steal
counts toward the victim's decay for the day. Only a real loss scores
and counts: goods the thief's own account did not kill for open a run
that scores nothing.
================
*/
func (rt *Runtime) openStealRun(c *enterworld.Character, victim, lostTo string, at time.Time) {
	if rt == nil || rt.JobHonor == nil || c == nil {
		return
	}
	rt.jobHonorRuns.mu.Lock()
	defer rt.jobHonorRuns.mu.Unlock()
	runs := &rt.jobHonorRuns
	// A steal scores only on a real loss (owner, 2026-11-16): goods the
	// thief's own account took by killing their trader or its transport
	// (grounditem.Item.LostTo). Goods dropped any other way - a monster's
	// kill, a logout, another player's kill - cost the thief nothing to
	// take, so a player's own trader alt could feed them. One such pickup
	// spoils the whole unsold haul: a sale cannot be split by stack, so
	// mixing free goods into a real haul would otherwise score them.
	realLoss := lostTo != "" && lostTo == c.AccountID
	run := runs.runs[c.ID]
	if run != nil && !run.sold && run.victim == victim {
		if !realLoss {
			run.decay = honorStealsScored
		}
		return
	}
	day := jobhonor.DayStart(at).Unix()
	if runs.runs == nil {
		runs.runs = map[int64]*honorRun{}
	}
	if runs.steals == nil {
		runs.steals = map[honorSteal]int{}
	}
	// Yesterday's counts can never apply again.
	for key := range runs.steals {
		if key.day != day {
			delete(runs.steals, key)
		}
	}
	// Goods of the thief's own account are no steal: with several
	// characters online at once, one account could run a trader and a
	// thief together. The run scores nothing and counts toward nothing.
	if victim == honorAccountVictim(c.AccountID) || !realLoss {
		runs.runs[c.ID] = &honorRun{victim: victim, decay: honorStealsScored}
		return
	}
	key := honorSteal{account: c.AccountID, victim: victim, day: day}
	runs.runs[c.ID] = &honorRun{victim: victim, decay: runs.steals[key]}
	runs.steals[key]++
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
	if rt.jobHonorRuns.runs == nil {
		rt.jobHonorRuns.runs = map[int64]*honorRun{}
	}
	run := rt.jobHonorRuns.runs[c.ID]
	if run == nil {
		run = &honorRun{}
		rt.jobHonorRuns.runs[c.ID] = run
	}
	if job == domain.JobThief {
		run.sold = true
		// A thief's sale with no steal behind it (goods the thief never
		// hauled this run) scores nothing; a decayed steal halves.
		if run.victim == "" || run.decay >= honorStealsScored {
			return 0
		}
		points >>= run.decay
	}
	points = min(points, honorRunPointCap-run.scored)
	if points <= 0 {
		return 0
	}
	kept := rt.JobHonor.AwardJobHonor(c.AccountID, c.ID, int(job), points, at)
	run.scored += kept
	return kept
}
