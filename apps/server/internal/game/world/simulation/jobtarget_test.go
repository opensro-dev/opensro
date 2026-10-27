/*
===========================================================================

jobtarget_test.go - a job monster passes over its own side (JobTargetRefused)

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestThiefMonsterAcquiresTheTraderNotTheNearerThief

Ordinary acquisition skips a target the attack admission would refuse: a
thief monster takes the farther trader over a nearer thief, and with only
the thief in sight acquires nobody. The thief's companion (owner job) is
skipped with it; a hunter monster does take the thief. An ordinary player
(state 4, no suit) is refused by both, so a job monster with only ordinary
players in sight acquires nobody.
================
*/
func TestThiefMonsterAcquiresTheTraderNotTheNearerThief(t *testing.T) {
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 2
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	thief := playerPose{Gid: 7, JobState: jobStateThief, Pose: Spawn{RegionID: monsterTestRegion, X: 1005, Z: 1000}}
	thiefPet := playerPose{Gid: 70, OwnerGid: 7, Pose: Spawn{RegionID: monsterTestRegion, X: 1002, Z: 1000}}
	trader := playerPose{Gid: 8, JobState: jobStateTrader, Pose: Spawn{RegionID: monsterTestRegion, X: 1020, Z: 1000}}
	if got, ok := ordinaryPlayerAcquisition(actor, from, []playerPose{thief, thiefPet, trader}, 100); !ok || got.Gid != 8 {
		t.Fatalf("thief monster acquired %d (found %v), want the trader 8", got.Gid, ok)
	}
	if got, ok := ordinaryPlayerAcquisition(actor, from, []playerPose{thief, thiefPet}, 100); ok {
		t.Fatalf("thief monster acquired %d from its own side", got.Gid)
	}
	ordinary := playerPose{Gid: 9, JobState: jobStateSpecial, Pose: Spawn{RegionID: monsterTestRegion, X: 1003, Z: 1000}}
	if got, ok := ordinaryPlayerAcquisition(actor, from, []playerPose{ordinary}, 100); ok {
		t.Fatalf("thief monster acquired ordinary player %d", got.Gid)
	}
	actor.Ref.TypeID4 = 3
	if got, ok := ordinaryPlayerAcquisition(actor, from, []playerPose{thief, thiefPet, trader}, 100); !ok || got.Gid == 8 {
		t.Fatalf("hunter monster acquired %d (found %v), want the thief or its pet", got.Gid, ok)
	}
	if got, ok := ordinaryPlayerAcquisition(actor, from, []playerPose{ordinary, trader}, 100); ok {
		t.Fatalf("hunter monster acquired %d from ordinary players and traders", got.Gid)
	}
	actor.Ref.TypeID4 = 1
	if got, ok := ordinaryPlayerAcquisition(actor, from, []playerPose{ordinary}, 100); !ok || got.Gid != 9 {
		t.Fatalf("an ordinary monster skipped the ordinary player: %d %v", got.Gid, ok)
	}
}

/*
================
TestJobMonsterAreaSparesItsOwnSide

The retail server's eligibility test for a job monster: an area strike
skips its own side and every ordinary player (state 4), a companion by
its owner's job.
================
*/
func TestJobMonsterAreaSparesItsOwnSide(t *testing.T) {
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 3
	for _, c := range []struct {
		candidate MonsterAreaCandidate
		want      bool
	}{
		{MonsterAreaCandidate{Gid: 7, JobState: jobStateTrader}, false},
		{MonsterAreaCandidate{Gid: 70, OwnerGid: 7, JobState: jobStateTrader}, false},
		{MonsterAreaCandidate{Gid: 8, JobState: jobStateThief}, true},
		{MonsterAreaCandidate{Gid: 9, JobState: jobStateSpecial}, false},
		{MonsterAreaCandidate{Gid: 90, OwnerGid: 9, JobState: jobStateSpecial}, false},
	} {
		if got := MonsterAreaStrikes(actor, c.candidate); got != c.want {
			t.Errorf("hunter monster area on %+v: %v, want %v", c.candidate, got, c.want)
		}
	}
}

/*
================
TestMonsterAreaStrikesIgnoresFirstAttackGuard

The eligibility test calls vtable +0x58C with checkFirstAttack=0, so the
monster hostility body accepts before its first-attack protection (Noise)
check, while still enforcing the hostility status check (Fear source, grab
pet, fellow band, body status).
================
*/
func TestMonsterAreaStrikesIgnoresFirstAttackGuard(t *testing.T) {
	actor := monster.Instance{Abnormal: &abnormal.Block{Mask: abnormal.Fear.Bit()}}
	actor.Ref.TidWord, actor.Ref.TypeID4, actor.Ref.Level = 0x00c6, 1, 10
	actor.Abnormal.Slots[abnormal.Fear].Active = true
	actor.Abnormal.Slots[abnormal.Fear].SourceGID = 99
	guard := monster.FirstAttackGuard{Mask: 1, Level: 100}
	for _, c := range []struct {
		name      string
		candidate MonsterAreaCandidate
		want      bool
	}{
		{"noise-protected player", MonsterAreaCandidate{Gid: 7, Guard: guard}, true},
		{"noise-protected companion", MonsterAreaCandidate{Gid: 70, OwnerGid: 7, Band: 3, Guard: guard}, true},
		{"fear source excluded", MonsterAreaCandidate{Gid: 99, Guard: guard}, false},
		{"grab pet excluded", MonsterAreaCandidate{Gid: 71, OwnerGid: 7, Band: 4, Guard: guard}, false},
		{"fellow band excluded", MonsterAreaCandidate{Gid: 72, OwnerGid: 7, Band: fellowCOSBand, Guard: guard}, false},
		{"untargetable status excluded", MonsterAreaCandidate{Gid: 8, NativeBodyStatus: 3, Guard: guard}, false},
	} {
		if got := MonsterAreaStrikes(actor, c.candidate); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

/*
================
TestSessionJobStateIsNative

The monster tick builds each player from its session: no appearance or no
worn suit is job state 4, a suit is its job.
================
*/
func TestSessionJobStateIsNative(t *testing.T) {
	for _, c := range []struct {
		appearance *PeerAppearance
		want       uint8
	}{
		{nil, jobStateSpecial},
		{&PeerAppearance{}, jobStateSpecial},
		{&PeerAppearance{JobType: jobStateThief}, jobStateThief},
		{&PeerAppearance{JobType: jobStateTrader}, jobStateTrader},
	} {
		got := sessionPlayerPose(SessionSnapshot{CharacterID: 5, Appearance: c.appearance}, 0)
		if got.JobState != c.want {
			t.Errorf("appearance %+v: job state %d, want %d", c.appearance, got.JobState, c.want)
		}
	}
}
