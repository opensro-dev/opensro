/*
===========================================================================

contracts.go - Two-way Mercenary Escort Contracts (port-only)

Manages two-way mercenary contracts between players and AI caravan agents:
 1. AI Traders departing on 3★-5★ runs recruit real Hunter players in town
    and pay bonus gold + trust upon delivery at the destination shop.
 2. Real Trader players in town hire AI Hunters to follow and defend their
    caravan across the Jangan-Donwhang-Hotan highway.

Slice 5 (#3704) makes the money side abuse-proof: an AI-trader escort
pays only when the Hunter stayed within escort range for the whole run
and the trader lived to deliver (NoteEscortWatch, VoidTraderEscort),
one Hunter holds one AI-trader escort at a time, and paid escorts are
counted per Hunter per UTC day against MaxPaidEscortsPerDay in the
EscortDayStore ledger. The amounts are the sketch's; the cap is a
placeholder for the owner's call.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Mercenary contract records and ContractBook
================================================================================
*/

const (
	// BaseHunterHireFeeGold is the base gold fee for a real Trader player to
	// hire an AI Hunter escort for one town-to-town leg.
	BaseHunterHireFeeGold int64 = 1_500_000

	// BaseAITraderEscortRewardPerStar is multiplied by the AI Trader's cargo
	// star rating (3★-5★) to compute the bonus gold paid to a real Hunter
	// player upon safe arrival at the destination specialty shop.
	BaseAITraderEscortRewardPerStar int64 = 20_000

	// MaxPaidEscortsPerDay caps how many AI-trader escort rewards one
	// Hunter may be paid in one UTC day; further completed escorts that
	// day complete unpaid. PLACEHOLDER for the owner's call (#3704
	// slice 5): no number is authored anywhere yet.
	MaxPaidEscortsPerDay = 3
	// escortMinRunMs is the shortest run that can pay: a trader bound at
	// its destination's gate earns nothing.
	escortMinRunMs = 30_000
	// escortNearPercent is the share of the run the Hunter must be
	// sampled within escort range of the trader.
	escortNearPercent = 90
	// escortSampleGapMs is the longest gap between two samples; a longer
	// one (the Hunter offline, the tick stalled) invalidates the run.
	escortSampleGapMs = 5000
)

/*
================
EscortDayStore

The consumer-owned ledger port behind the daily cap: one row per
Hunter per day counting the escort rewards already paid. Nil keeps
the counts in memory only (tests); the shard's store persists them
(store layout 17), so a restart cannot reset the day.
================
*/
type EscortDayStore interface {
	LoadCaravanEscortDays(division string) ([]domain.CaravanEscortDayRow, error)
	SaveCaravanEscortDay(row domain.CaravanEscortDayRow) error
}

// EscortContract represents an active mercenary escort agreement.
type EscortContract struct {
	Division     string
	TraderName   string
	TraderIsAI   bool
	HunterName   string
	HunterIsAI   bool
	FromTown     TownID
	ToTown       TownID
	CargoStars   uint8
	RewardGold   int64
	UpfrontFee   int64
	StartedAtMs  int64
	Active       bool
	LastSampleMs int64
	NearMs       int64
	Invalid      bool
}

// paidEscortKey names one Hunter's paid-escort count for one UTC day.
type paidEscortKey struct {
	division, hunter, day string
}

// ContractBook tracks active escort contracts per division.
type ContractBook struct {
	mu        sync.RWMutex
	contracts map[string]EscortContract
	// days is the daily-cap ledger port (nil = in-memory only).
	days EscortDayStore
	// paid counts paid escorts per Hunter per day, loaded lazily per
	// division from days; dirty holds the rows Flush still owes.
	paid      map[paidEscortKey]int
	dirtyDays map[paidEscortKey]domain.CaravanEscortDayRow
	loaded    map[string]bool
}

/*
==================
NewContractBook

NewContractBook creates an empty mercenary ContractBook. days may be
nil, in which case the daily cap counts this process's payments only.
==================
*/
func NewContractBook(days EscortDayStore) *ContractBook {
	return &ContractBook{
		contracts: make(map[string]EscortContract),
		days:      days,
		paid:      make(map[paidEscortKey]int),
		dirtyDays: make(map[paidEscortKey]domain.CaravanEscortDayRow),
		loaded:    make(map[string]bool),
	}
}

/*
==================
ComputeAITraderEscortReward

ComputeAITraderEscortReward calculates the gold payout an AI Trader offers a
real Hunter player for escorting a cargoStars (1..5) trade run.
==================
*/
func ComputeAITraderEscortReward(cargoStars uint8, from, to TownID) int64 {
	if cargoStars == 0 {
		cargoStars = 1
	}
	if cargoStars > 5 {
		cargoStars = 5
	}
	hops := int64(1)
	if (from == TownJangan && to == TownHotan) || (from == TownHotan && to == TownJangan) {
		hops = 2
	}
	return int64(cargoStars) * BaseAITraderEscortRewardPerStar * hops
}

