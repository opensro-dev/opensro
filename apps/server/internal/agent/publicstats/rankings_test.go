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
	// Two level-30 players with equal progress: the earlier to reach 30 ranks first.
	f.chars = append(f.chars,
		&domain.Character{ID: 4, Name: "Late", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(30), Experience: exp(10),
			LevelReachedAt: map[uint8]int64{30: 9000}},
		&domain.Character{ID: 5, Name: "Early", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(30), Experience: exp(10),
			LevelReachedAt: map[uint8]int64{30: 1000}})
	s := f.service()
	s.src.ExpRequired = func(level int64) (int64, bool) { return 100, true }

	byLevel := rankingNames(t, s, "/public/v1/rankings")
	names := []string{}
	for _, row := range byLevel.Rows {
		names = append(names, row.Name)
	}
	if want := "Kekw,Lune,Early,Late"; join(names) != want {
		t.Fatalf("by level %s, want %s (Shy hidden)", join(names), want)
	}
	if byLevel.Total != 4 || byLevel.Pages != 1 || byLevel.Rows[0].Rank != 1 || byLevel.Rows[0].ExpPercent != 50 {
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
	if byGear.Rows[0].Name != "Kekw" || byGear.Rows[0].Gear != (Gear{PlusTotal: 7, BestPlus: 7}) {
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
