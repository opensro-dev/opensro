/*
===========================================================================

rankings_test.go - every visible player, ranked and paged

===========================================================================
*/
package publicstats

import (
	"net/http"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
rankingNames
================
*/
func rankingNames(t *testing.T, s *Service, path string) RankingsResponse {
	t.Helper()
	var out RankingsResponse
	if code := get(t, s, path, &out); code != http.StatusOK {
		t.Fatalf("%s answered %d", path, code)
	}
	return out
}

/*
================
TestRankingsOrderEveryVisiblePlayer

Hidden players are left out; each ordering follows its keys; a race filter
and paging slice the same ordering.
================
*/
func TestRankingsOrderEveryVisiblePlayer(t *testing.T) {
	f := newFixture()
	lvl := func(n int64) *int64 { return &n }
	exp := func(n int64) *int64 { return &n }
	f.chars[0].Experience = exp(50)
	// Three level-30 players: the latest login ranks first, ahead of more EXP
	// and an earlier milestone; one never logged in since tracking, last.
	f.chars = append(f.chars,
		&domain.Character{ID: 4, Name: "Idle", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(30), Experience: exp(90),
			LevelReachedAt: map[uint8]int64{30: 1000}, LastLoginAtMs: 2000},
		&domain.Character{ID: 5, Name: "Active", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(30), Experience: exp(10),
			LevelReachedAt: map[uint8]int64{30: 9000}, LastLoginAtMs: 8000},
		&domain.Character{ID: 6, Name: "Absent", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(30), Experience: exp(99)})
	s := f.service()
	s.src.ExpRequired = func(level int64) (int64, bool) { return 100, true }

	byLevel := rankingNames(t, s, "/public/v1/rankings")
	names := []string{}
	for _, row := range byLevel.Rows {
		names = append(names, row.Name)
	}
	if want := "Kekw,Lune,Active,Idle,Absent"; join(names) != want {
		t.Fatalf("by level %s, want %s (Shy hidden)", join(names), want)
	}
	if byLevel.Total != 5 || byLevel.Pages != 1 || byLevel.Rows[0].Rank != 1 || byLevel.Rows[0].ExpPercent != 50 {
		t.Fatalf("by level header %+v", byLevel)
	}
	if byLevel.Rows[2].ReachedAt == nil || byLevel.Rows[0].ReachedAt != nil {
		t.Fatal("reachedAt should exist only for a recorded milestone level")
	}

	byUniques := rankingNames(t, s, "/public/v1/rankings?by=uniques")
	if byUniques.Rows[0].Name != "Kekw" || byUniques.Rows[0].UniqueKills != 2 {
		t.Fatalf("by uniques %+v", byUniques.Rows[0])
	}
	byGear := rankingNames(t, s, "/public/v1/rankings?by=gear")
	// Without item grades every piece is degree 0 and unsealed: points are the plus.
	if g := byGear.Rows[0].Gear; byGear.Rows[0].Name != "Kekw" || g.PlusTotal != 7 || g.BestPlus != 7 || g.Points != 7 {
		t.Fatalf("by gear %+v", byGear.Rows[0])
	}
	european := rankingNames(t, s, "/public/v1/rankings?race=european")
	if len(european.Rows) != 1 || european.Rows[0].Name != "Lune" || european.Rows[0].Rank != 1 || european.Race == nil {
		t.Fatalf("european %+v", european)
	}
	if past := rankingNames(t, s, "/public/v1/rankings?page=2"); len(past.Rows) != 0 || past.Pages != 1 {
		t.Fatalf("page past the end %+v", past)
	}
	// The largest page Atoi accepts must not overflow the slice bound.
	if huge := rankingNames(t, s, "/public/v1/rankings?page=9223372036854775807"); len(huge.Rows) != 0 {
		t.Fatalf("a huge page listed %d rows", len(huge.Rows))
	}
	if after := rankingNames(t, s, "/public/v1/rankings"); len(after.Rows) == 0 {
		t.Fatal("the API stopped answering after a huge page")
	}
	for _, path := range []string{"/public/v1/rankings?by=gold", "/public/v1/rankings?race=martian", "/public/v1/rankings?page=0"} {
		if code := get(t, s, path, nil); code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", path, code)
		}
	}
}

/*
================
join
================
*/
func join(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ","
		}
		out += n
	}
	return out
}

/*
================
TestGearPointsRankDegreeSealAndPlus

The owner's formula (BUG-082): 10 x degree + seal bonus (Star 10, Moon 15,
Sun 20) + plus per worn piece, summed; ties go to the strongest piece.
================
*/
func TestGearPointsRankDegreeSealAndPlus(t *testing.T) {
	// The three worked examples.
	// Tiers in ItemGrade's order (enterworld.EquipmentSeal): 0 Star, 1 Moon, 2 Sun.
	const star, moon, sun = 0, 1, 2
	if got := gearPoints(10, sun, true, 3); got != 123 {
		t.Errorf("10th-degree +3 Sun = %d, want 123", got)
	}
	if got := gearPoints(11, 0, false, 0); got != 110 {
		t.Errorf("11th-degree +0 = %d, want 110", got)
	}
	if got := gearPoints(8, star, true, 12); got != 102 {
		t.Errorf("8th-degree +12 Star = %d, want 102", got)
	}
	if got := gearPoints(8, moon, true, 0); got != 95 {
		t.Errorf("8th-degree Moon = %d, want 95", got)
	}

	// Grades by reference: 1 low +7 normal, 2 a 10th-degree Sun, 3 an
	// 11th-degree normal piece.
	grades := map[uint32][3]int{1: {1, 0, 0}, 2: {10, sun, 1}, 3: {11, 0, 0}}
	f := newFixture()
	lvl := func(n int64) *int64 { return &n }
	worn := func(ref uint32, plus int64) domain.InventoryRow {
		return domain.InventoryRow{Slot: 0, RefObjID: ref, Plus: plus}
	}
	f.chars = []*domain.Character{
		{ID: 10, Name: "PlusFarmer", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(20),
			MissionInventory: []domain.InventoryRow{worn(1, 7)}},
		{ID: 11, Name: "SunHolder", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(90),
			MissionInventory: []domain.InventoryRow{worn(2, 3)}},
		{ID: 12, Name: "Plain", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(95),
			MissionInventory: []domain.InventoryRow{worn(3, 0)}},
	}
	s := f.service()
	s.src.ItemGrade = func(ref uint32) (int, int, bool) {
		g := grades[ref]
		return g[0], g[1], g[2] == 1
	}
	out := rankingNames(t, s, "/public/v1/rankings?by=gear")
	names := []string{}
	for _, row := range out.Rows {
		names = append(names, row.Name)
	}
	if want := "SunHolder,Plain,PlusFarmer"; join(names) != want {
		t.Fatalf("by gear %s, want %s (a high plus on low gear no longer leads)", join(names), want)
	}
	if out.Rows[0].Gear.Points != 123 || out.Rows[0].Gear.PlusTotal != 3 {
		t.Fatalf("SunHolder gear %+v, want 123 points and plus 3", out.Rows[0].Gear)
	}
}