/*
==================
BindAITraderEscort

BindAITraderEscort binds a real Hunter player (hunterName) to escort an AI
Trader (aiTraderName) from fromTown to toTown. One contract per Hunter
and per trader: a Hunter who already holds an active escort, or a
trader already under one, is refused (bound is false) until that run
ends, so one Hunter cannot farm every departing trader at once.
==================
*/
func (c *ContractBook) BindAITraderEscort(
	division, aiTraderName, hunterName string,
	fromTown, toTown TownID,
	cargoStars uint8,
	nowMs int64,
) (contract EscortContract, bound bool) {
	reward := ComputeAITraderEscortReward(cargoStars, fromTown, toTown)
	contract = EscortContract{
		Division:    strings.TrimSpace(division),
		TraderName:  strings.TrimSpace(aiTraderName),
		TraderIsAI:  true,
		HunterName:  strings.TrimSpace(hunterName),
		HunterIsAI:  false,
		FromTown:    fromTown,
		ToTown:      toTown,
		CargoStars:  cargoStars,
		RewardGold:  reward,
		StartedAtMs: nowMs,
		Active:      true,
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.contracts {
		if existing.Division == contract.Division && existing.Active && (strings.EqualFold(existing.HunterName, contract.HunterName) || strings.EqualFold(existing.TraderName, contract.TraderName)) {
			return EscortContract{}, false
		}
	}
	contract.LastSampleMs = nowMs
	c.contracts[contractKey(division, aiTraderName)] = contract
	return contract, true
}

/*
==================
HireAIHunter

HireAIHunter binds an AI Hunter (aiHunterName) to escort a real Trader player
(playerTraderName) on their caravan run.
==================
*/
func (c *ContractBook) HireAIHunter(
	division, playerTraderName, aiHunterName string,
	fromTown, toTown TownID,
	nowMs int64,
) EscortContract {
	contract := EscortContract{
		Division:    strings.TrimSpace(division),
		TraderName:  strings.TrimSpace(playerTraderName),
		TraderIsAI:  false,
		HunterName:  strings.TrimSpace(aiHunterName),
		HunterIsAI:  true,
		FromTown:    fromTown,
		ToTown:      toTown,
		UpfrontFee:  BaseHunterHireFeeGold,
		StartedAtMs: nowMs,
		Active:      true,
	}
	c.mu.Lock()
	c.contracts[contractKey(division, aiHunterName)] = contract
	c.mu.Unlock()
	return contract
}

/*
==================
CompleteAITraderRun

CompleteAITraderRun closes an active AI Trader escort contract on arrival at
the destination specialty shop. payable is the payment rule: the run
lasted escortMinRunMs, the Hunter was sampled near the trader for at
least escortNearPercent of it, and no sample gap exceeded
escortSampleGapMs (SampleEscort). A trader who died was cancelled at
its respawn and never completes. Only a payable completion records the
+25 escort trust; the caller still owes the daily-cap check before any
gold moves.
==================
*/
func (c *ContractBook) CompleteAITraderRun(
	division, aiTraderName string,
	destRegionID uint16,
	nowMs int64,
	memories *MemoryBook,
) (contract EscortContract, completed bool, payable bool) {
	key := contractKey(division, aiTraderName)
	c.mu.Lock()
	contract, ok := c.contracts[key]
	if !ok || !contract.Active {
		c.mu.Unlock()
		return EscortContract{}, false, false
	}
	contract.Active = false
	delete(c.contracts, key)
	c.mu.Unlock()
	elapsed := nowMs - contract.StartedAtMs
	payable = !contract.Invalid && elapsed >= escortMinRunMs &&
		contract.NearMs*100 >= elapsed*escortNearPercent && nowMs-contract.LastSampleMs <= escortSampleGapMs
	if payable && memories != nil && !contract.HunterIsAI && contract.HunterName != "" {
		memories.RecordEscortSuccess(
			contract.Division,
			contract.TraderName,
			contract.HunterName,
			destRegionID,
			nowMs,
		)
	}
	return contract, true, payable
}

/*
==================
ActiveContractFor

ActiveContractFor returns the active contract keyed by agentName (either an AI
Trader being escorted or an AI Hunter hired by a player), if any.
==================
*/
func (c *ContractBook) ActiveContractFor(division, agentName string) (EscortContract, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	contract, ok := c.contracts[contractKey(division, agentName)]
	if !ok || !contract.Active {
		return EscortContract{}, false
	}
	return contract, true
}

/*
==================
FormatContractReply

FormatContractReply formats an in-character confirmation chat message when a
mercenary contract is formed.
==================
*/
func FormatContractReply(contract EscortContract) string {
	if contract.TraderIsAI {
		return fmt.Sprintf(
			"Contract signed with %s! Escort my %d-star caravan from %s to %s for %d bonus gold on delivery.",
			contract.HunterName, contract.CargoStars, contract.FromTown, contract.ToTown, contract.RewardGold,
		)
	}
	return fmt.Sprintf(
		"My blade is yours, %s! I will guard your caravan along the road from %s.",
		contract.TraderName, contract.FromTown,
	)
}

func contractKey(division, agentName string) string {
	return strings.TrimSpace(division) + "|" + strings.TrimSpace(agentName)
}

/*
================
SampleEscort

Missing/offline/dead/wrong-job escorts and observation gaps invalidate a run.
================
*/
func (c *ContractBook) SampleEscort(division, agent string, near bool, nowMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := contractKey(division, agent)
	contract, ok := c.contracts[key]
	if !ok || !contract.Active || !contract.TraderIsAI {
		return
	}
	delta := nowMs - contract.LastSampleMs
	if delta < 0 || delta > escortSampleGapMs {
		contract.Invalid = true
	}
	if near && delta > 0 {
		contract.NearMs += min(delta, escortSampleGapMs)
	}
	contract.LastSampleMs = nowMs
	c.contracts[key] = contract
}

/*
================
Cancel
================
*/
func (c *ContractBook) Cancel(division, agent string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.contracts, contractKey(division, agent))
}

