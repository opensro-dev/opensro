/*
===========================================================================

fortress_staff_test.go - employment atomically charges both resource owners

===========================================================================
*/
package store

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/fortress"
	"testing"
)

/*
================
TestFortressStaffHireDurableAndOrdered
================
*/
func TestFortressStaffHireDurableAndOrdered(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	leader, member := guildTestCharacter("hiremaster"), guildTestCharacter("hiremember")
	for _, c := range []*domain.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	id, _, _ := seedTestGuild(t, s, leader, member)
	a := fortress.New([]fortress.Catalog{{ID: 1}})
	if err := a.Restore(testDivision, s.Fortresses()); err != nil {
		t.Fatal(err)
	}
	a.Occupy(testDivision, 1, id)
	check := func(actor int64, flags, expected uint8) {
		t.Helper()
		code, err := a.HireStaff(testDivision, 1, actor, flags)
		if err != nil || code != expected {
			t.Fatalf("hire actor=%d flags=%d: %d %v want %d", actor, flags, code, err, expected)
		}
	}
	check(member.ID, 1, 7)
	check(leader.ID, 1, 15)
	gold := int64(90000)
	s.UpdateCharacters([]*domain.Character{leader}, "hire-fixture", func() bool { leader.Gold = &gold; return true })
	check(leader.ID, 1, 14)
	_, refused := s.Guilds().UpdateGuildAs(testDivision, leader.ID, "hire-fixture", domain.GuildAuthorization{}, func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
		g.GP = 9000
		return g, m, true
	})
	if refused.Refused() {
		t.Fatal(refused)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_staff BEFORE INSERT ON fortresses BEGIN SELECT RAISE(ABORT, 'staff failure'); END"); err != nil {
		t.Fatal(err)
	}
	if code, err := a.HireStaff(testDivision, 1, leader.ID, 1); code != 2 || err == nil {
		t.Fatalf("failed commit: %d %v", code, err)
	}
	unchanged, _ := a.Get(testDivision, 1)
	unchangedGuild, _, _ := s.Guilds().Guild(testDivision, id)
	if unchanged.StaffFlags != 0 || *leader.Gold != 90000 || unchangedGuild.GP != 9000 {
		t.Fatal("failed hire published partial payment or flags")
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_staff"); err != nil {
		t.Fatal(err)
	}
	a.SetPeriod(testDivision, fortress.PeriodWar, true)
	check(leader.ID, 1, 24)
	a.SetPeriod(testDivision, fortress.PeriodWar, false)
	check(leader.ID, 1, 0)
	check(leader.ID, 3, 9)
	check(leader.ID, 6, 0)
	check(leader.ID, 0, 0)
	row, _ := a.Get(testDivision, 1)
	g, _, _ := s.Guilds().Guild(testDivision, id)
	if row.StaffFlags != 7 || *leader.Gold != 0 || g.GP != 0 {
		t.Fatalf("hire state %+v gold=%d GP=%d", row, *leader.Gold, g.GP)
	}
	check(leader.ID, 1, 9)
	s.Close()
	s = openTest(t, dir, newTestClock())
	a = fortress.New([]fortress.Catalog{{ID: 1}})
	if err := a.Restore(testDivision, s.Fortresses()); err != nil {
		t.Fatal(err)
	}
	row, _ = a.Get(testDivision, 1)
	g, _, _ = s.Guilds().Guild(testDivision, id)
	s.mu.RLock()
	stored := s.characterByIDLocked(testDivision, leader.ID)
	gotGold := *stored.Gold
	s.mu.RUnlock()
	if row.StaffFlags != 7 || gotGold != 0 || g.GP != 0 {
		t.Fatalf("reopen flags=%d gold=%d GP=%d", row.StaffFlags, gotGold, g.GP)
	}
	a.Capture(testDivision, 1, id, 0)
	row, _ = a.Get(testDivision, 1)
	if row.StaffFlags != 0 {
		t.Fatal("capture retained employment")
	}
}
