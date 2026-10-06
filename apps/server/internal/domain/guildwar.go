/*
===========================================================================

guildwar.go - durable guild-war terms, combat accounts and settlement

The guild-war authority owns these rows. The store commits stakes, scores
and compensation together with the affected guild and character records.

===========================================================================
*/
package domain

/*
================
GuildWarRecord

SR_ShardManager 435A10: two guilds, doubled stake, optional deadline and
unsigned scores. A zero deadline means an unlimited war.
================
*/
type GuildWarRecord struct {
	ID         uint32    `json:"id"`
	Type       uint8     `json:"type"`
	ScoreIndex uint8     `json:"scoreIndex"`
	Stake      uint32    `json:"stake"`
	EndMs      int64     `json:"endMs"`
	Guilds     [2]int64  `json:"guilds"`
	Scores     [2]uint32 `json:"scores"`
}

/*
================
GuildWarTerms
================
*/
type GuildWarTerms struct {
	Type       uint8
	Period     uint32
	ScoreIndex uint8
	Stake      uint32
}

/*
================
GuildWarStart
================
*/
type GuildWarStart struct {
	Record  GuildWarRecord
	Masters [2]int64
}

/*
================
GuildWarCombat

The credited owner and actual striker can differ for an attacking COS.
Level facts are captured before death changes the victim's progression.
================
*/
type GuildWarCombat struct {
	WarID    uint32
	KillerID int64
	VictimID int64
	Score    uint8
}

/*
================
GuildWarMemberScore
================
*/
type GuildWarMemberScore struct {
	CharacterID int64  `json:"characterId"`
	GuildID     int64  `json:"guildId"`
	Score       uint32 `json:"score"`
	Kills       uint32 `json:"kills"`
	Deaths      uint32 `json:"deaths"`
}

/*
================
GuildWarStore

Every mutator commits before returning its new row. A nonzero refusal
makes no change; a storage failure is an error, never a successful receipt.
================
*/
type GuildWarStore interface {
	GuildWars(division string) ([]GuildWarRecord, error)
	BeginGuildWar(division string, start GuildWarStart) (GuildWarRecord, uint8, error)
	AccountGuildWarCombat(division string, combat GuildWarCombat) (GuildWarRecord, uint8, error)
	EndGuildWar(division string, id uint32, winner int64) (uint8, error)
	GuildWarMemberScores(division string, guildID int64) ([]GuildWarMemberScore, error)
}
