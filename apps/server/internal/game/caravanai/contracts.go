/*
===========================================================================

contracts.go - Two-way Mercenary Escort Contracts (port-only)

Manages two-way mercenary contracts between players and AI caravan agents:
 1. AI Traders departing on 3★-5★ runs recruit real Hunter players in town
    and pay bonus gold + trust upon delivery at the destination shop.
 2. Real Trader players in town hire AI Hunters to follow and defend their
    caravan across the Jangan-Donwhang-Hotan highway.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"
	"sync"
)

/*
================================================================================
Mercenary contract records and ContractBook
================================================================================
*/

const (
	// BaseHunterHireFeeGold is the base gold fee for a real Trader player to
	// hire an AI Hunter escort for one town-to-town leg.
	BaseHunterHireFeeGold int64 = 15_000

	// BaseAITraderEscortRewardPerStar is multiplied by the AI Trader's cargo
	// star rating (3★-5★) to compute the bonus gold paid to a real Hunter
	// player upon safe arrival at the destination specialty shop.
	BaseAITraderEscortRewardPerStar int64 = 20_000
)

// EscortContract represents an active mercenary escort agreement.
type EscortContract struct {
	Division    string
	TraderName  string
	TraderIsAI  bool
	HunterName  string
	HunterIsAI  bool
	FromTown    TownID
	ToTown      TownID
	CargoStars  uint8
	RewardGold  int64
	UpfrontFee  int64
	StartedAtMs int64
	Active      bool
}

// ContractBook tracks active escort contracts per division.
type ContractBook struct {
	mu        sync.RWMutex
	contracts map[string]EscortContract
}

/*
==================
NewContractBook

NewContractBook creates an empty mercenary ContractBook.
==================
*/
func NewContractBook() *ContractBook {
	return &ContractBook{
		contracts: make(map[string]EscortContract),
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
Trader (aiTraderName) from fromTown to toTown.
==================
*/
func (c *ContractBook) BindAITraderEscort(
	division, aiTraderName, hunterName string,
	fromTown, toTown TownID,
	cargoStars uint8,
	nowMs int64,
) EscortContract {
	reward := ComputeAITraderEscortReward(cargoStars, fromTown, toTown)
	contract := EscortContract{
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
	c.contracts[contractKey(division, aiTraderName)] = contract
	c.mu.Unlock()
	return contract
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
the destination specialty shop, returning the completed contract and recording
+25 trust in memories.
==================
*/
func (c *ContractBook) CompleteAITraderRun(
	division, aiTraderName string,
	destRegionID uint16,
	nowMs int64,
	memories *MemoryBook,
) (EscortContract, bool) {
	key := contractKey(division, aiTraderName)
	c.mu.Lock()
	contract, ok := c.contracts[key]
	if !ok || !contract.Active {
		c.mu.Unlock()
		return EscortContract{}, false
	}
	contract.Active = false
	delete(c.contracts, key)
	c.mu.Unlock()

	if memories != nil && !contract.HunterIsAI && contract.HunterName != "" {
		memories.RecordEscortSuccess(
			contract.Division,
			contract.TraderName,
			contract.HunterName,
			destRegionID,
			nowMs,
		)
	}
	return contract, true
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
