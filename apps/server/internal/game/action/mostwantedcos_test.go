/*
===========================================================================

mostwantedcos_test.go - companion-delivered job kills and Most Wanted

Port-only, not native: a job kill delivered on a companion counts for
Most Wanted only with SRO_MOST_WANTED_COS_KILLS on - the killer the COS
door books (the companion's owner when a companion dealt the blow) takes
the streak and any bounty. Off, the kill pays job EXP and nothing else,
as it always has. The pair limit binds companion kills exactly like
player kills.

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
TestMostWantedCOSKillsFromEnv
================
*/
func TestMostWantedCOSKillsFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvMostWantedCOSKills, value)
		if got := MostWantedCOSKillsFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}

/*
================
TestMostWantedCOSKillCountsForTheKiller

Through payCOSKillInDoor with SRO_MOST_WANTED on: a stranger job kill
delivered on a companion raises the booked killer's streak and pays a
wanted owner's bounty only with SRO_MOST_WANTED_COS_KILLS on; with the
companion flag off, the same kill counts nothing and pays no bounty.
================
*/
func TestMostWantedCOSKillCountsForTheKiller(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, killer, owner := newPvpPair(t)
		rt.MostWanted, rt.MostWantedCOSKills = true, on
		owner.MostWanted = &domain.MostWanted{Streak: 25}
		gold := goldOf(killer)
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, owner)
		_, _, after, _ := rt.payCOSKillInDoor(testDivision, killer, owner, kill, 5, 0)
		if !on {
			if killer.MostWanted != nil || goldOf(killer) != gold || len(after.wanted) != 0 || len(after.bounties) != 0 {
				t.Fatalf("off: streak %+v gold %+d payout %+v", killer.MostWanted, goldOf(killer)-gold, after)
			}
			continue
		}
		if killer.MostWanted == nil || killer.MostWanted.Streak != 1 {
			t.Fatalf("on: streak %+v, want 1", killer.MostWanted)
		}
		if goldOf(killer)-gold != 2*mostWantedBountyPerLevel {
			t.Fatalf("on: bounty %d, want %d", goldOf(killer)-gold, 2*mostWantedBountyPerLevel)
		}
	}
}

/*
================
TestMostWantedCOSKillPairLimit

A killer counts the same companion's owner at most 3 times in an hour
through the COS door, as through the player door.
================
*/
func TestMostWantedCOSKillPairLimit(t *testing.T) {
	rt, _, killer, owner := newPvpPair(t)
	rt.MostWanted, rt.MostWantedCOSKills = true, true
	kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, owner)
	for i := int64(0); i < 4; i++ {
		rt.payCOSKillInDoor(testDivision, killer, owner, kill, 5, i)
	}
	if killer.MostWanted == nil || killer.MostWanted.Streak != mostWantedPairLimit {
		t.Fatalf("streak %+v, want %d after four kills of one pair", killer.MostWanted, mostWantedPairLimit)
	}
}
