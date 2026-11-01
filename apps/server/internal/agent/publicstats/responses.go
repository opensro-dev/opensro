/*
===========================================================================

responses.go - the public read API's response shapes

These structs are the contract the community site reads (opensro-web
docs/COMMUNITY.md, section P). The handlers encode exactly these types and
GET /public/v1/schema publishes a JSON Schema generated from them
(schema.go), so the site validates against the shapes the server sends and
nothing is copied by hand. A breaking change bumps SchemaVersion.

Port-only, not native: the original has no public read API.

===========================================================================
*/
package publicstats

// SchemaVersion moves when a response shape changes incompatibly. 2: a
// unique no longer carries its respawn delay or return window.
const SchemaVersion = 2

// LastKill is a unique's most recent recorded kill.
type LastKill struct {
	At     string `json:"at"`
	Killer string `json:"killer"`
	Guild  string `json:"guild,omitempty"`
}

// Unique is one P1 row. When a unique returns is a secret (owner,
// 2026-10-11): the row says only whether it is up, since when, and who
// killed it last, never its nest's respawn delay.
type Unique struct {
	RefObjID  uint32    `json:"refObjId"`
	Name      string    `json:"name"`
	Level     uint8     `json:"level"`
	RegionID  uint16    `json:"regionId"`
	Alive     bool      `json:"alive"`
	SpawnedAt *string   `json:"spawnedAt"`
	LastKill  *LastKill `json:"lastKill"`
}

// UniquesResponse is P1 GET /public/v1/uniques.
type UniquesResponse struct {
	ServerTime string   `json:"serverTime"`
	Uniques    []Unique `json:"uniques"`
}

// Kill is one P2 row, newest first.
type Kill struct {
	At       string `json:"at"`
	RefObjID uint32 `json:"refObjId"`
	Name     string `json:"name"`
	Killer   string `json:"killer"`
	Guild    string `json:"guild,omitempty"`
}

// KillsResponse is P2 GET /public/v1/uniques/kills.
type KillsResponse struct {
	Kills []Kill `json:"kills"`
}

// LeaderboardRow is one P3 row.
type LeaderboardRow struct {
	Rank     int            `json:"rank"`
	Name     string         `json:"name"`
	Guild    string         `json:"guild,omitempty"`
	Level    int64          `json:"level"`
	Race     string         `json:"race"`
	Kills    int            `json:"kills"`
	ByUnique map[string]int `json:"byUnique"`
}

// LeaderboardResponse is P3 GET /public/v1/leaderboards/uniques.
type LeaderboardResponse struct {
	Period string           `json:"period"`
	Rows   []LeaderboardRow `json:"rows"`
}

// SealBoardRow is one weekly seal board row: a job alias and the week's
// Star, Moon and Sun drops counted for it. Aliases only (#1641): no
// character name and no character id ever reach this shape.
type SealBoardRow struct {
	Rank  int    `json:"rank"`
	Alias string `json:"alias"`
	Sun   int64  `json:"sun"`
	Moon  int64  `json:"moon"`
	Star  int64  `json:"star"`
}

// SealBoardResponse is GET /public/v1/leaderboards/seals: the week's
// seal drops by job alias, Sun first. The week opens Monday 00:00 UTC;
// CountingSince is when this week's counting began (a mid-week enable
// counts from then, never backfilled), so a partial week reads as one.
type SealBoardResponse struct {
	WeekStart     string         `json:"weekStart"`
	WeekEnd       string         `json:"weekEnd"`
	CountingSince string         `json:"countingSince"`
	GeneratedAt   string         `json:"generatedAt"`
	Rows          []SealBoardRow `json:"rows"`
}

// LevelFirst is one P4 level milestone.
type LevelFirst struct {
	Level uint8  `json:"level"`
	Name  string `json:"name"`
	At    string `json:"at"`
}

// UniqueFirst is one P4 first kill of a unique.
type UniqueFirst struct {
	RefObjID uint32 `json:"refObjId"`
	Name     string `json:"name"`
	Killer   string `json:"killer"`
	At       string `json:"at"`
}

// FirstsResponse is P4 GET /public/v1/firsts.
type FirstsResponse struct {
	Levels  []LevelFirst  `json:"levels"`
	Uniques []UniqueFirst `json:"uniques"`
	Shard   string        `json:"shard"`
}

// CharacterSummary is one P5 search row.
type CharacterSummary struct {
	Name  string `json:"name"`
	Level int64  `json:"level"`
	Race  string `json:"race"`
	Guild string `json:"guild,omitempty"`
}

// OwnCharacter is one of an account's characters with its privacy flag
// and its item-break notice opt-in (#1738).
type OwnCharacter struct {
	Name        string `json:"name"`
	Level       int64  `json:"level"`
	Race        string `json:"race"`
	Hidden      bool   `json:"hidden"`
	GearHidden  bool   `json:"gearHidden"`
	BreakNotice bool   `json:"breakNotice"`
}

