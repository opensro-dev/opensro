/*
===========================================================================

seals_test.go - the weekly seal board: aliases, ordering, privacy, schema

===========================================================================
*/
package publicstats

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/sealboard"
)

// sealNow is a Wednesday; its week opens Monday 2026-10-26 00:00 UTC.
var sealNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

var sealWeekStart = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

/*
================
sealService

A board whose counting began Wednesday 09:30 (a mid-week enable) over a
fixed roster and fixed counts.
================
*/
func sealService(chars []*domain.Character, counts []sealboard.Count) *Service {
	return New(Sources{
		Characters: func() []*domain.Character { return chars },
		Now:        func() time.Time { return sealNow },
		SealCounts: func(weekStart time.Time) ([]sealboard.Count, error) {
			if !weekStart.Equal(sealWeekStart) {
				return nil, errBadRequest
			}
			return counts, nil
		},
		SealCountingSince: func(weekStart time.Time) (time.Time, error) {
			return time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC), nil
		},
	})
}

/*
================
sealFixture

Five counted characters: a Sun leader, a Moon tie broken by Star, an
alias tie, a Star-only row; the hidden, alias-less and unknown counts
must never appear.
================
*/
func sealFixture() ([]*domain.Character, []sealboard.Count) {
	char := func(id int64, name, alias string, hidden bool) *domain.Character {
		c := &domain.Character{ID: id, Name: name, PublicHidden: hidden}
		c.Job.Alias = alias
		return c
	}
	chars := []*domain.Character{
		char(1, "AlphaChar", "SunHunter", false),
		char(2, "BetaChar", "MoonRunner", false),
		char(3, "GhostChar", "GhostJob", true),
		char(4, "NoAliasChar", "", false),
		char(5, "StarChar", "StarGazer", false),
		char(6, "TieOneChar", "AlphaJob", false),
		char(7, "TieTwoChar", "BetaJob", false),
	}
	count := func(id int64, tier int, drops int64) sealboard.Count {
		return sealboard.Count{CharacterID: id, Tier: tier, Drops: drops}
	}
	counts := []sealboard.Count{
		count(1, sealboard.TierSun, 2), count(1, sealboard.TierMoon, 1), count(1, sealboard.TierStar, 5),
		count(2, sealboard.TierSun, 2), count(2, sealboard.TierMoon, 3), count(2, sealboard.TierStar, 1),
		count(3, sealboard.TierSun, 99),
		count(4, sealboard.TierSun, 50),
		count(5, sealboard.TierStar, 7),
		count(6, sealboard.TierSun, 2), count(6, sealboard.TierMoon, 3),
		count(7, sealboard.TierSun, 2), count(7, sealboard.TierMoon, 3),
		count(99, sealboard.TierSun, 42),
	}
	return chars, counts
}

/*
================
TestSealBoardRows
================
*/
func TestSealBoardRows(t *testing.T) {
	chars, counts := sealFixture()
	var out SealBoardResponse
	if code := get(t, sealService(chars, counts), "/public/v1/leaderboards/seals", &out); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if out.WeekStart != "2026-10-26T00:00:00Z" || out.WeekEnd != "2026-11-02T00:00:00Z" {
		t.Fatalf("week %s to %s", out.WeekStart, out.WeekEnd)
	}
	if out.CountingSince != "2026-10-28T09:30:00Z" {
		t.Fatalf("counting since %s, want the mid-week enable", out.CountingSince)
	}
	want := []SealBoardRow{
		{Rank: 1, Alias: "MoonRunner", Sun: 2, Moon: 3, Star: 1},
		{Rank: 2, Alias: "AlphaJob", Sun: 2, Moon: 3, Star: 0},
		{Rank: 3, Alias: "BetaJob", Sun: 2, Moon: 3, Star: 0},
		{Rank: 4, Alias: "SunHunter", Sun: 2, Moon: 1, Star: 5},
		{Rank: 5, Alias: "StarGazer", Sun: 0, Moon: 0, Star: 7},
	}
	if len(out.Rows) != len(want) {
		t.Fatalf("rows %+v", out.Rows)
	}
	for i := range want {
		if out.Rows[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, out.Rows[i], want[i])
		}
	}
}

/*
================
TestSealBoardLabelsTheCurrentAlias

The counts are keyed by character; the row wears the alias the character
has now, so an alias change moves the counts instead of splitting them.
================
*/
func TestSealBoardLabelsTheCurrentAlias(t *testing.T) {
	chars, counts := sealFixture()
	chars[0].Job.Alias = "RenamedJob"
	var out SealBoardResponse
	if code := get(t, sealService(chars, counts), "/public/v1/leaderboards/seals", &out); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, row := range out.Rows {
		if row.Alias == "SunHunter" {
			t.Fatalf("the old alias survived: %+v", out.Rows)
		}
		if row.Alias == "RenamedJob" && (row.Sun != 2 || row.Moon != 1 || row.Star != 5) {
			t.Fatalf("the counts did not follow the alias: %+v", row)
		}
	}
}

