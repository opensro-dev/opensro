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

// SchemaVersion moves when a response shape changes incompatibly.
const SchemaVersion = 1

// Respawn is a unique nest's authored delay range.
type Respawn struct {
	MinSeconds int `json:"minSeconds"`
	MaxSeconds int `json:"maxSeconds"`
}

// Window is the span a dead unique can return in.
type Window struct {
	OpensAt  string `json:"opensAt"`
	ClosesAt string `json:"closesAt"`
}

// LastKill is a unique's most recent recorded kill.
type LastKill struct {
	At     string `json:"at"`
	Killer string `json:"killer"`
	Guild  string `json:"guild,omitempty"`
}

// Unique is one P1 row.
type Unique struct {
	RefObjID  uint32    `json:"refObjId"`
	Name      string    `json:"name"`
	Level     uint8     `json:"level"`
	RegionID  uint16    `json:"regionId"`
	Alive     bool      `json:"alive"`
	SpawnedAt *string   `json:"spawnedAt"`
	Respawn   Respawn   `json:"respawn"`
	NextWin   *Window   `json:"nextWindow"`
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

// WornItem is one equipment or avatar socket.
type WornItem struct {
	Slot      int64  `json:"slot"`
	RefItemID uint32 `json:"refItemId"`
	Plus      int64  `json:"plus,omitempty"`
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
	Name        string    `json:"name"`
	Race        string    `json:"race"`
	Gender      string    `json:"gender"`
	Level       int64     `json:"level"`
	Guild       *Guild    `json:"guild"`
	Job         string    `json:"job"`
	JobLevel    uint8     `json:"jobLevel"`
	Masteries   []Mastery `json:"masteries"`
	Stats       Stats     `json:"stats"`
	Look        Look      `json:"look"`
	UniqueKills int       `json:"uniqueKills"`
	LastLogin   *string   `json:"lastLogin"`
	Online      bool      `json:"online"`
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

// RulesResponse is P9 GET /public/v1/rules.
type RulesResponse struct {
	Rules      []Rule      `json:"rules"`
	Deviations []Deviation `json:"deviations"`
}
