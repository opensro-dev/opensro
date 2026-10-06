/*
===========================================================================

guildwars_test.go - durable stakes, score accounting and settlement rollback

===========================================================================
*/
package store

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/social/guildwar"
	"testing"
)

/*
================
TestGuildWarAtomicLifecycle
================
*/
func TestGuildWarAtomicLifecycle(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	var masters [2]*domain.Character
	var guilds [2]int64
	for side, name := range []string{"Red", "Blue"} {
		c := guildTestCharacter(name + "master")
		gold := int64(1000)
		c.Gold = &gold
		if err := s.CreateCharacter(testDivision, name, c); err != nil {
			t.Fatal(err)
		}
		id, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: name, Level: 1}, domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name, Grade: 0}, c)
		if err != nil {
			t.Fatal(err)
		}
		masters[side], guilds[side] = c, id
	}
	start := domain.GuildWarStart{Masters: [2]int64{masters[0].ID, masters[1].ID}, Record: domain.GuildWarRecord{Guilds: guilds, Stake: 1000, ScoreIndex: 1, EndMs: 2000}}
	a, err := guildwar.New(testDivision, s.GuildWars())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_war BEFORE UPDATE ON guild_wars BEGIN SELECT RAISE(ABORT, 'war failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, code, err := a.Begin(start); code != 2 || err == nil {
		t.Fatalf("rollback %d %v", code, err)
	}
	if *masters[0].Gold != 1000 || *masters[1].Gold != 1000 || len(a.Wars(testDivision, guilds[0])) != 0 {
		t.Fatal("failed begin leaked payment")
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_war"); err != nil {
		t.Fatal(err)
	}
	war, code, err := a.Begin(start)
	if code != 0 || err != nil {
		t.Fatalf("begin %d %v", code, err)
	}
	if *masters[0].Gold != 500 || *masters[1].Gold != 500 {
		t.Fatal("stakes not paid together")
	}
	if _, code, err := a.Begin(start); code != 0x3a || err != nil {
		t.Fatalf("duplicate %d %v", code, err)
	}
	if _, refusal := s.Guilds().DissolveGuildAs(testDivision, masters[0].ID); refusal != domain.GuildRefusalWarActive {
		t.Fatalf("disband %d", refusal)
	}
	if len(a.Expired(2000)) != 0 {
		t.Fatal("expired tied war must remain active")
	}
	combat := domain.GuildWarCombat{WarID: war.ID, KillerID: masters[0].ID, VictimID: masters[1].ID, Score: 100}
	row, winner, code, err := a.Combat(combat, 1999)
	if code != 0 || err != nil || winner != 0 || row.Scores != [2]uint32{100, 0} {
		t.Fatalf("combat %+v %d %d %v", row, winner, code, err)
	}
	if len(a.Expired(2000)) != 1 {
		t.Fatal("expired unequal war not offered for settlement")
	}
	s.Close()
	s = openTest(t, dir, newTestClock())
	a, err = guildwar.New(testDivision, s.GuildWars())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Find(testDivision, guilds[0], guilds[1]); !ok {
		t.Fatal("reopen lost hostility")
	}
	accounts, err := s.GuildWars().GuildWarMemberScores(testDivision, guilds[0])
	if err != nil || len(accounts) != 1 || accounts[0].Score != 100 || accounts[0].Kills != 1 {
		t.Fatalf("member account %+v %v", accounts, err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_end BEFORE DELETE ON guild_wars BEGIN SELECT RAISE(ABORT, 'end failure'); END"); err != nil {
		t.Fatal(err)
	}
	if code, err := a.End(war.ID, guilds[0]); code != 2 || err == nil {
		t.Fatalf("end rollback %d %v", code, err)
	}
	g, _, _ := s.Guilds().Guild(testDivision, guilds[0])
	if g.WarCompensation != 0 || len(a.Wars(testDivision, guilds[0])) != 1 {
		t.Fatal("failed end leaked compensation")
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_end"); err != nil {
		t.Fatal(err)
	}
	if code, err := a.End(war.ID, guilds[0]); code != 0 || err != nil {
		t.Fatalf("end %d %v", code, err)
	}
	g, _, _ = s.Guilds().Guild(testDivision, guilds[0])
	if g.WarCompensation != 1000 || len(a.Wars(testDivision, guilds[0])) != 0 {
		t.Fatal("settlement missing")
	}
	if code, err := a.End(war.ID, guilds[0]); code != 2 || err != nil {
		t.Fatalf("duplicate end %d %v", code, err)
	}
	paid, refusal := s.Guilds().ClaimWarCompensationAs(testDivision, masters[0].ID)
	if refusal.Refused() || paid != 1000 {
		t.Fatalf("claim %d %d", paid, refusal)
	}
	if err := validateGuildWars(s.db); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestGuildWarNativeTermsAndScores
================
*/
func TestGuildWarNativeTermsAndScores(t *testing.T) {
	for _, test := range []struct {
		delta int64
		score uint8
	}{{-7, 1}, {-6, 1}, {-5, 25}, {0, 100}, {10, 250}, {11, 251}} {
		if got := guildwar.KillScore(50+test.delta, 50); got != test.score {
			t.Fatalf("delta %d: %d", test.delta, got)
		}
	}
	for _, test := range []struct {
		period uint32
		valid  bool
	}{{30<<10 | 23<<15 | 50<<20, true}, {31 << 10, false}, {24 << 15, false}, {51 << 20, false}, {0x7fffffff, true}, {0xffffffff, true}} {
		if got := guildwar.ValidTerms(domain.GuildWarTerms{Period: test.period}); got != test.valid {
			t.Fatalf("period %x: %v", test.period, got)
		}
	}
	if guildwar.Deadline(1<<10|2<<15|30<<20, 1000) != 95401000 {
		t.Fatal("duration field packing")
	}
}