/*
================
TestSealBoardUnservedWhenOff

No counter wired (SRO_SEAL_BOARD off): the route answers 404.
================
*/
func TestSealBoardUnservedWhenOff(t *testing.T) {
	chars, _ := sealFixture()
	service := New(Sources{
		Characters: func() []*domain.Character { return chars },
		Now:        func() time.Time { return sealNow },
	})
	// Off must be indistinguishable from a route that does not exist:
	// same status, content type, cache header and body as an unknown
	// path on the same handler (the census' no-fingerprint rule).
	probe := func(path string) (int, string, string, string) {
		rec := httptest.NewRecorder()
		service.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"), rec.Body.String()
	}
	code, ctype, cache, body := probe("/public/v1/leaderboards/seals")
	if code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 while the board is off", code)
	}
	ucode, uctype, ucache, ubody := probe("/public/v1/leaderboards/no-such-board")
	if code != ucode || ctype != uctype || cache != ucache || body != ubody {
		t.Fatalf("off answer (%d, %q, %q, %q) differs from an unknown route (%d, %q, %q, %q)",
			code, ctype, cache, body, ucode, uctype, ucache, ubody)
	}
}

/*
================
TestSealBoardReadsTheRealCounter

Slice A's store behind slice B's sources: drops noted on the counter
come back as the character's alias row, and the counting-since stamp is
the counter's own.
================
*/
func TestSealBoardReadsTheRealCounter(t *testing.T) {
	counter, err := sealboard.Open(t.TempDir(), func() time.Time { return sealNow })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = counter.Close() }()
	counter.NoteSealDrop(1, sealboard.TierSun, sealNow)
	counter.NoteSealDrop(1, sealboard.TierSun, sealNow)
	counter.NoteSealDrop(1, sealboard.TierStar, sealNow)
	chars, _ := sealFixture()
	service := New(Sources{
		Characters:        func() []*domain.Character { return chars },
		Now:               func() time.Time { return sealNow },
		SealCounts:        counter.Counts,
		SealCountingSince: counter.CountingSince,
	})
	var out SealBoardResponse
	if code := get(t, service, "/public/v1/leaderboards/seals", &out); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(out.Rows) != 1 || out.Rows[0] != (SealBoardRow{Rank: 1, Alias: "SunHunter", Sun: 2, Moon: 0, Star: 1}) {
		t.Fatalf("rows %+v", out.Rows)
	}
	if out.CountingSince != "2026-10-28T12:00:00Z" {
		t.Fatalf("counting since %s, want the counter's first open", out.CountingSince)
	}
}

/*
================
TestSealBoardSchemaNamesNoCharacter

The published schema for the board must not name a character anywhere:
no characterId, no name, at any depth.
================
*/
func TestSealBoardSchemaNamesNoCharacter(t *testing.T) {
	doc := SchemaDocument()
	endpoints, ok := doc["endpoints"].(map[string]any)
	if !ok {
		t.Fatalf("schema endpoints: %T", doc["endpoints"])
	}
	schema, ok := endpoints["/public/v1/leaderboards/seals"].(map[string]any)
	if !ok {
		t.Fatalf("the seals endpoint is not in the schema: %v", endpoints)
	}
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch node := v.(type) {
		case map[string]any:
			for key, child := range node {
				if key == "properties" {
					if props, ok := child.(map[string]any); ok {
						for name := range props {
							lower := strings.ToLower(name)
							if lower == "name" || strings.Contains(lower, "character") {
								t.Errorf("schema property %q at %s names a character", name, path)
							}
						}
					}
				}
				walk(child, path+"/"+key)
			}
		case []any:
			for _, child := range node {
				walk(child, path)
			}
		}
	}
	walk(schema, "/public/v1/leaderboards/seals")
	// The row shape is exactly rank, alias and the three seals.
	properties := schema["properties"].(map[string]any)
	rows := properties["rows"].(map[string]any)
	items := rows["items"].(map[string]any)
	rowProps := items["properties"].(map[string]any)
	for _, field := range []string{"rank", "alias", "sun", "moon", "star"} {
		if _, ok := rowProps[field]; !ok {
			t.Errorf("row schema lacks %q", field)
		}
	}
	if len(rowProps) != 5 {
		t.Errorf("row schema properties %v, want exactly rank/alias/sun/moon/star", rowProps)
	}
}
