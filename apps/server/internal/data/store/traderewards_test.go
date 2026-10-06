/*
===========================================================================

traderewards_test.go - durable, shard-isolated trade reward contributions

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestTradeRewardPoolCommitsWithCharactersAndSurvivesReopen
================
*/
func TestTradeRewardPoolCommitsWithCharactersAndSurvivesReopen(t *testing.T) {
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := guildTestCharacter("trader")
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	if !s.UpdateTrade([]*domain.Character{c}, "sale", func(pool *domain.TradeRewardPool) bool {
		if !pool.Credit(domain.JobTrader, 17) {
			return false
		}
		gold := int64(178)
		c.Gold = &gold
		return true
	}) {
		t.Fatal("sale refused")
	}
	if s.UpdateTrade([]*domain.Character{c}, "refused", func(pool *domain.TradeRewardPool) bool {
		pool.Credit(domain.JobThief, 50)
		return false
	}) {
		t.Fatal("refused sale accepted")
	}
	if got := s.TradeRewards(testDivision); got != (domain.TradeRewardPool{Hunters: 17}) {
		t.Fatalf("pool %+v", got)
	}
	if got := s.TradeRewards("other"); got != (domain.TradeRewardPool{}) {
		t.Fatalf("cross-shard pool %+v", got)
	}
	s.Close()
	s = openTest(t, dir, clock)
	if got := s.TradeRewards(testDivision); got != (domain.TradeRewardPool{Hunters: 17}) {
		t.Fatalf("reopened pool %+v", got)
	}
	loaded, err := loadDB(s.db, CurrentVersion, CurrentLayoutVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got := *loaded.characters[testDivision][0].Gold; got != 178 {
		t.Fatalf("reopened gold %d", got)
	}
}

/*
================
TestTradeRewardPoolRejectsUnknownShardAndInvalidRecord
================
*/
func TestTradeRewardPoolRejectsUnknownShardAndInvalidRecord(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	if _, err := s.db.Exec("INSERT INTO meta (key, value) VALUES (?, ?)", metaKeyTradeRewards, `{"lost":{"hunters":3,"thieves":2}}`); err != nil {
		t.Fatal(err)
	}
	pools, err := loadTradeRewards(s.db)
	if err != nil {
		t.Fatal(err)
	}
	s.meta.TradeRewards = pools
	if err := s.ValidateShardState([]string{testDivision}); err == nil {
		t.Fatal("unknown reward shard accepted")
	}
	if _, err := s.db.Exec("UPDATE meta SET value = ? WHERE key = ?", `{"lost":{"hunters":-1,"thieves":2}}`, metaKeyTradeRewards); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTradeRewards(s.db); err == nil {
		t.Fatal("negative pool accepted")
	}
}

/*
================
TestTradeRewardPoolAndGoldCannotCommitSeparately

Abort the character write after the pool metadata has been written inside
the transaction. The authority retains its pending in-memory update for the
normal write-health retry, but disk must contain neither half until retry.
================
*/
func TestTradeRewardPoolAndGoldCannotCommitSeparately(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := guildTestCharacter("poolfailure")
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_trade BEFORE UPDATE ON characters BEGIN SELECT RAISE(ABORT, 'trade test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if !s.UpdateTrade([]*domain.Character{c}, "sale-failed-write", func(pool *domain.TradeRewardPool) bool {
		if !pool.Credit(domain.JobThief, 22) {
			return false
		}
		gold := int64(227)
		c.Gold = &gold
		return true
	}) {
		t.Fatal("admitted mutation refused")
	}
	pools, err := loadTradeRewards(s.db)
	if err != nil || pools[testDivision] != (domain.TradeRewardPool{}) {
		t.Fatalf("half-committed pool %+v: %v", pools, err)
	}
	var committedGold int64
	if err := s.db.QueryRow("SELECT COALESCE(json_extract(record, '$.gold'), 0) FROM characters WHERE id = ? AND division = ?", c.ID, testDivision).Scan(&committedGold); err != nil {
		t.Fatal(err)
	}
	if committedGold == 227 {
		t.Fatal("failed trade persisted gold")
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_trade"); err != nil {
		t.Fatal(err)
	}
	if !s.UpdateCharacters([]*domain.Character{c}, "retry", func() bool { return true }) {
		t.Fatal("retry refused")
	}
	pools, err = loadTradeRewards(s.db)
	if err != nil || pools[testDivision] != (domain.TradeRewardPool{Thieves: 22}) {
		t.Fatalf("retried pool %+v: %v", pools, err)
	}
	if err := s.db.QueryRow("SELECT json_extract(record, '$.gold') FROM characters WHERE id = ? AND division = ?", c.ID, testDivision).Scan(&committedGold); err != nil || committedGold != 227 {
		t.Fatalf("retried gold %d: %v", committedGold, err)
	}
}
