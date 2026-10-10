/*
===========================================================================

publicstats_test.go - tests for the public read API

===========================================================================
*/
package publicstats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	tigerRef  = 1954
	uruchiRef = 1982
)

/*
================
fixture

Three characters (one hides), three kills and two uniques on a fixed clock.
================
*/
type fixture struct {
	chars []*domain.Character
	kills []domain.UniqueKill
	now   time.Time
}

func newFixture() *fixture {
	lvl := func(n int64) *int64 { return &n }
	str := int64(120)
	return &fixture{
		chars: []*domain.Character{
			{ID: 1, Name: "Kekw", ModelCodename: "CHAR_CH_MAN_ADVENTURER", Level: lvl(54), Strength: &str,
				LevelReachedAt: map[uint8]int64{20: 1000, 30: 5000}, LastLoginAtMs: 9000,
				Masteries:        []domain.CharacterMastery{{ID: 257, Level: 54}},
				MissionInventory: []domain.InventoryRow{{Slot: 6, RefObjID: 3801, Plus: 7}, {Slot: 20, RefObjID: 99}}},
			{ID: 2, Name: "Lune", ModelCodename: "CHAR_EU_WOMAN_WARRIOR", Level: lvl(40), LevelReachedAt: map[uint8]int64{20: 800}},
			{ID: 3, Name: "Shy", ModelCodename: "CHAR_CH_WOMAN_ADVENTURER", Level: lvl(60), PublicHidden: true,
				LevelReachedAt: map[uint8]int64{20: 500, 30: 600}},
		},
		kills: []domain.UniqueKill{
			{Seq: 1, AtMs: 1000, RefObjID: tigerRef, KillerCharID: 3, KillerName: "Shy"},
			{Seq: 2, AtMs: 2000, RefObjID: tigerRef, KillerCharID: 1, KillerName: "Kekw"},
			{Seq: 3, AtMs: 3000, RefObjID: uruchiRef, KillerCharID: 1, KillerName: "Kekw"},
		},
		now: time.UnixMilli(100_000_000),
	}
}

func (f *fixture) service() *Service {
	return New(Sources{
		Characters: func() []*domain.Character { return f.chars },
		Kills: func(sinceMs int64) ([]domain.UniqueKill, error) {
			var out []domain.UniqueKill
			for _, k := range f.kills {
				if k.AtMs >= sinceMs {
					out = append(out, k)
				}
			}
			return out, nil
		},
		Uniques: func() []simulation.UniqueState {
			return []simulation.UniqueState{
				{RefObjID: tigerRef, Codename: "MOB_CH_TIGERWOMAN", Level: 20, RegionID: 23687, Alive: true, SpawnedAtMs: 4000, RespawnMinSec: 3600, RespawnMaxSec: 7200},
				{RefObjID: uruchiRef, Codename: "MOB_OA_URUCHI", Level: 40, RegionID: 23688, DiedAtMs: 3500, RespawnMinSec: 60, RespawnMaxSec: 120},
			}
		},
		UniqueName: func(ref uint32) string {
			if ref == tigerRef {
				return "Tiger Girl"
			}
			return ""
		},
		GuildOf: func(c *domain.Character) (string, string) {
			if c.ID == 1 {
				return "Silk", "member"
			}
			return "", ""
		},
		Online: func(characterID int64) bool { return characterID == 1 },
		Shard:  "beta",
		Now:    func() time.Time { return f.now },
	})
}

/*
================
get

One request through the handler; the body decodes into out.
================
*/
func get(t *testing.T, s *Service, path string, out any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v (%s)", path, err, rec.Body.String())
		}
	}
	if !strings.HasPrefix(rec.Header().Get("Cache-Control"), "max-age=") {
		t.Fatalf("%s carries no max-age", path)
	}
	return rec.Code
}

/*
================
TestUniquesShowStateWindowAndPrivateKiller

P1: a live unique has no window; a dead one returns between death + min and
death + max; the last kill names a visible killer and its guild.
================
*/
func TestUniquesShowStateWindowAndPrivateKiller(t *testing.T) {
	var out UniquesResponse
	if code := get(t, newFixture().service(), "/public/v1/uniques", &out); code != http.StatusOK || len(out.Uniques) != 2 {
		t.Fatalf("uniques %d %+v", code, out)
	}
	tiger, uruchi := out.Uniques[0], out.Uniques[1]
	if tiger.Name != "Tiger Girl" || !tiger.Alive || tiger.NextWin != nil || tiger.LastKill == nil ||
		tiger.LastKill.Killer != "Kekw" || tiger.LastKill.Guild != "Silk" {
		t.Fatalf("tiger %+v %+v", tiger, tiger.LastKill)
	}
	if uruchi.Name != "MOB_OA_URUCHI" || uruchi.Alive || uruchi.NextWin == nil ||
		uruchi.NextWin.OpensAt != rfc3339(3500+60_000) || uruchi.NextWin.ClosesAt != rfc3339(3500+120_000) {
		t.Fatalf("uruchi %+v %+v", uruchi, uruchi.NextWin)
	}
}