/*
================
HasHiredHunter
================
*/
func (c *ContractBook) HasHiredHunter(division, player string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, contract := range c.contracts {
		if contract.Division == division && contract.Active && contract.HunterIsAI && strings.EqualFold(contract.TraderName, player) {
			return true
		}
	}
	return false
}

/*
================================================================================
The daily paid-escort ledger (slice 5)
================================================================================
*/

// escortDay is the UTC calendar day a payment counts against.
func escortDay(nowMs int64) string {
	return time.UnixMilli(nowMs).UTC().Format("2006-01-02")
}

/*
==================
ensureDayLoadedLocked

Loads a division's ledger rows once. A store fault degrades the cap to
this process's counts rather than blocking caravans. Callers hold c.mu.
==================
*/
func (c *ContractBook) ensureDayLoadedLocked(division string) {
	division = strings.TrimSpace(division)
	if c.loaded[division] {
		return
	}
	c.loaded[division] = true
	if c.days == nil {
		return
	}
	rows, err := c.days.LoadCaravanEscortDays(division)
	if err != nil {
		return
	}
	for _, row := range rows {
		key := paidEscortKey{division: row.Division, hunter: strings.ToLower(row.HunterName), day: row.Day}
		if row.PaidCount > c.paid[key] {
			c.paid[key] = row.PaidCount
		}
	}
}

/*
==================
PaidEscortsToday

How many AI-trader escort rewards hunterName has already been paid in
the division on nowMs's UTC day.
==================
*/
func (c *ContractBook) PaidEscortsToday(division, hunterName string, nowMs int64) int {
	key := paidEscortKey{
		division: strings.TrimSpace(division),
		hunter:   strings.ToLower(strings.TrimSpace(hunterName)),
		day:      escortDay(nowMs),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureDayLoadedLocked(division)
	return c.paid[key]
}

/*
==================
RecordPaidEscort

Counts one paid escort reward against the Hunter's day and marks the
row for the next Flush.
==================
*/
func (c *ContractBook) RecordPaidEscort(division, hunterName string, nowMs int64) {
	key := paidEscortKey{
		division: strings.TrimSpace(division),
		hunter:   strings.ToLower(strings.TrimSpace(hunterName)),
		day:      escortDay(nowMs),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureDayLoadedLocked(division)
	c.paid[key]++
	c.dirtyDays[key] = domain.CaravanEscortDayRow{
		Division:   key.division,
		HunterName: hunterName,
		Day:        key.day,
		PaidCount:  c.paid[key],
	}
}

/*
==================
Flush

Writes every dirty ledger row to the EscortDayStore, newest value per
key. Called off the tick by the runtime's flush loop and once more by
Close, with MemoryBook.Flush's retry discipline: a failed row goes
back to the dirty set unless a newer value arrived meanwhile.
==================
*/
func (c *ContractBook) Flush() error {
	if c == nil || c.days == nil {
		return nil
	}
	c.mu.Lock()
	dirty := c.dirtyDays
	c.dirtyDays = make(map[paidEscortKey]domain.CaravanEscortDayRow)
	c.mu.Unlock()

	var first error
	for key, row := range dirty {
		if err := c.days.SaveCaravanEscortDay(row); err != nil {
			if first == nil {
				first = err
			}
			c.mu.Lock()
			if _, newer := c.dirtyDays[key]; !newer {
				c.dirtyDays[key] = row
			}
			c.mu.Unlock()
		}
	}
	return first
}
