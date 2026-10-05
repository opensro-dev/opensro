/*
===========================================================================

fortress.go - the persisted fortress state

The shard rows a fortress war leaves behind: who occupies each fortress
and who holds it during a war (_SiegeFortress GuildID and TempGuildID),
and the guilds registered for the coming war (_SiegeFortressRequest). They
are cross-character state, so they live in the authority store's
fortresses and fortress_requests tables, reached through FortressStore.

===========================================================================
*/
package domain

/*
================
FortressRecord

One _SiegeFortress row.
================
*/
type FortressRecord struct {
	FortressID  uint32 `json:"fortressId"`
	GuildID     int64  `json:"guildId,omitempty"`
	TempGuildID int64  `json:"tempGuildId,omitempty"`
}

/*
================
FortressRequestRecord

One _SiegeFortressRequest row: a guild's application and its kind (0 to
attack, 1 to ally with the occupier).
================
*/
type FortressRequestRecord struct {
	FortressID uint32
	GuildID    int64
	Kind       uint8
}

/*
================
FortressStore

The authority store's fortress door. Each save commits before it
returns; a failed save is an error, never a silent loss.
================
*/
type FortressStore interface {
	// FortressState returns a division's stored fortresses and requests in
	// fortress order.
	FortressState(divisionID string) ([]FortressRecord, []FortressRequestRecord, error)
	// SaveFortress writes one fortress row.
	SaveFortress(divisionID string, record FortressRecord) error
	// SaveFortressRequest writes (present) or removes one request.
	SaveFortressRequest(divisionID string, request FortressRequestRecord, present bool) error
}
