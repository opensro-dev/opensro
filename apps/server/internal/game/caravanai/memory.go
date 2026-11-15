/*
===========================================================================

memory.go - Persistent Grudge & Trust memory book and route heat (port-only)

Maintains bounded per-agent relationship records (-100 Vendetta to +100
Trusted Ally) and regional ambush danger heatmaps, writing through a narrow
consumer-owned MemoryStore port.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
)

/*
================================================================================
Consumer-owned persistence port and MemoryBook
================================================================================
*/

// MemoryStore is the consumer-owned persistence port that caravanai uses to
// load and persist bounded relationship memories and route ambush heat.
type MemoryStore interface {
	LoadCaravanAgentMemories(division string) ([]domain.CaravanAgentMemoryRow, error)
	SaveCaravanAgentMemory(row domain.CaravanAgentMemoryRow) error
	LoadCaravanRouteHeat(division string) ([]domain.CaravanRouteHeatRow, error)
	SaveCaravanRouteHeat(row domain.CaravanRouteHeatRow) error
}

// MemoryBook caches agent->player memories and route danger heat per division
// while writing updates through to the optional MemoryStore.
type MemoryBook struct {
	mu       sync.RWMutex
	store    MemoryStore
	memories map[string]map[string]map[string]domain.CaravanAgentMemoryRow
	heat     map[string]map[uint16]domain.CaravanRouteHeatRow
}

/*
==================
NewMemoryBook

NewMemoryBook constructs a MemoryBook backed by store (may be nil for pure
in-memory operation).
==================
*/
func NewMemoryBook(store MemoryStore) *MemoryBook {
	return &MemoryBook{
		store:    store,
		memories: make(map[string]map[string]map[string]domain.CaravanAgentMemoryRow),
		heat:     make(map[string]map[uint16]domain.CaravanRouteHeatRow),
	}
}

/*
==================
LoadDivision

LoadDivision hydrates the in-memory cache for division from the backing
MemoryStore if one is configured.
==================
*/
func (b *MemoryBook) LoadDivision(division string) error {
	division = strings.TrimSpace(division)
	if division == "" || b.store == nil {
		return nil
	}
	memRows, err := b.store.LoadCaravanAgentMemories(division)
	if err != nil {
		return err
	}
	heatRows, err := b.store.LoadCaravanRouteHeat(division)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	agentMap := make(map[string]map[string]domain.CaravanAgentMemoryRow)
	for _, row := range memRows {
		byPlayer, ok := agentMap[row.AgentName]
		if !ok {
			byPlayer = make(map[string]domain.CaravanAgentMemoryRow)
			agentMap[row.AgentName] = byPlayer
		}
		byPlayer[row.PlayerName] = row
	}
	b.memories[division] = agentMap

	heatMap := make(map[uint16]domain.CaravanRouteHeatRow, len(heatRows))
	for _, row := range heatRows {
		heatMap[row.RegionID] = row
	}
	b.heat[division] = heatMap
	return nil
}

/*
==================
Relationship

Relationship returns the stored memory row that agentName holds for playerName
in division, if any.
==================
*/
func (b *MemoryBook) Relationship(division, agentName, playerName string) (domain.CaravanAgentMemoryRow, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	byAgent, ok := b.memories[strings.TrimSpace(division)]
	if !ok {
		return domain.CaravanAgentMemoryRow{}, false
	}
	byPlayer, ok := byAgent[strings.TrimSpace(agentName)]
	if !ok {
		return domain.CaravanAgentMemoryRow{}, false
	}
	row, ok := byPlayer[strings.TrimSpace(playerName)]
	return row, ok
}

/*
==================
RegionHeat

RegionHeat returns the current ambush danger score (0..100) for regionID in
division.
==================
*/
func (b *MemoryBook) RegionHeat(division string, regionID uint16) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	byRegion, ok := b.heat[strings.TrimSpace(division)]
	if !ok {
		return 0
	}
	return byRegion[regionID].DangerScore
}

/*
==================
HeatSnapshot

HeatSnapshot returns a copy of regionID -> DangerScore for route planning.
==================
*/
func (b *MemoryBook) HeatSnapshot(division string) map[uint16]int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	byRegion, ok := b.heat[strings.TrimSpace(division)]
	if !ok || len(byRegion) == 0 {
		return nil
	}
	out := make(map[uint16]int, len(byRegion))
	for regionID, row := range byRegion {
		out[regionID] = row.DangerScore
	}
	return out
}

