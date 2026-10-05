/*
===========================================================================

guildvote.go - the guild door's master release vote

The vote lives on the guild record (domain.GuildVote) and every change to
it, and the hand-over a close may bring, commits with the guild in one
store transaction. The rules are domain/guildvote.go's.

===========================================================================
*/

package store

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/domain"
)

/*
================
OpenMasterReleaseVoteAs

5C6AC0: any member but the master, while no vote runs (0x37) and the
master has been gone GuildMasterAbsenceMs (0x33). A master never seen
leaving counts as present.
================
*/
func (door storeGuildDoor) OpenMasterReleaseVoteAs(divisionID string, actorID int64, nowMs int64, seen func(characterID int64) int64) (domain.GuildSnapshot, domain.GuildRefusal) {
	var refused domain.GuildSnapshot
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, actorIndex, _, refusal := door.authorizedGuildActorLocked(divisionID, actorID, domain.GuildAuthorization{})
	if refusal.Refused() {
		return refused, refusal
	}
	if members[actorIndex].Grade == 0 {
		return refused, domain.GuildRefusalPermissionDenied
	}
	if guild.Vote != nil {
		return refused, domain.GuildRefusalVoteOpen
	}
	vote := &domain.GuildVote{ID: uint32(nowMs / 1000), Kind: domain.GuildVoteMasterRelease,
		EndsAtUnixMs: nowMs + domain.GuildVoteDurationMs, Ballots: map[uint32]uint8{}}
	for _, member := range members {
		last := seen(member.CharID)
		if member.Grade == 0 {
			if last == 0 || nowMs-last < domain.GuildMasterAbsenceMs {
				return refused, domain.GuildRefusalVoteNotTime
			}
			continue
		}
		if last != 0 && nowMs-last < domain.GuildVoterAbsenceMs {
			vote.Candidates = append(vote.Candidates, member.JID)
		}
	}
	guild.Vote = vote
	s.guilds[divisionID][guildID] = guild
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.commitLocked(fmt.Sprintf("guild-vote-open %s/%d", divisionID, guildID))
	return domain.GuildSnapshot{Guild: guild, Members: members}, domain.GuildRefusalNone
}

/*
================
CastGuildBallotAs

VoteMgr_CastBallot (5E7EA0) and Vote_MoveBallot (5E7A70): the vote must be
open (0x5006) and the option a candidate (0x5008); a second ballot moves.
================
*/
func (door storeGuildDoor) CastGuildBallotAs(divisionID string, actorID int64, voteID uint32, option uint8) (domain.GuildVoteBallot, domain.GuildRefusal) {
	var refused domain.GuildVoteBallot
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, actorIndex, _, refusal := door.authorizedGuildActorLocked(divisionID, actorID, domain.GuildAuthorization{})
	if refusal.Refused() {
		return refused, refusal
	}
	if guild.Vote == nil || guild.Vote.ID != voteID {
		return refused, domain.GuildRefusalNoVote
	}
	if int(option) >= len(guild.Vote.Candidates) {
		return refused, domain.GuildRefusalNotCandidate
	}
	vote := guild.Vote.Clone()
	voter := members[actorIndex].JID
	previous, voted := vote.Ballots[voter]
	if !voted {
		previous = domain.GuildVoteNoOption
	}
	vote.Ballots[voter] = option
	guild.Vote = vote
	s.guilds[divisionID][guildID] = guild
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.commitLocked(fmt.Sprintf("guild-vote-ballot %s/%d", divisionID, guildID))
	return domain.GuildVoteBallot{Snapshot: domain.GuildSnapshot{Guild: guild, Members: members},
		VoteID: voteID, Previous: previous, Option: option, Count: vote.Count(option)}, domain.GuildRefusalNone
}