// OwnCharactersResponse is the website's authenticated
// GET /v1/accounts/{id}/characters (privacy.go): every live character of
// the account, hidden ones included, so /account can show each switch.
type OwnCharactersResponse struct {
	Characters []OwnCharacter `json:"characters"`
}

// CharactersResponse is P5 GET /public/v1/characters.
type CharactersResponse struct {
	Characters []CharacterSummary `json:"characters"`
}

// Guild is a profile's guild and the character's place in it.
type Guild struct {
	Name string `json:"name"`
	Rank string `json:"rank"`
}

// Mastery is one learned mastery.
type Mastery struct {
	RefID uint32 `json:"refId"`
	Level int64  `json:"level"`
}

// Stats are a profile's base numbers.
type Stats struct {
	Str int64 `json:"str"`
	Int int64 `json:"int"`
	HP  int64 `json:"hp"`
	MP  int64 `json:"mp"`
}

// WornItem is one equipment or avatar socket, with what the in-game item
// tooltip prints from the item itself (CIFItemTooltip, 78C5B0 -> 78BD00 ->
// 555720): the reference's ranges come from the catalogue, the roll from
// here.
type WornItem struct {
	Slot      int64  `json:"slot"`
	RefItemID uint32 `json:"refItemId"`
	Plus      int64  `json:"plus,omitempty"`
	// Variance is the item's packed white-stat roll as a decimal u64 (five
	// bits a stat, 0..31, in the order of its equipment family), as the
	// CSOItem wire carries it; absent for an item without one.
	Variance string `json:"variance,omitempty"`
	// Durability is a worn item's current durability, present even at 0 (a
	// broken item, which the tooltip prints as 0/max in red); absent on an
	// avatar socket and on an item whose reference has no durability
	// (ammunition, accessories), which can never be broken.
	Durability *int64 `json:"durability,omitempty"`
	// MagicOptions are the item's blue stats in the item's own order, each
	// the encoded u64 the CSOItem wire carries (low u16 the magicoption.txt
	// param id, high u32 the value; domain.InventoryRow) as a decimal
	// string, so the client's own tooltip code reads them unchanged.
	MagicOptions []string `json:"magicOptions,omitempty"`
}

// Look is what the 3D viewer (V1) needs to draw the character.
type Look struct {
	BodyRefObjID uint32     `json:"bodyRefObjId"`
	BodyCodename string     `json:"bodyCodename"`
	Worn         []WornItem `json:"worn"`
	Avatar       []WornItem `json:"avatar"`
}

// Profile is P6 GET /public/v1/characters/{name}.
type Profile struct {
	Name      string    `json:"name"`
	Race      string    `json:"race"`
	Gender    string    `json:"gender"`
	Level     int64     `json:"level"`
	Guild     *Guild    `json:"guild"`
	Job       string    `json:"job"`
	JobLevel  uint8     `json:"jobLevel"`
	Masteries []Mastery `json:"masteries"`
	Stats     Stats     `json:"stats"`
	Look      Look      `json:"look"`
	// GearHidden is the player's "hide my gear" choice: look keeps the
	// items, plus and sockets (the 3D view draws them, and the game shows
	// +N glows to anyone nearby) but no rolls, blue stats or durability.
	// The +N therefore stays public, here and in the rankings' gear plus.
	GearHidden  bool    `json:"gearHidden,omitempty"`
	UniqueKills int     `json:"uniqueKills"`
	LastLogin   *string `json:"lastLogin"`
	Online      bool    `json:"online"`
}

// Rule is one port-only switch and its live value.
type Rule struct {
	Flag   string `json:"flag"`
	On     bool   `json:"on"`
	Native string `json:"native"`
	Now    string `json:"now"`
	Why    string `json:"why"`
}

// Deviation is a deliberate difference that has no switch.
type Deviation struct {
	Title  string `json:"title"`
	Native string `json:"native"`
	Now    string `json:"now"`
}

// StatusTick is the last five minutes of simulation ticks: the median
// and 99th percentile durations and the percent over the tick interval.
type StatusTick struct {
	P50Ms       float64 `json:"p50Ms"`
	P99Ms       float64 `json:"p99Ms"`
	SlowPercent float64 `json:"slowPercent"`
}

// StatusPoint is one closed hour: its peak online and tick p99.
type StatusPoint struct {
	At     string  `json:"at"`
	Online int     `json:"online"`
	P99Ms  float64 `json:"p99Ms"`
}

// StatusResponse is P7 GET /public/v1/status. History holds up to 48
// hourly points, oldest first, since the GameWorld started.
type StatusResponse struct {
	Up            bool          `json:"up"`
	Online        int           `json:"online"`
	UptimeSeconds int64         `json:"uptimeSeconds"`
	Tick          StatusTick    `json:"tick"`
	History       []StatusPoint `json:"history"`
	// MasteryTotalOverride is the shard's total mastery allowance when a
	// beta setting replaces the native rule (CH 300, EU min(2 x level,
	// 240)); omitted when native. It is the value the client receives.
	MasteryTotalOverride int `json:"masteryTotalOverride,omitempty"`
}

