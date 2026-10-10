/*
===========================================================================

guildvote_test.go - the master release vote through the guild door

===========================================================================
*/

package store

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestReleaseVoteElectsTheLeadingCandidate

The master left 46 days ago; both members were seen today. The member
the other votes for leads, cannot leave while the vote runs, and becomes
master at the close.
================
*/
func TestReleaseVoteElectsTheLeadingCandidate(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	leader := guildTestCharacter("absentmaster")
	member := guildTestCharacter("voter")
	for _, character := range []*enterworld.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	guildID, _, members := seedTestGuild(t, s, leader, member)
	const day = int64(24 * 60 * 60 * 1000)
	now := 100 * day
	seen := map[int64]int64{leader.ID: now - day}
	lastSeen := func(id int64) int64 {
		if at, ok := seen[id]; ok {
			return at
		}
		return now
	}
	if _, refusal := s.Guilds().OpenMasterReleaseVoteAs(testDivision, member.ID, now, lastSeen); refusal != domain.GuildRefusalVoteNotTime {
		t.Fatalf("a recent master was released: %v", refusal)
	}
	seen[leader.ID] = now - 46*day
	snapshot, refusal := s.Guilds().OpenMasterReleaseVoteAs(testDivision, member.ID, now, lastSeen)
	if refusal.Refused() || len(snapshot.Guild.Vote.Candidates) != 1 || snapshot.Guild.Vote.Candidates[0] != members[1].JID {
		t.Fatalf("opened vote %+v/%v", snapshot.Guild.Vote, refusal)
	}
	vote := snapshot.Guild.Vote
	if _, refusal := s.Guilds().OpenMasterReleaseVoteAs(testDivision, member.ID, now, lastSeen); refusal != domain.GuildRefusalVoteOpen {
		t.Fatalf("a second vote opened: %v", refusal)
	}
	if _, refusal := s.Guilds().CastGuildBallotAs(testDivision, member.ID, vote.ID, 3); refusal != domain.GuildRefusalNotCandidate {
		t.Fatalf("a ballot for no candidate: %v", refusal)
	}
	ballot, refusal := s.Guilds().CastGuildBallotAs(testDivision, member.ID, vote.ID, 0)
	if refusal.Refused() || ballot.Previous != domain.GuildVoteNoOption || ballot.Count != 1 {
		t.Fatalf("ballot %+v/%v", ballot, refusal)
	}
	if _, refusal := s.Guilds().LeaveGuild(testDivision, member.ID); refusal != domain.GuildRefusalVoteInProgress {
		t.Fatalf("a candidate left during the vote: %v", refusal)
	}
	if outcomes := s.Guilds().CloseDueGuildVotes(testDivision, now+domain.GuildVoteDurationMs-1, 10, 0); len(outcomes) != 0 {
		t.Fatal("the vote closed early")
	}
	outcomes := s.Guilds().CloseDueGuildVotes(testDivision, now+domain.GuildVoteDurationMs, 10, 0)
	if len(outcomes) != 1 || !outcomes[0].Elected || outcomes[0].Heir.CharID != member.ID || outcomes[0].Heir.Grade != 0 {
		t.Fatalf("outcomes %+v", outcomes)
	}
	stored, roster, _ := s.Guilds().Guild(testDivision, guildID)
	if stored.Vote != nil || roster[0].Grade != 10 || roster[1].Grade != 0 {
		t.Fatalf("guild after the vote %+v %+v", stored, roster)
	}
	// The commander role moves with the master (5C46E0).
	if roster[0].FortressRole != 0 || roster[1].FortressRole != domain.GuildFortressRoleCommander {
		t.Fatalf("roles after the vote %d %d, want 0 and the commander's", roster[0].FortressRole, roster[1].FortressRole)
	}
}
