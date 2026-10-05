/*
===========================================================================

alliance.go - the persisted guild unions

A union (CAlliance) is up to eight guilds in fixed slots, the leading
guild in slot 0 (CAlliance_SetGuildSlot 5B8C70 bounds the index to 8;
CAlliance_GetMasterGuildID 5B9260 reads slot 0). It is cross-guild
state, so it lives in the authority store's alliances table, reached
through AllianceStore.

===========================================================================
*/
package domain

// AllianceSlots is the CAlliance guild slot count (5B8C70: index < 8).
const AllianceSlots = 8

/*
================
AllianceRecord

One union: its id, its emblem and the guild in each slot (0 = empty).
A union lives as long as its leading guild leads it, so its id is that
guild's id (guild ids are never reused).
================
*/
type AllianceRecord struct {
	AllianceID int64                `json:"allianceId"`
	Crest      uint32               `json:"crest,omitempty"`
	Guilds     [AllianceSlots]int64 `json:"guilds"`
}

/*
================
AllianceStore

The authority store's union door. Each save commits before it returns; a
failed save is an error, never a silent loss.
================
*/
type AllianceStore interface {
	// Alliances returns a division's stored unions in id order.
	Alliances(divisionID string) ([]AllianceRecord, error)
	// SaveAlliance writes (present) or removes one union.
	SaveAlliance(divisionID string, record AllianceRecord, present bool) error
}