/*
================
TestHiddenCharacterVanishesEverywhereIncludingPastKills

A character that opts out after killing is "a hunter" in the feed and the
firsts, absent from the leaderboard, search and profile; others are
untouched.
================
*/
func TestHiddenCharacterVanishesEverywhereIncludingPastKills(t *testing.T) {
	f := newFixture()
	s := f.service()
	var kills KillsResponse
	get(t, s, "/public/v1/uniques/kills?limit=10", &kills)
	if len(kills.Kills) != 3 || kills.Kills[0].Killer != "Kekw" || kills.Kills[2].Killer != HiddenName || kills.Kills[2].Guild != "" {
		t.Fatalf("feed %+v", kills.Kills)
	}
	var firsts FirstsResponse
	get(t, s, "/public/v1/firsts", &firsts)
	if len(firsts.Levels) != 2 || firsts.Levels[0].Name != HiddenName || firsts.Uniques[0].Killer != HiddenName || firsts.Shard != "beta" {
		t.Fatalf("firsts %+v", firsts)
	}
	var board LeaderboardResponse
	get(t, s, "/public/v1/leaderboards/uniques", &board)
	if len(board.Rows) != 1 || board.Rows[0].Name != "Kekw" || board.Rows[0].Kills != 2 || board.Rows[0].ByUnique["1954"] != 1 {
		t.Fatalf("board %+v", board.Rows)
	}
	var search CharactersResponse
	get(t, s, "/public/v1/characters?q=s", &search)
	if len(search.Characters) != 0 {
		t.Fatalf("hidden character found by search: %+v", search.Characters)
	}
	if code := get(t, s, "/public/v1/characters/Shy", nil); code != http.StatusNotFound {
		t.Fatalf("hidden profile answered %d", code)
	}

	// Kekw opts out after the kills: every past kill and the board follow.
	f2 := newFixture()
	f2.chars[0].PublicHidden = true
	s2 := f2.service()
	get(t, s2, "/public/v1/uniques/kills", &kills)
	for _, k := range kills.Kills {
		if k.Killer != HiddenName {
			t.Fatalf("a past kill still names its hidden killer: %+v", k)
		}
	}
	get(t, s2, "/public/v1/leaderboards/uniques", &board)
	if len(board.Rows) != 0 {
		t.Fatalf("the board still lists a hidden hunter: %+v", board.Rows)
	}
}

/*
================
TestProfileCarriesLookStatsAndCounts

P6 for a visible character: race and gender from the model, guild, job,
masteries, worn sockets only (no bag rows), kill count and presence.
================
*/
func TestProfileCarriesLookStatsAndCounts(t *testing.T) {
	var p Profile
	if code := get(t, newFixture().service(), "/public/v1/characters/kekw", &p); code != http.StatusOK {
		t.Fatalf("profile %d", code)
	}
	if p.Name != "Kekw" || p.Race != "chinese" || p.Gender != "male" || p.Level != 54 || p.Guild == nil || p.Guild.Name != "Silk" ||
		p.Stats.Str != 120 || len(p.Masteries) != 1 || p.UniqueKills != 2 || !p.Online || p.LastLogin == nil {
		t.Fatalf("profile %+v", p)
	}
	if len(p.Look.Worn) != 1 || p.Look.Worn[0].RefItemID != 3801 || p.Look.Worn[0].Plus != 7 || p.Look.BodyCodename != "CHAR_CH_MAN_ADVENTURER" {
		t.Fatalf("look %+v", p.Look)
	}
	if code := get(t, newFixture().service(), "/public/v1/characters/nobody", nil); code != http.StatusNotFound {
		t.Fatalf("unknown profile %d", code)
	}
}

/*
================
TestBadQueriesAreRefused
================
*/
func TestBadQueriesAreRefused(t *testing.T) {
	s := newFixture().service()
	for _, path := range []string{
		"/public/v1/uniques/kills?limit=0", "/public/v1/uniques/kills?limit=500",
		"/public/v1/leaderboards/uniques?period=month", "/public/v1/characters?q=",
	} {
		if code := get(t, s, path, nil); code != http.StatusBadRequest {
			t.Fatalf("%s answered %d", path, code)
		}
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/public/v1/uniques", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST answered %d", rec.Code)
	}
}