/*
==================
RecordRobberyOrKill

RecordRobberyOrKill records that playerName killed or robbed agentName in
regionID, penalizing trust (-35), incrementing Deaths and GoldStolen, and
raising regional ambush danger (+25).
==================
*/
func (b *MemoryBook) RecordRobberyOrKill(
	division, agentName, playerName string,
	goldStolen int64,
	regionID uint16,
	note string,
	nowMs int64,
) domain.CaravanAgentMemoryRow {
	if note == "" {
		if goldStolen > 0 {
			note = fmt.Sprintf("Ambushed caravan in region %d (%d gold stolen)", regionID, goldStolen)
		} else {
			note = fmt.Sprintf("Slain in region %d", regionID)
		}
	}
	row := b.mutateMemory(division, agentName, playerName, func(cur *domain.CaravanAgentMemoryRow) {
		cur.TrustScore = domain.ClampCaravanTrust(cur.TrustScore - 35)
		cur.Deaths++
		if goldStolen > 0 {
			cur.GoldStolen += goldStolen
		}
		cur.LastRegion = regionID
		cur.LastNote = domain.TruncateCaravanNote(note)
		cur.UpdatedAtMs = nowMs
	})
	b.AddRegionDanger(division, regionID, 25, 1, nowMs)
	return row
}

/*
==================
RecordAgentKill

RecordAgentKill records that agentName defeated hostile playerName in combat.
==================
*/
func (b *MemoryBook) RecordAgentKill(
	division, agentName, playerName string,
	regionID uint16,
	note string,
	nowMs int64,
) domain.CaravanAgentMemoryRow {
	if note == "" {
		note = fmt.Sprintf("Defeated attacker in region %d", regionID)
	}
	return b.mutateMemory(division, agentName, playerName, func(cur *domain.CaravanAgentMemoryRow) {
		cur.TrustScore = domain.ClampCaravanTrust(cur.TrustScore - 15)
		cur.Kills++
		cur.LastRegion = regionID
		cur.LastNote = domain.TruncateCaravanNote(note)
		cur.UpdatedAtMs = nowMs
	})
}

/*
==================
RecordEscortSuccess

RecordEscortSuccess rewards hunterName (+25 trust) for defending or escorting
agentName's trade run to completion.
==================
*/
func (b *MemoryBook) RecordEscortSuccess(
	division, agentName, hunterName string,
	regionID uint16,
	nowMs int64,
) domain.CaravanAgentMemoryRow {
	return b.mutateMemory(division, agentName, hunterName, func(cur *domain.CaravanAgentMemoryRow) {
		cur.TrustScore = domain.ClampCaravanTrust(cur.TrustScore + 25)
		cur.LastRegion = regionID
		cur.LastNote = domain.TruncateCaravanNote("Escorted caravan safely to specialty shop")
		cur.UpdatedAtMs = nowMs
	})
}

/*
==================
RecordParleyBetrayal

RecordParleyBetrayal marks thiefName with a permanent -100 Betrayal Vendetta
for attacking agentName after a parley cargo bribe was dropped.
==================
*/
func (b *MemoryBook) RecordParleyBetrayal(
	division, agentName, thiefName string,
	regionID uint16,
	nowMs int64,
) domain.CaravanAgentMemoryRow {
	row := b.mutateMemory(division, agentName, thiefName, func(cur *domain.CaravanAgentMemoryRow) {
		cur.TrustScore = domain.CaravanTrustMin
		cur.LastRegion = regionID
		cur.LastNote = domain.TruncateCaravanNote("Betrayed parley truce after taking cargo bribe")
		cur.UpdatedAtMs = nowMs
	})
	b.AddRegionDanger(division, regionID, 35, 1, nowMs)
	return row
}

