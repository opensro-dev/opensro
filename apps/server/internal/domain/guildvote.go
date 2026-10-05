/*
===========================================================================

guildvote.go - the master release vote a guild holds at its manager

A member may call a vote to replace a master who has been gone 45 days
(v1.188 GuildManager_RequestMasterRelease 5C6AC0 asks the shard
_Guild_Master_Election_Available(master, 45)). The vote lasts ten minutes
(UIIT_MSG_MRELEASE_CONFIRM), members away three days have no part in it
(UIIT_MSG_MRELEASEERR_NOTVOTING), and its voters and candidates may not
leave or be expelled while it runs (0x38 / 0x39). v1.188 moves ballots
between options (Vote_MoveBallot 5E7A70) and the ShardManager closes it.

INFERENCE (the ShardManager is not in the research binaries): the
candidates are the members seen within three days other than the master,
in roster order; a vote with one candidate strictly ahead and at least one
ballot elects them, any other closes broken (UIIT_MSG_MRELEASE_BROKEN).
The v1.150 client never receives a candidate list (0x3A6C type 2 is an
assert in 7603D0), so its window offers nothing to vote for and a vote it
calls closes broken unless a ballot arrives by another client.

===========================================================================
*/

package domain

import (
	"maps"
	"slices"
)

const (
	// GuildVoteMasterRelease is the vote kind the release row looks up
	// (SGuildData_FindVoteEntry(0), 82CC00).
	GuildVoteMasterRelease uint8 = 0
	// GuildVoteDurationMs is the ten minutes of UIIT_MSG_MRELEASE_CONFIRM.
	GuildVoteDurationMs int64 = 10 * 60 * 1000
	// GuildMasterAbsenceMs is the 45 days 5C6AC0 hands the shard.
	GuildMasterAbsenceMs int64 = 45 * 24 * 60 * 60 * 1000
	// GuildVoterAbsenceMs is the three days of UIIT_MSG_MRELEASEERR_NOTVOTING.
	GuildVoterAbsenceMs int64 = 3 * 24 * 60 * 60 * 1000
	// GuildVoteNoOption is the "no previous ballot" option 0x3A6C type 4
	// carries (82D0B0 skips the decrement for 0xFF).
	GuildVoteNoOption uint8 = 0xff
)

/*
================
GuildVote

One open vote: the candidates by option index and each voter's option.
================
*/
type GuildVote struct {
	ID           uint32           `json:"id"`
	Kind         uint8            `json:"kind"`
	EndsAtUnixMs int64            `json:"endsAtUnixMs"`
	Candidates   []uint32         `json:"candidates"`
	Ballots      map[uint32]uint8 `json:"ballots,omitempty"`
}

/*
================
GuildVote.Clone
================
*/
func (v *GuildVote) Clone() *GuildVote {
	if v == nil {
		return nil
	}
	next := *v
	next.Candidates = append([]uint32(nil), v.Candidates...)
	next.Ballots = make(map[uint32]uint8, len(v.Ballots))
	maps.Copy(next.Ballots, v.Ballots)
	return &next
}

/*
================
GuildVote.Count

The ballots an option holds.
================
*/
func (v *GuildVote) Count(option uint8) uint8 {
	count := uint8(0)
	for _, chosen := range v.Ballots {
		if chosen == option && count < 0xff {
			count++
		}
	}
	return count
}

/*
================
GuildVote.Involves

Whether a member is a candidate or has voted.
================
*/
func (v *GuildVote) Involves(jid uint32) bool {
	_, voted := v.Ballots[jid]
	return voted || slices.Contains(v.Candidates, jid)
}

/*
================
GuildVote.Winner

The candidate strictly ahead with at least one ballot (INFERENCE above).
================
*/
func (v *GuildVote) Winner() (uint32, bool) {
	best, bestCount, tied := -1, uint8(0), false
	for option := range v.Candidates {
		count := v.Count(uint8(option))
		switch {
		case count > bestCount:
			best, bestCount, tied = option, count, false
		case count == bestCount && count > 0:
			tied = true
		}
	}
	if best < 0 || tied {
		return 0, false
	}
	return v.Candidates[best], true
}

// GuildVoteBallot is a committed ballot: the option it left and joined and
// the joined option's count (0x3A6C type 4).
type GuildVoteBallot struct {
	Snapshot GuildSnapshot
	VoteID   uint32
	Previous uint8
	Option   uint8
	Count    uint8
}

// GuildVoteOutcome is a closed vote. Elected names the heir and the
// master they replaced, whose rows already carry the new grades.
type GuildVoteOutcome struct {
	Snapshot GuildSnapshot
	VoteID   uint32
	Elected  bool
	Heir     GuildMemberRecord
	Former   GuildMemberRecord
}
