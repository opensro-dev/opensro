/*
===========================================================================

mostwantedtest_test.go - Most Wanted's test pacing, both settings

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/pk"
)

/*
================
TestMostWantedTestFromEnv
================
*/
func TestMostWantedTestFromEnv(t *testing.T) {
	for env, want := range map[string]bool{"": false, "off": false, "on": true, "1": true} {
		t.Setenv(EnvMostWantedTest, env)
		if got := MostWantedTestFromEnv(); got != want {
			t.Errorf("%q: %v", env, got)
		}
	}
}

/*
================
TestMostWantedTestPacingCountsTheSameVictimEveryTime

Test pacing: 5 kills of one victim make a wanted level and 50 make level
10, with no anti-farm window. The ordinary rule counts that victim only 3
times an hour, so the same 50 kills leave the killer unwanted.
================
*/
func TestMostWantedTestPacingCountsTheSameVictimEveryTime(t *testing.T) {
	for _, test := range []bool{false, true} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted, rt.MostWantedTest = true, test
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}
		levelAfter := map[int]int64{}
		for n := 1; n <= 50; n++ {
			rt.payPlayerKillInDoor(testDivision, killer, victim, kill.withVictim(killer, victim), int64(n))
			levelAfter[n] = mostWantedLevel(killer)
		}
		if !test {
			if levelAfter[50] != 0 || killer.MostWanted.Streak != mostWantedPairLimit {
				t.Fatalf("ordinary rule: streak %d level %d after 50 kills of one victim, want streak %d level 0",
					killer.MostWanted.Streak, levelAfter[50], mostWantedPairLimit)
			}
			continue
		}
		if levelAfter[4] != 0 || levelAfter[5] != 1 || levelAfter[25] != 5 || levelAfter[50] != mostWantedMaxLevel {
			t.Fatalf("test pacing levels: 4->%d 5->%d 25->%d 50->%d, want 0, 1, 5, %d",
				levelAfter[4], levelAfter[5], levelAfter[25], levelAfter[50], mostWantedMaxLevel)
		}
	}
}

/*
================
TestMostWantedTestPacingPaysASmallBounty

Killing a level-2 wanted player pays 2,000,000 under the test pacing and
200,000,000 under the ordinary rule (the minted bounty, transfer off).
================
*/
func TestMostWantedTestPacingPaysASmallBounty(t *testing.T) {
	for _, test := range []bool{false, true} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted, rt.MostWantedTest = true, test
		victim.MostWanted = &domain.MostWanted{Streak: 25}
		gold := goldOf(killer)
		kill := playerKill{kind: pk.DeathPlayer, mostWantedStrangers: true}.withVictim(killer, victim)
		rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0)
		want := uint64(200_000_000)
		if test {
			want = 2_000_000
		}
		if got := goldOf(killer) - gold; got != want {
			t.Fatalf("test pacing %v: bounty %d, want %d", test, got, want)
		}
		if test && rt.mostWantedBountyGold(mostWantedMaxLevel) != 10_000_000 {
			t.Fatal("test pacing: level 10 is not worth 10,000,000")
		}
	}
}