/*
================
TestAnswersAreCachedNotRebuiltPerRequest

Within an endpoint's period a second request is served from the cache; the
sources are not read again until it expires.
================
*/
func TestAnswersAreCachedNotRebuiltPerRequest(t *testing.T) {
	f := newFixture()
	reads := 0
	s := f.service()
	inner := s.src.Characters
	s.src.Characters = func() []*domain.Character { reads++; return inner() }
	get(t, s, "/public/v1/characters?q=k", nil)
	get(t, s, "/public/v1/characters?q=k", nil)
	if reads != 1 {
		t.Fatalf("sources read %d times within the cache period", reads)
	}
	f.now = f.now.Add(charactersMaxAge)
	get(t, s, "/public/v1/characters?q=k", nil)
	if reads != 2 {
		t.Fatalf("sources read %d times after expiry", reads)
	}
}

/*
================
TestSchemaDescribesEveryEncodedAnswer

The published schema names every endpoint, and each answer the handlers
encode satisfies its schema: required fields present, no extra fields,
types matching. The schema and the encoder share the response structs, so
this pins the generator, not a hand-copied list.
================
*/
func TestSchemaDescribesEveryEncodedAnswer(t *testing.T) {
	s := newFixture().service()
	var doc map[string]any
	get(t, s, "/public/v1/schema", &doc)
	endpoints, _ := doc["endpoints"].(map[string]any)
	samples := map[string]string{
		"/public/v1/uniques":              "/public/v1/uniques",
		"/public/v1/uniques/kills":        "/public/v1/uniques/kills",
		"/public/v1/leaderboards/uniques": "/public/v1/leaderboards/uniques",
		"/public/v1/firsts":               "/public/v1/firsts",
		"/public/v1/characters":           "/public/v1/characters?q=k",
		"/public/v1/characters/{name}":    "/public/v1/characters/Kekw",
		"/public/v1/rules":                "/public/v1/rules",
	}
	if len(endpoints) != len(samples) {
		t.Fatalf("schema lists %d endpoints, want %d", len(endpoints), len(samples))
	}
	for endpoint, path := range samples {
		schema, ok := endpoints[endpoint].(map[string]any)
		if !ok {
			t.Fatalf("schema has no %s", endpoint)
		}
		var answer any
		if code := get(t, s, path, &answer); code != http.StatusOK {
			t.Fatalf("%s answered %d", path, code)
		}
		if err := validate(schema, answer, endpoint); err != "" {
			t.Fatalf("%s does not satisfy its schema: %s", path, err)
		}
	}
}

/*
================
validate

A minimal JSON Schema check for the subset typeSchema emits.
================
*/
func validate(schema map[string]any, value any, at string) string {
	if options, ok := schema["anyOf"].([]any); ok {
		for _, option := range options {
			if validate(option.(map[string]any), value, at) == "" {
				return ""
			}
		}
		return at + ": no anyOf branch matches"
	}
	switch schema["type"] {
	case "null":
		if value != nil {
			return at + ": not null"
		}
	case "string":
		if _, ok := value.(string); !ok {
			return at + ": not a string"
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return at + ": not a boolean"
		}
	case "integer", "number":
		n, ok := value.(float64)
		if !ok {
			return at + ": not a number"
		}
		if schema["type"] == "integer" && n != float64(int64(n)) {
			return at + ": not an integer"
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return at + ": not an array"
		}
		for _, item := range items {
			if err := validate(schema["items"].(map[string]any), item, at+"[]"); err != "" {
				return err
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return at + ": not an object"
		}
		if extra, ok := schema["additionalProperties"].(map[string]any); ok {
			for key, item := range object {
				if err := validate(extra, item, at+"."+key); err != "" {
					return err
				}
			}
			return ""
		}
		properties, _ := schema["properties"].(map[string]any)
		for _, name := range schema["required"].([]any) {
			if _, ok := object[name.(string)]; !ok {
				return at + ": missing " + name.(string)
			}
		}
		for key, item := range object {
			property, ok := properties[key].(map[string]any)
			if !ok {
				return at + ": unexpected field " + key
			}
			if err := validate(property, item, at+"."+key); err != "" {
				return err
			}
		}
	}
	return ""
}

/*
================
TestSchemaCoversEveryResponseField

Every exported field of every response type appears in the schema.
================
*/
func TestSchemaCoversEveryResponseField(t *testing.T) {
	for _, endpoint := range endpointTypes {
		schema := typeSchema(endpoint.typ)
		properties := schema["properties"].(map[string]any)
		for i := 0; i < endpoint.typ.NumField(); i++ {
			name, _, _ := strings.Cut(endpoint.typ.Field(i).Tag.Get("json"), ",")
			if _, ok := properties[name]; !ok {
				t.Fatalf("%s: %s missing", endpoint.path, name)
			}
		}
		if !reflect.DeepEqual(schema, typeSchema(endpoint.typ)) {
			t.Fatal("schema generation is not deterministic")
		}
	}
}
