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
	"opensro.online/server/internal/game/social/chatfeed"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/sealboard"
	"opensro.online/server/internal/transport"
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
				MissionInventory: []domain.InventoryRow{{Slot: 6, RefObjID: 3801, Plus: 7, VarianceBits: "1094795585", Durability: 48, MagicOptions: []uint64{7<<32 | 33, 20<<32 | 41}}, {Slot: 7, RefObjID: 3802}, {Slot: 20, RefObjID: 99}}},
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
		Kills: func(afterSeq int64) ([]domain.UniqueKill, error) {
			var out []domain.UniqueKill
			for _, k := range f.kills {
				if k.Seq > afterSeq {
					out = append(out, k)
				}
			}
			return out, nil
		},
		Uniques: func() []simulation.UniqueState {
			return []simulation.UniqueState{
				{RefObjID: tigerRef, Codename: "MOB_CH_TIGERWOMAN", Level: 20, RegionID: 23687, Alive: true, SpawnedAtMs: 4000},
				{RefObjID: uruchiRef, Codename: "MOB_OA_URUCHI", Level: 40, RegionID: 23688},
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
		SealCounts: func(weekStart time.Time) ([]sealboard.Count, error) {
			return []sealboard.Count{{CharacterID: 1, Tier: sealboard.TierSun, Drops: 2}}, nil
		},
		SealCountingSince: func(weekStart time.Time) (time.Time, error) { return weekStart, nil },
		Shard:             "beta",
		Now:               func() time.Time { return f.now },
		Sample:            func() (int, transport.Metrics) { return 3, transport.Metrics{} },
		StartedAt:         f.now.Add(-time.Hour),
		Schedule: func(now time.Time) []ScheduledEvent {
			return []ScheduledEvent{{Kind: "fortress", Title: "Fortress war", Start: now, End: now.Add(time.Hour), Running: true},
				{Kind: "maintenance", Title: "Maintenance", Start: now, Running: true}}
		},
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
TestUniquesShowStateAndPrivateKiller

P1: a unique says whether it is up and since when; the last kill names a
visible killer and its guild.
================
*/
func TestUniquesShowStateAndPrivateKiller(t *testing.T) {
	var out UniquesResponse
	if code := get(t, newFixture().service(), "/public/v1/uniques", &out); code != http.StatusOK || len(out.Uniques) != 2 {
		t.Fatalf("uniques %d %+v", code, out)
	}
	tiger, uruchi := out.Uniques[0], out.Uniques[1]
	if tiger.Name != "Tiger Girl" || !tiger.Alive || tiger.SpawnedAt == nil || tiger.LastKill == nil ||
		tiger.LastKill.Killer != "Kekw" || tiger.LastKill.Guild != "Silk" {
		t.Fatalf("tiger %+v %+v", tiger, tiger.LastKill)
	}
	if uruchi.Name != "MOB_OA_URUCHI" || uruchi.Alive || uruchi.SpawnedAt != nil {
		t.Fatalf("uruchi %+v", uruchi)
	}
}

/*
================
TestUniquesKeepTheRespawnSecret

When a unique returns is a secret: neither a live nor a dead unique's row,
nor the published schema, carries a respawn delay or a return window.
================
*/
func TestUniquesKeepTheRespawnSecret(t *testing.T) {
	s := newFixture().service()
	var raw struct {
		Uniques []map[string]json.RawMessage `json:"uniques"`
	}
	if code := get(t, s, "/public/v1/uniques", &raw); code != http.StatusOK || len(raw.Uniques) != 2 {
		t.Fatalf("uniques %d %+v", code, raw)
	}
	want := []string{"alive", "lastKill", "level", "name", "refObjId", "regionId", "spawnedAt"}
	for _, row := range raw.Uniques {
		keys := make([]string, 0, len(row))
		for key := range row {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if !reflect.DeepEqual(keys, want) {
			t.Fatalf("unique row keys %v, want %v", keys, want)
		}
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public/v1/schema", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"spawnedAt"`) {
		t.Fatalf("schema %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"respawn", "nextWindow", "opensAt", "closesAt"} {
		if strings.Contains(rec.Body.String(), `"`+secret+`"`) {
			t.Fatalf("schema publishes %q", secret)
		}
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
	if len(p.Look.Worn) != 2 || p.Look.Worn[0].RefItemID != 3801 || p.Look.Worn[0].Plus != 7 || p.Look.BodyCodename != "CHAR_CH_MAN_ADVENTURER" {
		t.Fatalf("look %+v", p.Look)
	}
	// The tooltip's own inputs: the white-stat roll, durability and blue stats.
	if w := p.Look.Worn[0]; w.Variance != "1094795585" || w.Durability == nil || *w.Durability != 48 ||
		len(w.MagicOptions) != 2 || w.MagicOptions[0] != "30064771105" || w.MagicOptions[1] != "85899345961" {
		t.Fatalf("worn item %+v", w)
	}
	// A broken item (durability 0) still says so: absent would read as full.
	if w := p.Look.Worn[1]; w.Durability == nil || *w.Durability != 0 {
		t.Fatalf("broken item %+v", w)
	}
	if code := get(t, newFixture().service(), "/public/v1/characters/nobody", nil); code != http.StatusNotFound {
		t.Fatalf("unknown profile %d", code)
	}
}

/*
================
TestAmmunitionIsNeverBroken

Worn arrows read durability 0 (their reference has none); the profile
leaves durability out for them, so nothing shows them as broken, while a
weapon at 0 still says so.
================
*/
func TestAmmunitionIsNeverBroken(t *testing.T) {
	const arrowRef = 62
	f := newFixture()
	f.chars[0].MissionInventory = []domain.InventoryRow{{Slot: 6, RefObjID: 3801}, {Slot: 7, RefObjID: arrowRef}}
	service := f.service()
	service.src.HasDurability = func(refObjID uint32) bool { return refObjID != arrowRef }
	var p Profile
	if code := get(t, service, "/public/v1/characters/kekw", &p); code != http.StatusOK {
		t.Fatalf("profile %d", code)
	}
	if len(p.Look.Worn) != 2 {
		t.Fatalf("look %+v", p.Look)
	}
	if w := p.Look.Worn[0]; w.Durability == nil || *w.Durability != 0 {
		t.Fatalf("a weapon at 0 is broken and says so: %+v", w)
	}
	if w := p.Look.Worn[1]; w.RefItemID != arrowRef || w.Durability != nil {
		t.Fatalf("arrows carry no durability: %+v", w)
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
	s := privacyService(newFixture(), testWriteToken)
	// A named line and a notice, so both forms of character meet the schema.
	s.src.Chat = func() []chatfeed.Line {
		return []chatfeed.Line{{At: time.UnixMilli(1000), Kind: chatfeed.KindChat, Character: "Kekw", Author: "Kekw", Text: "hi"},
			{At: time.UnixMilli(2000), Kind: chatfeed.KindNotice, Text: "Maintenance"}}
	}
	s.src.Events = func() []chatfeed.Line {
		return []chatfeed.Line{{At: time.UnixMilli(3000), Kind: chatfeed.KindEvent, Text: "Kekw got a Seal of Star"}}
	}
	// A guild with a union partner and a fortress, so every optional field meets the schema.
	s.src.Guilds = func() []GuildEntry {
		return []GuildEntry{{ID: 1, Name: "Silk", Level: 2, Members: []GuildEntryMember{{CharID: 1}}, Union: []int64{2}, Fortress: "jangan",
			FoundedAtMs: 1760000000000},
			{ID: 2, Name: "Tea", Level: 1, Members: []GuildEntryMember{{CharID: 2}}, Union: []int64{1}}}
	}
	var doc map[string]any
	get(t, s, "/public/v1/schema", &doc)
	endpoints, _ := doc["endpoints"].(map[string]any)
	samples := map[string]string{
		"/public/v1/uniques":              "/public/v1/uniques",
		"/public/v1/uniques/kills":        "/public/v1/uniques/kills",
		"/public/v1/leaderboards/uniques": "/public/v1/leaderboards/uniques",
		"/public/v1/leaderboards/seals":   "/public/v1/leaderboards/seals",
		"/public/v1/firsts":               "/public/v1/firsts",
		"/public/v1/characters":           "/public/v1/characters?q=k",
		"/public/v1/characters/{name}":    "/public/v1/characters/Kekw",
		"/public/v1/rules":                "/public/v1/rules",
		"/v1/accounts/{id}/characters":    "/v1/accounts/alpha/characters",
		"/public/v1/status":               "/public/v1/status",
		"/public/v1/schedule":             "/public/v1/schedule",
		"/public/v1/rankings":             "/public/v1/rankings?by=gear",
		"/public/v1/chat":                 "/public/v1/chat",
		"/public/v1/events":               "/public/v1/events",
		"/public/v1/guilds":               "/public/v1/guilds?q=s",
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
		if strings.HasPrefix(path, "/v1/") {
			code, body := authorizedGet(s, path, testWriteToken)
			if code != http.StatusOK {
				t.Fatalf("%s answered %d", path, code)
			}
			if err := json.Unmarshal(body, &answer); err != nil {
				t.Fatal(err)
			}
		} else if code := get(t, s, path, &answer); code != http.StatusOK {
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

/*
================
TestDistinctRequestsShareOneSourceCopy

A burst of different names and searches (each its own cache key) copies the
characters and reads the kills once per snapshot period, not once per key;
a privacy write drops the copy so the next read sees the change.
================
*/
func TestDistinctRequestsShareOneSourceCopy(t *testing.T) {
	f := newFixture()
	s := privacyService(f, testWriteToken)
	characterReads, killReads := 0, 0
	characters, kills := s.src.Characters, s.src.Kills
	s.src.Characters = func() []*domain.Character { characterReads++; return characters() }
	s.src.Kills = func(afterSeq int64) ([]domain.UniqueKill, error) { killReads++; return kills(afterSeq) }
	for _, path := range []string{
		"/public/v1/characters/Kekw", "/public/v1/characters/Lune", "/public/v1/characters/Nobody1",
		"/public/v1/characters/Nobody2", "/public/v1/characters?q=k", "/public/v1/characters?q=lu",
		"/public/v1/uniques", "/public/v1/leaderboards/uniques?period=week", "/public/v1/firsts",
	} {
		get(t, s, path, nil)
	}
	if characterReads != 1 || killReads != 1 {
		t.Fatalf("burst copied characters %d and read kills %d times, want 1 and 1", characterReads, killReads)
	}
	f.now = f.now.Add(snapshotMaxAge)
	get(t, s, "/public/v1/characters/Ghost", nil)
	if characterReads != 2 {
		t.Fatalf("characters copied %d times after the period, want 2", characterReads)
	}
	put(s, "/v1/accounts/alpha/characters/Kekw/hidden", testWriteToken, `{"hidden":true}`)
	get(t, s, "/public/v1/characters/Ghost2", nil)
	if characterReads != 3 {
		t.Fatalf("characters copied %d times after a privacy write, want 3", characterReads)
	}
}

/*
================
TestKillListCatchesUpWithoutLosingOldKills

The shared list pages through every kill on the first read, then asks only
for sequences after the last one held: all-time answers keep the oldest
kills however many follow, and a new kill appears after the period.
================
*/
func TestKillListCatchesUpWithoutLosingOldKills(t *testing.T) {
	f := newFixture()
	s := f.service()
	var asked []int64
	kills := s.src.Kills
	s.src.KillPage = 1
	s.src.Kills = func(afterSeq int64) ([]domain.UniqueKill, error) {
		asked = append(asked, afterSeq)
		page, err := kills(afterSeq)
		if len(page) > s.src.KillPage {
			page = page[:s.src.KillPage]
		}
		return page, err
	}
	var firsts FirstsResponse
	get(t, s, "/public/v1/firsts", &firsts)
	if len(asked) != 4 || asked[0] != 0 || asked[3] != 3 {
		t.Fatalf("first read asked after %v, want 0,1,2,3", asked)
	}
	f.kills = append(f.kills, domain.UniqueKill{Seq: 4, AtMs: 4000, RefObjID: uruchiRef, KillerCharID: 2, KillerName: "Lune"})
	f.now = f.now.Add(snapshotMaxAge)
	asked = nil
	var feed KillsResponse
	get(t, s, "/public/v1/uniques/kills?limit=100", &feed)
	if len(asked) != 2 || asked[0] != 3 {
		t.Fatalf("catch-up asked after %v, want 3 then 4", asked)
	}
	if len(feed.Kills) != 4 {
		t.Fatalf("feed holds %d kills, want all 4", len(feed.Kills))
	}
}
