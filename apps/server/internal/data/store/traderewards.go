/*
===========================================================================

traderewards.go - trade settlement across characters and the shard reward pool

The existing authority commit writes cargo, party payouts and the global
pool in one SQLite transaction. A refused callback publishes neither pool
changes nor character changes. The caller must validate before mutation.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const metaKeyTradeRewards = "tradeRewards"

/*
================
UpdateTrade
================
*/
func (s *Store) UpdateTrade(cs []*domain.Character, label string, update func(*domain.TradeRewardPool) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(cs) == 0 || update == nil || cs[0] == nil {
		return false
	}
	division, known := s.charDivision[cs[0]]
	if !known {
		return false
	}
	for _, c := range cs {
		if shard, present := s.charDivision[c]; !present || shard != division {
			return false
		}
	}
	pool := s.meta.TradeRewards[division]
	return s.updateCharactersLocked(cs, label, func() bool {
		if !update(&pool) {
			return false
		}
		if s.meta.TradeRewards == nil {
			s.meta.TradeRewards = map[string]domain.TradeRewardPool{}
		}
		s.meta.TradeRewards[division] = pool
		s.changes.tradeRewards = true
		return true
	}, false)
}

/*
================
TradeRewards
================
*/
func (s *Store) TradeRewards(division string) domain.TradeRewardPool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta.TradeRewards[division]
}

/*
================
loadTradeRewards

Older authorities have no pool key and start at zero. Negative funds are
corruption, not a reason to silently discard the pool. A pool's unknown
top-level fields are returned by division for the next write (json.go).
================
*/
func loadTradeRewards(db *sql.DB) (map[string]domain.TradeRewardPool, map[string]unknownFields, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeyTradeRewards).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]domain.TradeRewardPool{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var pools map[string]domain.TradeRewardPool
	unknown, err := decodePlaneFields([]byte(raw), &pools)
	if err != nil {
		return nil, nil, fmt.Errorf("trade reward pools: %w", err)
	}
	for division, pool := range pools {
		if division == "" || strings.TrimSpace(division) != division || pool.Hunters < 0 || pool.Thieves < 0 {
			return nil, nil, fmt.Errorf("invalid trade reward pool for division %q", division)
		}
	}
	return pools, unknown, nil
}

/*
================
tradeRewardsRecord

The meta row's bytes for the pool plane; empty when no pool exists, in
which case no writer puts a row down. unknown holds the pools' stored
unknown fields by division.
================
*/
func tradeRewardsRecord(pools map[string]domain.TradeRewardPool, unknown map[string]unknownFields) (string, error) {
	if len(pools) == 0 {
		return "", nil
	}
	raw, err := encodePlaneFields(pools, unknown)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

/*
================
writeTradeRewards
================
*/
func writeTradeRewards(tx *sql.Tx, pools map[string]domain.TradeRewardPool) error {
	record, err := tradeRewardsRecord(pools, nil)
	if err != nil || record == "" {
		return err
	}
	return upsertMetaTx(tx, metaKeyTradeRewards, record)
}
