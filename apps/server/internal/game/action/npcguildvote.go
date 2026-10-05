/*
===========================================================================

npcguildvote.go - the master release vote at the guild manager

The guild manager's release row confirms (UIIT_MSG_MRELEASE_CONFIRM) and
sends 0x76DC; once a vote runs the row becomes the vote row, whose window
sends a ballot (CIFGuildMasterElection 5EFF00):

	0x76DC [u32 npc]                     release  -> 0xB6DC [1] | [2][code]
	0x7330 [u32 npc][u32 vote][u8 opt]   ballot   -> 0xB330 [1] | [2][code]

and every member online follows the vote on 0x3A6C (7603D0): type 1 when
it opens, 4 for each ballot, 3 when it closes. The rules and their
inferences are domain/guildvote.go's; the store commits each step.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opGuildMasterRelease     uint16 = 0x76dc
	opGuildMasterReleaseDone uint16 = 0xb6dc
	opGuildBallot            uint16 = 0x7330
	opGuildBallotDone        uint16 = 0xb330
	opGuildVotePush          uint16 = 0x3a6c

	// The ballot refusals the client reads in category 0x15 (75CC50).
	// INFERENCE: the low bytes of v1.188 5E7EA0's 0x5006 (no vote) and
	// 0x5008 (no such candidate).
	ballotErrNoVote       uint8 = 0x06
	ballotErrNotCandidate uint8 = 0x08
)

/*
================
memberLastSeen

A guild member's last-seen time: now while admitted to the world, else
the time they last left it (0 when never recorded).
================
*/
func (rt *Runtime) memberLastSeen(division string, nowMs int64) func(int64) int64 {
	characters := rt.deps.CharactersForDivision(division)
	return func(id int64) int64 {
		for _, c := range characters {
			if c.ID != id {
				continue
			}
			if _, online := rt.characterAdmissions.Load(simulation.WorldKey(division, c.Name)); online {
				return nowMs
			}
			return c.LastSeenUnixMs
		}
		return 0
	}
}

/*
================
pushGuildMembers

One frame list to every member of a snapshot except the named actor.
================
*/
func (rt *Runtime) pushGuildMembers(division string, members []domain.GuildMemberRecord, skip int64, frames []wire.Frame) {
	if rt.PushCharacterFrames == nil {
		return
	}
	for _, member := range members {
		if member.CharID != skip {
			rt.PushCharacterFrames(division, member.Name, frames)
		}
	}
}

/*
================
HandleGuildMasterRelease

0x76DC: 5C6AC0's admission (0x0D outside a guild, 0x37 while a vote runs,
0x33 while the master is not long gone), then the vote opens for ten
minutes.
================
*/
func (rt *Runtime) HandleGuildMasterRelease(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	store := rt.deps.GuildAuthority()
	if c == nil || err != nil || r.Done() != nil || store == nil {
		return guildNpcAnswer(opGuildMasterReleaseDone, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildMasterReleaseDone, guildNpcRefused)
	}
	now := rt.Now().UnixMilli()
	snapshot, refusal := store.OpenMasterReleaseVoteAs(division, c.ID, now, rt.memberLastSeen(division, now))
	switch refusal {
	case domain.GuildRefusalNone:
	case domain.GuildRefusalVoteOpen:
		return guildNpcAnswer(opGuildMasterReleaseDone, guild.GuildErrVoteRunning)
	case domain.GuildRefusalVoteNotTime:
		return guildNpcAnswer(opGuildMasterReleaseDone, guild.GuildErrVoteNotTime)
	default:
		return guildNpcAnswer(opGuildMasterReleaseDone, guildRefusalCode(refusal))
	}
	push := wire.Frame{Opcode: opGuildVotePush, Payload: guild.EncodeVoteOpened3A6C(snapshot.Guild.Vote, now)}
	rt.pushGuildMembers(division, snapshot.Members, c.ID, []wire.Frame{push})
	result := guildNpcAnswer(opGuildMasterReleaseDone, 0)
	result.Frames = append(result.Frames, push)
	return result
}

/*
================
HandleGuildBallot

0x7330 from the election window: VoteMgr_CastBallot (5E7EA0).
================
*/
func (rt *Runtime) HandleGuildBallot(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, err := r.U32()
	voteID, err2 := r.U32()
	option, err3 := r.U8()
	store := rt.deps.GuildAuthority()
	if c == nil || err != nil || err2 != nil || err3 != nil || r.Done() != nil || store == nil {
		return guildNpcAnswer(opGuildBallotDone, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if !rt.guildManagerNpc(division, c, gid) {
		return guildNpcAnswer(opGuildBallotDone, guildNpcRefused)
	}
	ballot, refusal := store.CastGuildBallotAs(division, c.ID, voteID, option)
	switch refusal {
	case domain.GuildRefusalNone:
	case domain.GuildRefusalNoVote:
		return guildNpcAnswer(opGuildBallotDone, ballotErrNoVote)
	case domain.GuildRefusalNotCandidate:
		return guildNpcAnswer(opGuildBallotDone, ballotErrNotCandidate)
	default:
		return guildNpcAnswer(opGuildBallotDone, guildNpcRefused)
	}
	push := wire.Frame{Opcode: opGuildVotePush,
		Payload: guild.EncodeVoteBallot3A6C(ballot.VoteID, ballot.Previous, ballot.Option, ballot.Count)}
	rt.pushGuildMembers(division, ballot.Snapshot.Members, c.ID, []wire.Frame{push})
	result := guildNpcAnswer(opGuildBallotDone, 0)
	result.Frames = append(result.Frames, push)
	return result
}

/*
================
advanceGuildVotes

The ShardManager's close, on the tick: each due vote in a division with
players in it closes, electing where the tally allows, and its members
learn the result and the new grades.
================
*/
func (rt *Runtime) advanceGuildVotes(nowMs int64) {
	store := rt.deps.GuildAuthority()
	if store == nil {
		return
	}
	divisions := map[string]bool{}
	rt.characterAdmissions.Range(func(_, value any) bool {
		divisions[value.(populationAdmission).division] = true
		return true
	})
	for division := range divisions {
		for _, outcome := range store.CloseDueGuildVotes(division, nowMs, guild.JoinerGrade, guild.JoinerPermMask) {
			frames := []wire.Frame{{Opcode: opGuildVotePush,
				Payload: guild.EncodeVoteClosed3A6C(outcome.VoteID, outcome.Elected, outcome.Heir.JID)}}
			if outcome.Elected {
				frames = append(frames,
					wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberGrade3B29(outcome.Former.JID, outcome.Former.Grade, outcome.Former.PermMask)},
					wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberGrade3B29(outcome.Heir.JID, outcome.Heir.Grade, outcome.Heir.PermMask)})
			}
			rt.pushGuildMembers(division, outcome.Snapshot.Members, 0, frames)
		}
	}
}

/*
================
noteLastSeen

The character leaves the world: the guild vote's absence clock starts.
================
*/
func (rt *Runtime) noteLastSeen(c *enterworld.Character) {
	if c == nil {
		return
	}
	now := rt.Now().UnixMilli()
	rt.deps.Mutate(c, "last-seen", func() { c.LastSeenUnixMs = now })
}
