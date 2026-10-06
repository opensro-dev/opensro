/*
===========================================================================

fortress.go - the persisted fortress state

The shard rows a fortress war leaves behind: who occupies each fortress
and who holds it during a war (_SiegeFortress GuildID and TempGuildID),
its signed tax rate and accumulated tax gold,
the guilds registered for the coming war (_SiegeFortressRequest), and the
structures standing on its event zones with their hit points and state
(_SiegeFortressStruct). They are cross-character state, so they live in
the authority store's fortresses, fortress_requests and
fortress_structures tables, reached through FortressStore.

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
	TaxRate     int16  `json:"taxRate,omitempty"`
	TaxGold     int64  `json:"taxGold,omitempty"`
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
FortressStructureRecord

One _SiegeFortressStruct row: the structure installed on an event zone,
its owner, hit points and state word.
================
*/
type FortressStructureRecord struct {
	FortressID    uint32 `json:"fortressId"`
	EventStructID uint32 `json:"eventStructId"`
	RefObjID      uint32 `json:"refObjId"`
	OwnerGuildID  int64  `json:"ownerGuildId,omitempty"`
	HP            uint32 `json:"hp"`
	State         uint16 `json:"state,omitempty"`
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
	// FortressStructures returns a division's stored structures in
	// fortress and event-zone order.
	FortressStructures(divisionID string) ([]FortressStructureRecord, error)
	// SaveFortressStructure writes (present) or removes one zone's structure.
	SaveFortressStructure(divisionID string, structure FortressStructureRecord, present bool) error
}