/*
==================
RecordParleyHonored

RecordParleyHonored records that thiefName accepted a parley cargo drop and
spared agentName's caravan (-10 mild grudge instead of a vendetta).
==================
*/
func (b *MemoryBook) RecordParleyHonored(
	division, agentName, thiefName string,
	goldDropped int64,
	regionID uint16,
	nowMs int64,
) domain.CaravanAgentMemoryRow {
	return b.mutateMemory(division, agentName, thiefName, func(cur *domain.CaravanAgentMemoryRow) {
		cur.TrustScore = domain.ClampCaravanTrust(cur.TrustScore - 10)
		if goldDropped > 0 {
			cur.GoldStolen += goldDropped
		}
		cur.LastRegion = regionID
		cur.LastNote = domain.TruncateCaravanNote(
			fmt.Sprintf("Honored parley truce in region %d (%d gold tribute)", regionID, goldDropped),
		)
		cur.UpdatedAtMs = nowMs
	})
}

/*
==================
AddRegionDanger

AddRegionDanger adjusts the ambush heat score (0..100) for regionID and
persists the updated row.
==================
*/
func (b *MemoryBook) AddRegionDanger(
	division string,
	regionID uint16,
	deltaScore int,
	deltaAmbushes int,
	nowMs int64,
) domain.CaravanRouteHeatRow {
	division = strings.TrimSpace(division)
	if regionID == 0 {
		return domain.CaravanRouteHeatRow{}
	}

	b.mu.Lock()
	byRegion, ok := b.heat[division]
	if !ok {
		byRegion = make(map[uint16]domain.CaravanRouteHeatRow)
		b.heat[division] = byRegion
	}
	cur := byRegion[regionID]
	cur.Division = division
	cur.RegionID = regionID
	cur.DangerScore += deltaScore
	if cur.DangerScore < 0 {
		cur.DangerScore = 0
	} else if cur.DangerScore > 100 {
		cur.DangerScore = 100
	}
	if deltaAmbushes > 0 {
		cur.Ambushes += deltaAmbushes
	}
	cur.UpdatedAtMs = nowMs
	byRegion[regionID] = cur
	if len(byRegion) > domain.MaxCaravanRouteHeatRegions {
		evictOldestRegionHeat(byRegion)
	}
	b.mu.Unlock()

	if b.store != nil && division != "" {
		_ = b.store.SaveCaravanRouteHeat(cur)
	}
	return cur
}

func (b *MemoryBook) mutateMemory(
	division, agentName, playerName string,
	fn func(cur *domain.CaravanAgentMemoryRow),
) domain.CaravanAgentMemoryRow {
	division = strings.TrimSpace(division)
	agentName = strings.TrimSpace(agentName)
	playerName = strings.TrimSpace(playerName)

	b.mu.Lock()
	byAgent, ok := b.memories[division]
	if !ok {
		byAgent = make(map[string]map[string]domain.CaravanAgentMemoryRow)
		b.memories[division] = byAgent
	}
	byPlayer, ok := byAgent[agentName]
	if !ok {
		byPlayer = make(map[string]domain.CaravanAgentMemoryRow)
		byAgent[agentName] = byPlayer
	}
	cur := byPlayer[playerName]
	cur.Division = division
	cur.AgentName = agentName
	cur.PlayerName = playerName
	fn(&cur)
	byPlayer[playerName] = cur
	if len(byPlayer) > domain.MaxCaravanMemoriesPerAgent {
		evictOldestAgentMemory(byPlayer)
	}
	b.mu.Unlock()

	if b.store != nil && division != "" && agentName != "" && playerName != "" {
		_ = b.store.SaveCaravanAgentMemory(cur)
	}
	return cur
}

func evictOldestAgentMemory(byPlayer map[string]domain.CaravanAgentMemoryRow) {
	var oldestKey string
	var oldestTs int64
	first := true
	for k, v := range byPlayer {
		if first || v.UpdatedAtMs < oldestTs {
			oldestKey = k
			oldestTs = v.UpdatedAtMs
			first = false
		}
	}
	if !first {
		delete(byPlayer, oldestKey)
	}
}

func evictOldestRegionHeat(byRegion map[uint16]domain.CaravanRouteHeatRow) {
	var oldestKey uint16
	var oldestTs int64
	first := true
	for k, v := range byRegion {
		if first || v.UpdatedAtMs < oldestTs {
			oldestKey = k
			oldestTs = v.UpdatedAtMs
			first = false
		}
	}
	if !first {
		delete(byRegion, oldestKey)
	}
}