// ScheduleEvent is one event the server runs. Kind is "fortress" (the
// war), "fortress-registration" or "maintenance"; EndsAt is null for an
// open-ended maintenance.
type ScheduleEvent struct {
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	StartsAt string  `json:"startsAt"`
	EndsAt   *string `json:"endsAt"`
	Running  bool    `json:"running"`
}

// ScheduleResponse is P8 GET /public/v1/schedule.
type ScheduleResponse struct {
	ServerTime string          `json:"serverTime"`
	Events     []ScheduleEvent `json:"events"`
}

// ChatLine is one public chat line. Kind is "chat" (ordinary beta chat,
// shard-wide), "global" (the Global Chatting item) or "notice" (an
// operator announcement, Character null). Gameplay announcements are
// not chat: they come from /public/v1/events. Profile is false when the
// character hid their profile: the site shows the name without a link.
type ChatLine struct {
	At        string  `json:"at"`
	Kind      string  `json:"kind"`
	Character *string `json:"character"`
	Profile   bool    `json:"profile"`
	Text      string  `json:"text"`
}

// ChatResponse is P10 GET /public/v1/chat: the latest public lines,
// oldest first, at most chatfeed.MaxLines. Whispers, party, guild and
// union lines never enter it; a banned or muted author's lines are gone.
type ChatResponse struct {
	Lines []ChatLine `json:"lines"`
}

// EventLine is one automated gameplay announcement, as the game's notice
// bar showed it: a seal drop, an enhancement advance, a job kill or a
// break.
type EventLine struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// EventsResponse is GET /public/v1/events: the latest announcements,
// oldest first, at most chatfeed.MaxEvents. A separate stream from chat,
// so a busy drop table never scrolls people's lines off the site.
type EventsResponse struct {
	Lines []EventLine `json:"lines"`
}

// Gear is a ranked player's worn equipment: the plus sum and highest, and
// the gear points the ranking orders by (rankings.go gearPoints).
type Gear struct {
	PlusTotal int `json:"plusTotal"`
	BestPlus  int `json:"bestPlus"`
	Points    int `json:"points"`
	// best is the strongest single piece's points, the first tie-break.
	best int
}

// RankingRow is one visible player in a ranking. ReachedAt is when the
// player reached its level, recorded only at the milestone levels (20, 30
// ... 90) and only since tracking began; otherwise null.
type RankingRow struct {
	Rank        int     `json:"rank"`
	Name        string  `json:"name"`
	Race        string  `json:"race"`
	Gender      string  `json:"gender"`
	Level       int64   `json:"level"`
	ExpPercent  float64 `json:"expPercent"`
	ReachedAt   *string `json:"reachedAt"`
	Guild       string  `json:"guild,omitempty"`
	Job         string  `json:"job,omitempty"`
	UniqueKills int     `json:"uniqueKills"`
	Gear        Gear    `json:"gear"`
}

// RankingsResponse is GET /public/v1/rankings: one page of 50.
type RankingsResponse struct {
	By          string       `json:"by"`
	Race        *string      `json:"race"`
	Page        int          `json:"page"`
	Pages       int          `json:"pages"`
	Total       int          `json:"total"`
	GeneratedAt string       `json:"generatedAt"`
	Rows        []RankingRow `json:"rows"`
}

// RulesResponse is P9 GET /public/v1/rules.
type RulesResponse struct {
	Rules      []Rule      `json:"rules"`
	Deviations []Deviation `json:"deviations"`
}

// GuildsResponse is GET /public/v1/guilds: one page of 50 (guilds.go).
type GuildsResponse struct {
	Page        int        `json:"page" doc:"1-based page number."`
	Pages       int        `json:"pages" doc:"Pages in the full list (at least 1)."`
	Total       int        `json:"total" doc:"Guilds matching the query."`
	Query       *string    `json:"query,omitempty" doc:"The lower-cased name prefix searched for, when given."`
	GeneratedAt string     `json:"generatedAt" doc:"When this answer was built (RFC 3339, UTC)."`
	Rows        []GuildRow `json:"rows"`
}

// GuildRow is one guild on the list.
type GuildRow struct {
	Rank     int      `json:"rank" doc:"Place on the full list: level, then members, then name."`
	Name     string   `json:"name"`
	Level    int      `json:"level" doc:"Guild level, 1 to 5."`
	Members  int      `json:"members"`
	Master   string   `json:"master" doc:"The master's name, \"a hunter\" for a master who hides, \"\" if unknown."`
	Union    []string `json:"union" doc:"The other guilds of its union, by name; empty when it has none."`
	Fortress *string  `json:"fortress,omitempty" doc:"The fortress it holds, by town in lower case (\"jangan\")."`
	Founded  *string  `json:"founded,omitempty" doc:"When the guild was founded (RFC 3339, UTC); absent when it predates the record."`
}
