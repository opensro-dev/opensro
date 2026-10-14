/*
===========================================================================

mostwanted_test.go - the job-kill streak, its bonuses and bounty

Port-only, not native: every rule is checked with SRO_MOST_WANTED off
(nothing counts, no bonus, no bounty, a saved streak untouched) and on.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
)

/*
================
TestMostWantedLevelAndBonuses
================
*/
func TestMostWantedLevelAndBonuses(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	c := testCharacter()
	for streak, want := range map[int64]int64{0: 0, 9: 0, 10: 1, 29: 2, 100: 10, 250: 10} {
		c.MostWanted = &domain.MostWanted{Streak: streak}
		if got := mostWantedLevel(c); got != want {
			t.Fatalf("streak %d: level %d, want %d", streak, got, want)
		}
	}
	for level, want := range map[int64]float32{0: 0, 2: 0, 3: 1, 4: 1, 5: 2, 9: 2, 10: 3} {
		if got := mostWantedDamagePercent(level); got != want {
			t.Fatalf("level %d: %v%%, want %v%%", level, got, want)
		}
	}
	c.MostWanted = &domain.MostWanted{Streak: 50}
	if rt.mostWantedWrites(c) != nil || rt.mostWantedSpeedFactor(c) != 1 {
		t.Fatal("flag off applied a bonus")
	}
	rt.MostWanted = true
	writes := rt.mostWantedWrites(c)
	// STR, INT, four damage-dealt rates and four incoming reductions.
	if len(writes) != 10 || writes[0].Parameter != 1 || writes[0].Value != 5 || writes[1].Parameter != 2 || writes[2].Value != 2 {
		t.Fatalf("writes %+v", writes)
	}
	if got := rt.mostWantedSpeedFactor(c); got != 1.05 {
		t.Fatalf("speed factor %v", got)
	}
}

/*
================
TestMostWantedRaisesTheProjectedStrength

The streak's writes reach the stat block the client is sent.
================
*/
func TestMostWantedRaisesTheProjectedStrength(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	native, err := rt.PlayerBaseStats(testDivision, actor)
	if err != nil {
		t.Fatal(err)
	}
	actor.MostWanted = &domain.MostWanted{Streak: 30}
	if off, _ := rt.PlayerBaseStats(testDivision, actor); off != native {
		t.Fatal("flag off changed the stat block")
	}
	rt.MostWanted = true
	on, err := rt.PlayerBaseStats(testDivision, actor)
	if err != nil || on.StrWord != native.StrWord+3 || on.IntWord != native.IntWord+3 {
		t.Fatalf("str %d->%d int %d->%d (%v)", native.StrWord, on.StrWord, native.IntWord, on.IntWord, err)
	}
}

/*
================
TestMostWantedPairLimit

One killer counts one victim at most 3 times in an hour; the window reopens.
================
*/
func TestMostWantedPairLimit(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	for i := 0; i < 3; i++ {
		if !rt.countMostWantedPair(testDivision, "Robber", "Merchant", int64(i)) {
			t.Fatalf("kill %d refused", i)
		}
	}
	if rt.countMostWantedPair(testDivision, "Robber", "MERCHANT", 10) {
		t.Fatal("a fourth kill in the hour counted")
	}
	if !rt.countMostWantedPair(testDivision, "Robber", "Other", 10) {
		t.Fatal("another victim was refused")
	}
	if !rt.countMostWantedPair(testDivision, "Robber", "Merchant", mostWantedPairWindowMs) {
		t.Fatal("the window did not reopen")
	}
}

/*
================
TestMostWantedEligibility

The killer's own account, party and guild never count; a stranger does.
================
*/
func TestMostWantedEligibility(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	killer.AccountID, victim.AccountID = "a", "b"
	if rt.mostWantedEligible(testDivision, killer, victim) {
		t.Fatal("flag off counted")
	}
	rt.MostWanted = true
	if !rt.mostWantedEligible(testDivision, killer, victim) {
		t.Fatal("a stranger did not count")
	}
	victim.AccountID = "a"
	if rt.mostWantedEligible(testDivision, killer, victim) {
		t.Fatal("the same account counted")
	}
	victim.AccountID = "b"
	guild := int64(7)
	killer.GuildID, victim.GuildID = &guild, &guild
	if rt.mostWantedEligible(testDivision, killer, victim) {
		t.Fatal("the same guild counted")
	}
	victim.GuildID = nil
	gids := []uint32{enterworld.ObjectIDForCharacter(killer), enterworld.ObjectIDForCharacter(victim)}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: gids}} }
	if rt.mostWantedEligible(testDivision, killer, victim) {
		t.Fatal("the same party counted")
	}
}

/*
================
TestMostWantedKillCountsAndPaysTheBounty

Through payPlayerKillInDoor: a counted job kill raises the killer's streak
(level 1 at 10, with a stat refresh), and killing a level-2 wanted player
pays 200,000,000 gold. Off, neither happens.
================
*/
func TestMostWantedKillCountsAndPaysTheBounty(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted = on
		killer.MostWanted = &domain.MostWanted{Streak: 9}
		victim.MostWanted = &domain.MostWanted{Streak: 25}
		gold := goldOf(killer)
		kill := playerKill{kind: pk.DeathJob, mostWantedEligible: true}.withVictim(killer, victim)
		actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0)
		stats := false
		for _, f := range actor {
			stats = stats || f.Opcode == wire.OpBaseStats
		}
		if !on {
			if killer.MostWanted.Streak != 9 || goldOf(killer) != gold || stats {
				t.Fatalf("off: streak %d gold %d stats %v", killer.MostWanted.Streak, goldOf(killer)-gold, stats)
			}
			continue
		}
		if killer.MostWanted.Streak != 10 || mostWantedLevel(killer) != 1 || !stats {
			t.Fatalf("on: streak %d stats %v", killer.MostWanted.Streak, stats)
		}
		if goldOf(killer)-gold != 2*mostWantedBountyPerLevel {
			t.Fatalf("on: bounty %d", goldOf(killer)-gold)
		}
		// An ineligible kill pays a bounty but never counts.
		before := killer.MostWanted.Streak
		kill.mostWantedEligible = false
		rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1)
		if killer.MostWanted.Streak != before {
			t.Fatal("an ineligible kill counted")
		}
	}
}

/*
================
TestMostWantedClearsOnDeath

Any death clears a streak with the flag on and keeps it with the flag off.
================
*/
func TestMostWantedClearsOnDeath(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, actor, _ := newPvpPair(t)
		rt.MostWanted = on
		actor.MostWanted = &domain.MostWanted{Streak: 40}
		rt.settlePlayerDeathInDoor(testDivision, actor, deathKiller{}, 0)
		if on != (actor.MostWanted == nil) {
			t.Fatalf("on=%v: streak after death %+v", on, actor.MostWanted)
		}
	}
}

/*
================
TestMostWantedFromEnv
================
*/
func TestMostWantedFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvMostWanted, value)
		if got := MostWantedFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
