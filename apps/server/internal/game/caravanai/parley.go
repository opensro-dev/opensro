/*
===========================================================================

parley.go - Cornered AI Trader Bribe / Parley truce system (port-only)

When an AI Trader is cornered (< 35% HP, no nearby Hunter allies), it can
halt and drop 30%-50% of its transport specialty cargo onto the ground with a
15-second truce window. Thieves who stop attacking and take the tribute earn
only a mild (-10) grudge; thieves who deal damage during the parley window
trigger a permanent -100 Betrayal Vendetta.

===========================================================================
*/
package caravanai

import (
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Parley constants and state tracker
================================================================================
*/

const (
	// ParleyWindowMs is the 15-second truce window granted after an AI
	// Trader drops its cargo bribe onto the road.
	ParleyWindowMs int64 = 15_000

	// ParleyCooldownMs prevents an AI Trader from offering repeated parleys
	// inside a 2-minute window.
	ParleyCooldownMs int64 = 120_000
)

// ParleyOffer represents an active or recently resolved cargo-bribe truce.
type ParleyOffer struct {
	Division         string
	TraderName       string
	ThiefName        string
	RegionID         uint16
	DroppedStacks    int
	DroppedGoldValue int64
	StartedAtMs      int64
	ExpiresAtMs      int64
	Betrayed         bool
}

// ParleyTracker manages active parley truce windows and cooldowns per agent.
type ParleyTracker struct {
	mu        sync.Mutex
	active    map[string]ParleyOffer
	cooldowns map[string]int64
}

/*
==================
NewParleyTracker

NewParleyTracker returns an initialized ParleyTracker.
==================
*/
func NewParleyTracker() *ParleyTracker {
	return &ParleyTracker{
		active:    make(map[string]ParleyOffer),
		cooldowns: make(map[string]int64),
	}
}

/*
==================
ComputeParleyCargoDrop

ComputeParleyCargoDrop returns the number of specialty cargo stacks (30%-50%
of totalStacks, minimum 1 when totalStacks > 0) that an AI Trader drops onto
the ground to bargain for its life.
==================
*/
func ComputeParleyCargoDrop(totalStacks int, personality Personality) int {
	if totalStacks <= 0 {
		return 0
	}
	percent := 40
	switch personality {
	case PersonalityGreedy:
		percent = 30
	case PersonalityCautious:
		percent = 50
	}
	drop := (totalStacks * percent) / 100
	if drop < 1 {
		drop = 1
	}
	if drop > totalStacks {
		drop = totalStacks
	}
	return drop
}

/*
==================
CanOfferParley

CanOfferParley reports whether traderName is off parley cooldown and not
already inside an active parley window at nowMs.
==================
*/
func (p *ParleyTracker) CanOfferParley(division, traderName string, nowMs int64) bool {
	key := parleyKey(division, traderName)
	p.mu.Lock()
	defer p.mu.Unlock()
	if offer, ok := p.active[key]; ok && nowMs < offer.ExpiresAtMs {
		return false
	}
	return nowMs >= p.cooldowns[key]
}

/*
==================
StartParley

StartParley opens a 15-second truce window for traderName and records the
cargo bribe offered to thiefName.
==================
*/
func (p *ParleyTracker) StartParley(
	division, traderName, thiefName string,
	regionID uint16,
	droppedStacks int,
	droppedGoldValue int64,
	nowMs int64,
) ParleyOffer {
	offer := ParleyOffer{
		Division:         strings.TrimSpace(division),
		TraderName:       strings.TrimSpace(traderName),
		ThiefName:        strings.TrimSpace(thiefName),
		RegionID:         regionID,
		DroppedStacks:    droppedStacks,
		DroppedGoldValue: droppedGoldValue,
		StartedAtMs:      nowMs,
		ExpiresAtMs:      nowMs + ParleyWindowMs,
	}
	key := parleyKey(division, traderName)
	p.mu.Lock()
	p.active[key] = offer
	p.cooldowns[key] = nowMs + ParleyCooldownMs
	p.mu.Unlock()
	return offer
}

/*
==================
OnDamageDuringParley

OnDamageDuringParley checks if traderName is inside an active parley window at
nowMs. If so, it marks the parley as betrayed, removes the active truce, and
records a -100 Betrayal Vendetta against attackerName in memories.
==================
*/
func (p *ParleyTracker) OnDamageDuringParley(
	division, traderName, attackerName string,
	nowMs int64,
	memories *MemoryBook,
) (ParleyOffer, domain.CaravanAgentMemoryRow, bool) {
	key := parleyKey(division, traderName)
	p.mu.Lock()
	offer, ok := p.active[key]
	if !ok || nowMs >= offer.ExpiresAtMs {
		p.mu.Unlock()
		return ParleyOffer{}, domain.CaravanAgentMemoryRow{}, false
	}
	offer.Betrayed = true
	if attackerName != "" {
		offer.ThiefName = strings.TrimSpace(attackerName)
	}
	delete(p.active, key)
	p.mu.Unlock()

	var memRow domain.CaravanAgentMemoryRow
	if memories != nil && offer.ThiefName != "" {
		memRow = memories.RecordParleyBetrayal(
			offer.Division,
			offer.TraderName,
			offer.ThiefName,
			offer.RegionID,
			nowMs,
		)
	}
	return offer, memRow, true
}

/*
==================
ResolveExpired

ResolveExpired finalizes any active parley offers whose 15-second truce window
has elapsed without betrayal at nowMs, recording an honored parley in
memories.
==================
*/
func (p *ParleyTracker) ResolveExpired(nowMs int64, memories *MemoryBook) []ParleyOffer {
	p.mu.Lock()
	var honored []ParleyOffer
	for key, offer := range p.active {
		if nowMs < offer.ExpiresAtMs {
			continue
		}
		delete(p.active, key)
		if !offer.Betrayed {
			honored = append(honored, offer)
		}
	}
	p.mu.Unlock()

	if memories != nil {
		for _, offer := range honored {
			if offer.ThiefName != "" {
				memories.RecordParleyHonored(
					offer.Division,
					offer.TraderName,
					offer.ThiefName,
					offer.DroppedGoldValue,
					offer.RegionID,
					nowMs,
				)
			}
		}
	}
	return honored
}

func parleyKey(division, traderName string) string {
	return strings.TrimSpace(division) + "|" + strings.TrimSpace(traderName)
}
