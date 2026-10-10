/*
===========================================================================

fortress_item_forge_test.go - production orders through the fortress
authority and the store's commits

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/fortress"
)

// forgeTestSmith classifies every item but the two trainer refs as the
// smith's, as the v1.150 forge groups do.
var forgeTestSmith fortress.ForgeKind = func(ref uint32) bool { return ref != 19569 && ref != 19570 }

/*
================
forgeFixture

A store with a fortress held by a two-member guild: the master and a
member without a fortress role.
================
*/
func forgeFixture(t *testing.T, dir string) (*Store, *fortress.Authority, *domain.Character, *domain.Character, int64) {
	t.Helper()
	s := openTest(t, dir, newTestClock())
	leader, member := guildTestCharacter("forgemaster"), guildTestCharacter("forgemember")
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
	return s, a, leader, member, id
}

/*
================
setForgeRole
================
*/
func setForgeRole(t *testing.T, s *Store, leader, member *domain.Character, role uint8, gp uint32) {
	t.Helper()
	_, refused := s.Guilds().UpdateGuildAs(testDivision, leader.ID, "forge-fixture", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.GP = gp
			for i := range m {
				if m[i].CharID == member.ID {
					m[i].FortressRole = role
				}
			}
			return g, m, true
		})
	if refused.Refused() {
		t.Fatal(refused)
	}
}

