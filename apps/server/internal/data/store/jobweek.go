/*
===========================================================================

jobweek.go - the job guilds' weekly close and its ranking snapshot

One authority commit closes a division's job week (domain.CloseJobWeek):
every job member's reward and contribution, the thief and hunter pools and
the new ranking snapshot. The snapshot lives in metadata beside the pools,
as the shard database kept Tab_RefRanking_* beside _TrijobRewards.

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

const metaKeyJobRankings = "jobRankings"

/*
================
CloseJobWeek

Closes the division's job week once: false when week is not after the
snapshot's, or when a first sighting only records the week (a shard has
no "last week" until one has ended under it).
================
*/
func (s *Store) CloseJobWeek(division string, week int64, label string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.meta.JobRankings[division]
	if week <= previous.Week {
		return false
	}
	if s.meta.JobRankings == nil {
		s.meta.JobRankings = map[string]domain.JobRankings{}
	}
	var members []*domain.Character
	for _, c := range s.characters[division] {
		if c != nil && c.Job.Type != domain.JobNone {
			members = append(members, c)
		}
	}
	if len(members) == 0 {
		// Nothing to close; the week rides along with the next commit, as
		// every commit writes the metadata.
		previous.Week = week
		s.meta.JobRankings[division] = previous
		return true
	}
	return s.updateCharactersLocked(members, label, func() bool {
		if previous.Week == 0 {
			previous.Week = week
			s.meta.JobRankings[division] = previous
			return true
		}
		pool := s.meta.TradeRewards[division]
		next := domain.CloseJobWeek(members, &pool, previous, week)
		if s.meta.TradeRewards == nil {
			s.meta.TradeRewards = map[string]domain.TradeRewardPool{}
		}
		s.meta.TradeRewards[division] = pool
		s.meta.JobRankings[division] = next
		return true
	})
}

/*
================
JobRankings
================
*/
func (s *Store) JobRankings(division string) domain.JobRankings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta.JobRankings[division]
}

/*
================
loadJobRankings

Authorities before schema 19 have no snapshot and start at week 0.
================
*/
func loadJobRankings(db *sql.DB) (map[string]domain.JobRankings, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeyJobRankings).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]domain.JobRankings{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rankings map[string]domain.JobRankings
	if err := decodeJSONStrict([]byte(raw), &rankings); err != nil {
		return nil, fmt.Errorf("job rankings: %w", err)
	}
	for division, ranking := range rankings {
		if division == "" || strings.TrimSpace(division) != division || ranking.Week < 0 {
			return nil, fmt.Errorf("invalid job rankings for division %q", division)
		}
		for _, lists := range ranking.Lists {
			for _, list := range lists {
				if len(list) > domain.JobRankRows {
					return nil, fmt.Errorf("job rankings for division %q exceed %d rows", division, domain.JobRankRows)
				}
			}
		}
	}
	return rankings, nil
}

/*
================
writeJobRankings
================
*/
func writeJobRankings(tx *sql.Tx, rankings map[string]domain.JobRankings) error {
	if len(rankings) == 0 {
		return nil
	}
	raw, err := json.Marshal(rankings)
	if err != nil {
		return err
	}
	return upsertMetaTx(tx, metaKeyJobRankings, string(raw))
}
