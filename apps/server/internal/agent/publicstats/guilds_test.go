/*
===========================================================================

guilds_test.go - the public guild list (guilds.go)

===========================================================================
*/

package publicstats

import (
	"net/http"
	"testing"
)

/*
================
TestGuildListRanksAndKeepsMastersPrivate

Guilds rank by level, then members, then name. A hidden master is "a
hunter"; union partners are named (an unknown guild id is dropped); the
held fortress shows; a name search keeps each guild's place on the full
list; past the last page is empty; a bad page is refused.
================
*/
func TestGuildListRanksAndKeepsMastersPrivate(t *testing.T) {
	f := newFixture()
	s := f.service()
	s.src.Guilds = func() []GuildEntry {
		return []GuildEntry{
			{ID: 10, Name: "Silk", Level: 3, Members: []GuildEntryMember{{CharID: 1, Grade: 0}, {CharID: 2, Grade: 2}},
				Union: []int64{11, 99}},
			{ID: 11, Name: "Tea", Level: 3,
				Members: []GuildEntryMember{{CharID: 3, Grade: 0}, {CharID: 7, Grade: 2}, {CharID: 8, Grade: 2}},
				Union:   []int64{10}},
			{ID: 12, Name: "Ash", Level: 5, Members: []GuildEntryMember{{CharID: 9, Grade: 0}}, Fortress: "jangan",
				FoundedAtMs: 1760000000000},
		}
	}
	var out GuildsResponse
	if code := get(t, s, "/public/v1/guilds", &out); code != http.StatusOK {
		t.Fatalf("guilds answered %d", code)
	}
	if out.Total != 3 || out.Pages != 1 || len(out.Rows) != 3 {
		t.Fatalf("guild page %+v", out)
	}
	ash, tea, silk := out.Rows[0], out.Rows[1], out.Rows[2]
	if ash.Name != "Ash" || tea.Name != "Tea" || silk.Name != "Silk" || ash.Rank != 1 || silk.Rank != 3 {
		t.Fatalf("order %s %s %s", ash.Name, tea.Name, silk.Name)
	}
	if ash.Fortress == nil || *ash.Fortress != "jangan" || ash.Master != "" || len(ash.Union) != 0 {
		t.Fatalf("Ash %+v", ash)
	}
	// A guild founded before the store kept the time has none.
	if ash.Founded == nil || *ash.Founded != "2025-10-09T08:53:20Z" || tea.Founded != nil {
		t.Fatalf("founded %v / %v", ash.Founded, tea.Founded)
	}
	if tea.Master != HiddenName || tea.Members != 3 || len(tea.Union) != 1 || tea.Union[0] != "Silk" {
		t.Fatalf("Tea %+v", tea)
	}
	if silk.Master != "Kekw" || len(silk.Union) != 1 || silk.Union[0] != "Tea" || silk.Fortress != nil {
		t.Fatalf("Silk %+v", silk)
	}
	var found GuildsResponse
	get(t, s, "/public/v1/guilds?q=SI", &found)
	if found.Total != 1 || found.Rows[0].Name != "Silk" || found.Rows[0].Rank != 3 || found.Query == nil || *found.Query != "si" {
		t.Fatalf("search %+v", found)
	}
	var past GuildsResponse
	get(t, s, "/public/v1/guilds?page=2", &past)
	if len(past.Rows) != 0 || past.Pages != 1 {
		t.Fatalf("past the last page %+v", past)
	}
	if code := get(t, s, "/public/v1/guilds?page=0", &past); code != http.StatusBadRequest {
		t.Fatalf("page 0 answered %d", code)
	}
}

/*
================
TestGuildBurstCopiesGuildsOnce

Every distinct search and page misses the answer cache, but the guild
list behind them is read once per snapshotMaxAge: a public endpoint must
not walk the shard per request.
================
*/
func TestGuildBurstCopiesGuildsOnce(t *testing.T) {
	f := newFixture()
	s := f.service()
	reads := 0
	s.src.Guilds = func() []GuildEntry {
		reads++
		return []GuildEntry{{ID: 1, Name: "Silk", Level: 1, Members: []GuildEntryMember{{CharID: 1}}}}
	}
	for _, path := range []string{
		"/public/v1/guilds", "/public/v1/guilds?q=a", "/public/v1/guilds?q=b", "/public/v1/guilds?q=si",
		"/public/v1/guilds?page=2", "/public/v1/guilds?q=c&page=3",
	} {
		get(t, s, path, nil)
	}
	if reads != 1 {
		t.Fatalf("burst read the guilds %d times, want 1", reads)
	}
	f.now = f.now.Add(snapshotMaxAge)
	get(t, s, "/public/v1/guilds?q=d", nil)
	if reads != 2 {
		t.Fatalf("after the snapshot age the guilds were read %d times, want 2", reads)
	}
}
