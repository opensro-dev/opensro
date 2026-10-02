/*
===========================================================================

pk.go - durable criminal records and transient relation projections

===========================================================================
*/
package domain

// PKRecord is the durable criminal record. Event teams and equipment-derived
// free-battle groups are deliberately separate identities. Mutations belong in
// the same character-store transaction as the combat consequence that caused
// them; serializers only read the record.
/*
================
PKRecord
================
*/
type PKRecord struct {
	DailyCount uint8  `json:"dailyCount"`
	TotalCount uint16 `json:"totalCount"`
	Penalty    uint32 `json:"penalty"`
}

// PVPState is CICUser+4F4. The aggression list is transient; a durable penalty
// takes priority on entry (research server 4E11CD..4E11E0).
/*
================
PVPState
================
*/
func (c *Character) PVPState() uint8 {
	if c == nil {
		return 0
	}
	if c.PK != nil && c.PK.Penalty != 0 {
		return 2
	}
	// 52AB89 leaves the last target protected for one tick after its grey
	// state clears. The next scheduled tick removes that final count.
	if len(c.Aggressions) == 1 {
		for _, remaining := range c.Aggressions {
			if remaining == 1 {
				return 0
			}
		}
	}
	if len(c.Aggressions) != 0 {
		return 1
	}
	return 0
}

// EventMembership is a projection of an event owner's enrolled participant.
// It is runtime-only: a restart must not recreate an event from a character's
// saved color or a default numeric team. ID zero is never an active event.
/*
================
EventMembership
================
*/
type EventMembership struct {
	ID   uint32
	Team uint8
}

/*
================
EventTeam
================
*/
func (c *Character) EventTeam() uint8 {
	if c == nil || c.EventMembership == nil || c.EventMembership.ID == 0 {
		return 0xff
	}
	return c.EventMembership.Team
}
