/*
===========================================================================

npcguildvote.go - the master release vote at the guild manager

The guild manager's release row confirms (UIIT_MSG_MRELEASE_CONFIRM) and
sends 0x76DC; once a vote runs the row becomes the vote row, whose window
sends a ballot (CIFGuildMasterElection 5EFF00):

	0x76DC [u32 npc]                     release  -> 0xB6DC [1] | [2][code]
	0x7330 [u32 npc][u32 vote][u8 opt]   ballot   -> 0xB330 [1] | [2][code]

Opening the vote window, the vote row first asks for the candidates
(5DA1B0 case 0x1A, NetClient_Send7332 700FF0):

	0x7332 [u32 vote]                    candidates -> 0xB332 [1][body] | [2][code]

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
	opGuildVoteJoin          uint16 = 0x7332
	opGuildVoteJoinDone      uint16 = 0xb332
	opGuildVotePush          uint16 = 0x3a6c

	// The ballot refusals the client reads in category 0x15 (75CC50).
	// INFERENCE: the low bytes of the reference server's ballot 0x5006 (no vote) and
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

One frame list to every member of guildID's snapshot except the named
actor. This is the action lane's one guild publication: its 0x3B29 deltas
reach a member only while that member's session holds the guild's block
(PushGuildFrames, guild/baseline.go).
================
*/
func (rt *Runtime) pushGuildMembers(division string, guildID int64, members []domain.GuildMemberRecord, skip int64, frames []wire.Frame) {
	for _, member := range members {
		if member.CharID == skip {
			continue
		}
		if rt.PushGuildFrames != nil {
			rt.PushGuildFrames(division, member.Name, guildID, frames)
		} else if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, member.Name, frames)
		}
	}
}

/*
================
HandleGuildMasterRelease

0x76DC: the native admission (0x0D outside a guild, 0x37 while a vote runs,
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
	rt.pushGuildMembers(division, snapshot.Guild.ID, snapshot.Members, c.ID, []wire.Frame{push})
	result := guildNpcAnswer(opGuildMasterReleaseDone, 0)
	result.Frames = append(result.Frames, push)
	return result
}

/*
================
HandleGuildBallot

0x7330 from the election window: the reference server's ballot cast.
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
	rt.pushGuildMembers(division, ballot.Snapshot.Guild.ID, ballot.Snapshot.Members, c.ID, []wire.Frame{push})
	result := guildNpcAnswer(opGuildBallotDone, 0)
	result.Frames = append(result.Frames, push)
	return result
}

/*
================
HandleGuildVoteParticipate

0x7332 [u32 vote] from the vote row while the client holds no candidates
for it: the open vote's candidates and their ballots, the body of the reference
server's participation answer. A member of no guild, or a vote that
is not the guild's open one, is refused [2][0x06], the low byte of its
0x5006. INFERENCE: a vote past its end that the tick has not closed yet is
already over for the client's countdown, so it is refused the same way.
================
*/
func (rt *Runtime) HandleGuildVoteParticipate(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	voteID, err := r.U32()
	store := rt.deps.GuildAuthority()
	if c == nil || err != nil || r.Done() != nil || store == nil {
		return guildNpcAnswer(opGuildVoteJoinDone, guildNpcRefused)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	now := rt.Now().UnixMilli()
	guildID, ok := store.GuildOfCharacter(division, c.ID)
	if !ok {
		return guildNpcAnswer(opGuildVoteJoinDone, ballotErrNoVote)
	}
	record, _, ok := store.Guild(division, guildID)
	if !ok || record.Vote == nil || record.Vote.ID != voteID || record.Vote.EndsAtUnixMs <= now {
		return guildNpcAnswer(opGuildVoteJoinDone, ballotErrNoVote)
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opGuildVoteJoinDone,
		Payload: guild.EncodeVoteParticipationB332(record.Vote, now)}}}
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
					wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberGrade3B29(outcome.Heir.JID, outcome.Heir.Grade, outcome.Heir.PermMask)},
					// The commander role moves with the master.
					wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberFortressRole3B29(outcome.Former.JID, outcome.Former.FortressRole)},
					wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeMemberFortressRole3B29(outcome.Heir.JID, outcome.Heir.FortressRole)})
			}
			rt.pushGuildMembers(division, outcome.Snapshot.Guild.ID, outcome.Snapshot.Members, 0, frames)
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