/*
================
CloseDueGuildVotes

Every vote past its end closes; a winner takes grade 0 with the master's
permissions and the master falls to formerGrade / formerPerm, in the same
commit as the close.
================
*/
func (door storeGuildDoor) CloseDueGuildVotes(divisionID string, nowMs int64, formerGrade uint8, formerPerm uint32) []domain.GuildVoteOutcome {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := make([]int64, 0)
	for guildID, guild := range s.guilds[divisionID] {
		if guild.Vote != nil && guild.Vote.EndsAtUnixMs <= nowMs {
			ids = append(ids, guildID)
		}
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	var outcomes []domain.GuildVoteOutcome
	for _, guildID := range ids {
		guild := s.guilds[divisionID][guildID]
		vote := guild.Vote
		guild.Vote = nil
		members, coherent := door.guildMemberViewsLocked(divisionID, s.guildMembers[divisionID][guildID])
		if !coherent {
			continue
		}
		outcome := domain.GuildVoteOutcome{VoteID: vote.ID}
		if heir, elected := vote.Winner(); elected {
			next := append([]domain.GuildMemberRecord(nil), members...)
			from, to := -1, -1
			for index, member := range next {
				if member.Grade == 0 {
					from = index
				}
				if member.JID == heir {
					to = index
				}
			}
			if from >= 0 && to >= 0 && from != to {
				next[to].Grade, next[to].PermMask = 0, next[from].PermMask
				next[from].Grade, next[from].PermMask = formerGrade, formerPerm
				members = next
				s.guildMembers[divisionID][guildID] = guildMembersForStorage(next)
				outcome.Elected, outcome.Heir, outcome.Former = true, next[to], next[from]
			}
		}
		s.guilds[divisionID][guildID] = guild
		s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
		outcome.Snapshot = domain.GuildSnapshot{Guild: guild, Members: members}
		outcomes = append(outcomes, outcome)
	}
	if len(outcomes) != 0 {
		s.commitLocked(fmt.Sprintf("guild-vote-close %s", divisionID))
	}
	return outcomes
}

/*
================
TransactGuildStorageAs

The guild warehouse transfer (v1.188 guild storage jobs): the acting
member and their guild's warehouse change together in one commit. The
capacity is the guild level's (GuildStorageCapacity); rows the level no
longer covers stay stored but out of reach.
================
*/
func (door storeGuildDoor) TransactGuildStorageAs(divisionID string, actorID int64, mutate func(next *domain.Character, storage *domain.AccountStorage) error) (domain.AccountStorage, domain.GuildRefusal, error) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, _, _, actor, refusal := door.authorizedGuildActorLocked(divisionID, actorID, domain.GuildAuthorization{})
	if refusal.Refused() {
		return domain.AccountStorage{}, refusal, nil
	}
	current := domain.AccountStorage{}
	if guild.Storage != nil {
		current = *guild.Storage
	}
	current.Capacity = domain.GuildStorageCapacity(guild.Level)
	storage := current
	storage.Rows = append([]domain.InventoryRow(nil), current.Rows...)
	for i := range storage.Rows {
		storage.Rows[i].Summon = domain.CloneCOS(current.Rows[i].Summon)
		storage.Rows[i].MagicOptions = append([]uint64(nil), current.Rows[i].MagicOptions...)
	}
	if mutate == nil {
		return current, domain.GuildRefusalNone, nil
	}
	next := actor.Snapshot()
	if err := mutate(next, &storage); err != nil {
		return current, domain.GuildRefusalNone, err
	}
	if err := validateAccountStorage(storage); err != nil {
		return current, domain.GuildRefusalNone, err
	}
	actor.MissionInventory = next.MissionInventory
	actor.Gold = next.Gold
	stored := storage
	guild.Storage = &stored
	s.guilds[divisionID][guildID] = guild
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.changes.characters[actor] = true
	s.commitLocked(fmt.Sprintf("guild-storage %s/%d", divisionID, guildID))
	return storage, domain.GuildRefusalNone, nil
}
