/*
===========================================================================

unique_kill.go - one recorded unique monster kill (port-only)

The persisted row of the store's unique_kills table (layout 9). Port-only,
not native: the original announces the killer and keeps nothing. The
killer is named by character id so the public read side applies the
character's current privacy choice to every past kill.

===========================================================================
*/
package domain

// PublicLevelMilestones are the levels whose first arrival a character
// records (LevelReachedAt) for the community site's world-firsts.
var PublicLevelMilestones = []uint8{20, 30, 40, 50, 60, 70, 80, 90}

/*
================
UniqueKill

Seq numbers the kill within its division. KillerName is the name at the
time of the kill; the read side looks the character up by KillerCharID for
its current privacy choice and guild.
================
*/
type UniqueKill struct {
	Seq          int64  `json:"seq"`
	AtMs         int64  `json:"atMs"`
	RefObjID     uint32 `json:"refObjId"`
	KillerCharID int64  `json:"killerCharId,omitempty"`
	KillerName   string `json:"killerName,omitempty"`
}

/*
================
RecordLevelReached

Notes the time of each milestone the character crossed going from level
from to level to (a gain can cross several). Only milestones above from
count, so a character that predates the record never claims an old
milestone as a new first, and a milestone already recorded keeps its time.
================
*/
func (character *Character) RecordLevelReached(from, to uint8, atMs int64) bool {
	if character == nil || to <= from {
		return false
	}
	changed := false
	for _, milestone := range PublicLevelMilestones {
		if milestone <= from || milestone > to {
			continue
		}
		if _, seen := character.LevelReachedAt[milestone]; seen {
			continue
		}
		if character.LevelReachedAt == nil {
			character.LevelReachedAt = make(map[uint8]int64, len(PublicLevelMilestones))
		}
		character.LevelReachedAt[milestone] = atMs
		changed = true
	}
	return changed
}
