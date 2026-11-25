/*
===========================================================================

job.go - character job membership

The wanted streak lives in character_mostwanted.go.

These records travel with a character; action owns their mutations.

===========================================================================
*/
package domain

/*
==================
CharacterJob

A job guild membership: the job type joined at a guild NPC, its grade
(+0x783, 1 on joining) and experience (+0x18B4), the alias a dressed job
player shows, and the time before a thief or hunter who withdrew may join
again (the seven-day timed job of the v1.188 0x70E2 job leave handler).

WeeklyReward is the native _CharTrijob.Contribution (job record +0x24,
raised by the server's contribution routine): a trader's sale share or a
thief's or hunter's job experience this week. Reward (+0x28) is the outcome
a thief or hunter collects at the guild; the weekly cycle (jobweek.go)
credits it.
==================
*/
type CharacterJob struct {
	WeeklyReward int32  `json:"weeklyReward,omitempty"`
	Reward       int32  `json:"reward,omitempty"`
	Type         uint8  `json:"type,omitempty"`
	Grade        uint8  `json:"grade,omitempty"`
	Exp          uint32 `json:"exp,omitempty"`
	Alias        string `json:"alias,omitempty"`
	RejoinAtMs   int64  `json:"rejoinAtMs,omitempty"`
}
