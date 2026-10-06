/*
===========================================================================

authority_test.go - native score-limit and deadline precedence

===========================================================================
*/
package guildwar

import (
	"errors"
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
settlementStore
================
*/
type settlementStore struct {
	domain.GuildWarStore
	row    domain.GuildWarRecord
	winner int64
	fail   bool
}

/*
================
GuildWars
================
*/
func (s *settlementStore) GuildWars(string) ([]domain.GuildWarRecord, error) {
	return []domain.GuildWarRecord{s.row}, nil
}

/*
================
AccountGuildWarCombat
================
*/
func (s *settlementStore) AccountGuildWarCombat(_ string, c domain.GuildWarCombat) (domain.GuildWarRecord, uint8, error) {
	s.row.Scores[0] += uint32(c.Score)
	return s.row, 0, nil
}

/*
================
EndGuildWar
================
*/
func (s *settlementStore) EndGuildWar(_ string, _ uint32, winner int64) (uint8, error) {
	if s.fail {
		return 2, errors.New("injected settlement failure")
	}
	s.winner = winner
	return 0, nil
}

/*
================
TestNativeSettlementPrecedenceAndFailure
================
*/
func TestNativeSettlementPrecedenceAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name             string
		scores           [2]uint32
		index            uint8
		end, now, winner int64
		fail             bool
	}{
		{"before-deadline", [2]uint32{0, 100}, 0, 1000, 999, 0, false},
		{"deadline-tie-is-B", [2]uint32{0, 100}, 0, 1000, 1000, 8, false},
		{"score-limit-before-time", [2]uint32{4900, 6000}, 1, 2000, 1000, 7, false},
		{"score-limit-overrides-deadline-leader", [2]uint32{4900, 6000}, 1, 1000, 1000, 7, false},
		{"committed-score-survives-end-failure", [2]uint32{4900, 6000}, 1, 1000, 1000, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &settlementStore{row: domain.GuildWarRecord{ID: 1, Guilds: [2]int64{7, 8}, Scores: tc.scores, ScoreIndex: tc.index, EndMs: tc.end}, fail: tc.fail}
			a, err := New("world", s)
			if err != nil {
				t.Fatal(err)
			}
			row, winner, code, err := a.Combat(domain.GuildWarCombat{WarID: 1, Score: 100}, tc.now)
			if code != 0 || winner != tc.winner || (err != nil) != tc.fail || row.Scores[0] != tc.scores[0]+100 {
				t.Fatalf("row %+v winner %d code %d err %v", row, winner, code, err)
			}
			active, exists := a.Find("world", 7, 8)
			if (winner == 0) != exists || exists && active.Scores != row.Scores {
				t.Fatalf("publication %+v %v", active, exists)
			}
		})
	}
}