/*
================
TestFortressItemForgeStartChargesAndOrders

Refusals in 632660's order after the period and fortress: the holder's
member needs the staff role, then gold, then guild points; a second order
for the same staff is busy. The payment and the order commit together.
================
*/
func TestFortressItemForgeStartChargesAndOrders(t *testing.T) {
	s, a, leader, member, id := forgeFixture(t, t.TempDir())
	order := domain.FortressItemForgeRecord{FortressID: 1, ItemRefID: 19227, Count: 10, StartedAtMs: 1000, EndsAtMs: 601000}
	start := func(actor int64, gold int64, gp uint32) uint8 {
		t.Helper()
		code, err := a.StartItemForge(testDivision, forgeTestSmith,
			domain.FortressItemForgeStart{Forge: order, ActorID: actor, Role: 0x08, Gold: gold, GP: gp})
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	if code := start(member.ID, 1, 1); code != domain.FortressForgeErrRole {
		t.Fatalf("roleless member: %#x", code)
	}
	setForgeRole(t, s, leader, member, 0x10, 0)
	if code := start(member.ID, 1, 1); code != domain.FortressForgeErrRole {
		t.Fatalf("trainer role on the smith: %#x", code)
	}
	setForgeRole(t, s, leader, member, 0x08, 500)
	if code := start(member.ID, 1, 1); code != domain.FortressForgeErrGold {
		t.Fatalf("no gold: %#x", code)
	}
	gold := int64(400000)
	s.UpdateCharacters([]*domain.Character{member}, "forge-fixture", func() bool { member.Gold = &gold; return true })
	if code := start(member.ID, 324170, 5900); code != domain.FortressForgeErrGP {
		t.Fatalf("short GP: %#x", code)
	}
	if code := start(member.ID, 324170, 500); code != 0 {
		t.Fatalf("start: %#x", code)
	}
	g, _, _ := s.Guilds().Guild(testDivision, id)
	if *member.Gold != 400000-324170 || g.GP != 0 {
		t.Fatalf("payment gold=%d GP=%d", *member.Gold, g.GP)
	}
	if code := start(leader.ID, 0, 0); code != domain.FortressForgeErrBusy {
		t.Fatalf("second smith order: %#x", code)
	}
	a.SetPeriod(testDivision, fortress.PeriodWar, true)
	if code := start(leader.ID, 0, 0); code != domain.FortressForgeErrWar {
		t.Fatalf("war period: %#x", code)
	}
	a.SetPeriod(testDivision, fortress.PeriodWar, false)
	if _, err := s.db.Exec("CREATE TRIGGER reject_forge BEFORE INSERT ON fortress_item_forges BEGIN SELECT RAISE(ABORT, 'forge failure'); END"); err != nil {
		t.Fatal(err)
	}
	trainer := func(ref uint32) bool { return !forgeTestSmith(ref) }
	code, err := a.StartItemForge(testDivision, trainer, domain.FortressItemForgeStart{Forge: domain.FortressItemForgeRecord{
		FortressID: 1, ItemRefID: 19569, Count: 1, EndsAtMs: 1}, ActorID: leader.ID, Role: 0x10, Gold: 0, GP: 0})
	if code != domain.FortressForgeErrFailure || err == nil {
		t.Fatalf("failed commit: %#x %v", code, err)
	}
	if _, _, present, _ := a.ItemForge(testDivision, 1, trainer); present {
		t.Fatal("a failed commit published the order")
	}
}

/*
================
TestFortressItemForgeCollectAndRestart

Collection waits for the tick that marks the order done, writes the bag
row with the remaining count and survives a restart; the last collection
removes the order. A cancelled order refunds nothing.
================
*/
func TestFortressItemForgeCollectAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, a, leader, _, id := forgeFixture(t, dir)
	setForgeRole(t, s, leader, leader, 0, 0)
	order := domain.FortressItemForgeRecord{FortressID: 1, ItemRefID: 19227, Count: 10, StartedAtMs: 1000, EndsAtMs: 601000}
	if code, err := a.StartItemForge(testDivision, forgeTestSmith, domain.FortressItemForgeStart{Forge: order, ActorID: leader.ID, Role: 0x08}); code != 0 || err != nil {
		t.Fatalf("start %#x %v", code, err)
	}
	row := func(slot int64) domain.FortressItemForgeCollect {
		return domain.FortressItemForgeCollect{Forge: order, ActorID: leader.ID, Role: 0x08,
			Item: domain.InventoryRow{Slot: slot, RefObjID: 19227, Codename: "ITEM_ETC_SIEGE_TEST", StackCount: 3}}
	}
	if code, _ := a.CollectItemForge(testDivision, forgeTestSmith, row(20), 3); code != domain.FortressForgeErrNotDone {
		t.Fatalf("collect while running: %#x", code)
	}
	a.AdvanceItemForges(600999)
	if _, forge, _, _ := a.ItemForge(testDivision, 1, forgeTestSmith); forge.Done {
		t.Fatal("done before its end")
	}
	a.AdvanceItemForges(601000)
	if code, _ := a.CollectItemForge(testDivision, forgeTestSmith, row(20), 11); code != domain.FortressForgeErrQuantity {
		t.Fatalf("over-collect: %#x", code)
	}
	if code, err := a.CollectItemForge(testDivision, forgeTestSmith, row(20), 3); code != 0 || err != nil {
		t.Fatalf("collect %#x %v", code, err)
	}
	if code, _ := a.CollectItemForge(testDivision, forgeTestSmith, row(20), 3); code != domain.FortressForgeErrBag {
		t.Fatalf("collect into an occupied slot: %#x", code)
	}
	s.Close()
	s = openTest(t, dir, newTestClock())
	a = fortress.New([]fortress.Catalog{{ID: 1}})
	if err := a.Restore(testDivision, s.Fortresses()); err != nil {
		t.Fatal(err)
	}
	a.Occupy(testDivision, 1, id)
	_, forge, present, _ := a.ItemForge(testDivision, 1, forgeTestSmith)
	if !present || !forge.Done || forge.Count != 7 {
		t.Fatalf("restored order %+v present=%v", forge, present)
	}
	s.mu.RLock()
	stored := s.characterByIDLocked(testDivision, leader.ID)
	collected := len(stored.MissionInventory) > 0 && stored.MissionInventory[len(stored.MissionInventory)-1].StackCount == 3
	s.mu.RUnlock()
	if !collected {
		t.Fatal("the collected stack did not survive the restart")
	}
	if code, err := a.CollectItemForge(testDivision, forgeTestSmith, row(21), 7); code != 0 || err != nil {
		t.Fatalf("last collect %#x %v", code, err)
	}
	if _, _, present, _ := a.ItemForge(testDivision, 1, forgeTestSmith); present {
		t.Fatal("the last collection left the order")
	}
	trainer := func(ref uint32) bool { return !forgeTestSmith(ref) }
	before, _, _ := s.Guilds().Guild(testDivision, id)
	if code, err := a.StartItemForge(testDivision, trainer, domain.FortressItemForgeStart{Forge: domain.FortressItemForgeRecord{
		FortressID: 1, ItemRefID: 19569, Count: 2, EndsAtMs: 9}, ActorID: leader.ID, Role: 0x10}); code != 0 || err != nil {
		t.Fatalf("trainer start %#x %v", code, err)
	}
	if code, _ := a.CancelItemForge(testDivision, 1, trainer, 19570); code != domain.FortressForgeErrUnknown {
		t.Fatalf("cancel another item: %#x", code)
	}
	if code, err := a.CancelItemForge(testDivision, 1, trainer, 19569); code != 0 || err != nil {
		t.Fatalf("cancel %#x %v", code, err)
	}
	if code, _ := a.CancelItemForge(testDivision, 1, trainer, 19569); code != domain.FortressForgeErrNone {
		t.Fatalf("cancel nothing: %#x", code)
	}
	after, _, _ := s.Guilds().Guild(testDivision, id)
	if after.GP != before.GP {
		t.Fatal("cancel refunded guild points")
	}
	rows, err := s.Fortresses().(domain.FortressItemForgeStore).FortressItemForges(testDivision)
	if err != nil || len(rows) != 0 {
		t.Fatalf("stored orders %+v %v", rows, err)
	}
}

/*
================
TestItemForgeUpgradeAddsTheProductionTable

A schema 20 authority at layout 7 upgrades to 21 with the empty production
table, its records untouched; a second upgrade has nothing to do.
================
*/
func TestItemForgeUpgradeAddsTheProductionTable(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	downgradeToLayout7(t, dir)
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preItemForgeVersion)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the upgrade rewrote the character")
	}
	rows, err := reopened.Fortresses().(domain.FortressItemForgeStore).FortressItemForges(testDivision)
	if err != nil || len(rows) != 0 {
		t.Fatalf("fresh production table %+v %v", rows, err)
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
