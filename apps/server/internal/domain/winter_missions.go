/*
===========================================================================

winter_missions.go - a division's winter Server Mission rows

The shard database's _ServerEvent rows (ID, Completions, Goal, Count),
kept per division so a restart resumes the missions where they stood, as
the native ShardManager reloads them. The rules are
progression/winter_missions.go's; the store only keeps the record.

===========================================================================
*/
package domain

/*
================
WinterMissionRow

One mission's live row.
================
*/
type WinterMissionRow struct {
	ID          uint32 `json:"id"`
	Completions int    `json:"completions"`
	Goal        int    `json:"goal"`
	Count       int    `json:"count"`
}

/*
================
WinterMissions

A division's mission rows, and when the Snow Flake Event's EXP/SP bonus
ends (Unix ms; 0 when none runs): the remaining reward time the native
server keeps beside the rows (RemainServerEventRewardTime).
================
*/
type WinterMissions struct {
	Rows         []WinterMissionRow `json:"rows,omitempty"`
	ExpUpUntilMs int64              `json:"expUpUntilMs,omitempty"`
}

/*
================
WinterMissions.Valid

No negative counts, completions or end time, and a goal on every row.
================
*/
func (m WinterMissions) Valid() bool {
	if m.ExpUpUntilMs < 0 {
		return false
	}
	for _, row := range m.Rows {
		if row.Goal <= 0 || row.Count < 0 || row.Completions < 0 {
			return false
		}
	}
	return true
}
