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
	"encoding/json"
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
		return true
	})
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

Older authorities have no pool key and start at zero. Unknown fields or
negative funds are corruption, not a reason to silently discard the pool.
================
*/
func loadTradeRewards(db *sql.DB) (map[string]domain.TradeRewardPool, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeyTradeRewards).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]domain.TradeRewardPool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var pools map[string]domain.TradeRewardPool
	if err := decodeJSONStrict([]byte(raw), &pools); err != nil {
		return nil, fmt.Errorf("trade reward pools: %w", err)
	}
	for division, pool := range pools {
		if division == "" || strings.TrimSpace(division) != division || pool.Hunters < 0 || pool.Thieves < 0 {
			return nil, fmt.Errorf("invalid trade reward pool for division %q", division)
		}
	}
	return pools, nil
}

/*
================
writeTradeRewards
================
*/
func writeTradeRewards(tx *sql.Tx, pools map[string]domain.TradeRewardPool) error {
	if len(pools) == 0 {
		return nil
	}
	raw, err := json.Marshal(pools)
	if err != nil {
		return err
	}
	return upsertMetaTx(tx, metaKeyTradeRewards, string(raw))
}
