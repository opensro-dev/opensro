/*
===========================================================================

mostwanted_test.go - the job-kill streak, its bonuses and bounty

Port-only, not native: every rule is checked with SRO_MOST_WANTED off
(nothing counts, no bonus, no bounty, a saved streak untouched) and on.

===========================================================================
*/
package action

import (
	"math"
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
	dressThief(c)
	if rt.mostWantedWrites(c) != nil || rt.mostWantedSpeedFactor(c) != 1 {
		t.Fatal("flag off applied a bonus")
	}
	rt.MostWanted = true
	unsuited := *c
	unsuited.MissionInventory = nil
	if rt.mostWantedWrites(&unsuited) != nil || rt.mostWantedSpeedFactor(&unsuited) != 1 {
		t.Fatal("an unsuited player was buffed")
	}
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
	actor.MostWanted = &domain.MostWanted{Streak: 30}
	dressForJob(rt, actor, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
	native, err := rt.PlayerBaseStats(testDivision, actor)
	if err != nil {
		t.Fatal(err)
	}
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

The killer's own account, party and guild are never strangers; a stranger is.
================
*/
func TestMostWantedEligibility(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	killer.AccountID, victim.AccountID = "a", "b"
	if rt.mostWantedStrangers(testDivision, killer, victim) {
		t.Fatal("flag off counted")
	}
	rt.MostWanted = true
	if !rt.mostWantedStrangers(testDivision, killer, victim) {
		t.Fatal("a stranger did not count")
	}
	victim.AccountID = "a"
	if rt.mostWantedStrangers(testDivision, killer, victim) {
		t.Fatal("the same account counted")
	}
	victim.AccountID = "b"
	guild := int64(7)
	killer.GuildID, victim.GuildID = &guild, &guild
	if rt.mostWantedStrangers(testDivision, killer, victim) {
		t.Fatal("the same guild counted")
	}
	victim.GuildID = nil
	gids := []uint32{enterworld.ObjectIDForCharacter(killer), enterworld.ObjectIDForCharacter(victim)}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: gids}} }
	if rt.mostWantedStrangers(testDivision, killer, victim) {
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
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
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
		// A related victim (alt, partymate, guildmate) neither counts nor pays.
		before, paid := killer.MostWanted.Streak, goldOf(killer)
		kill.mostWantedStrangers = false
		rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1)
		if killer.MostWanted.Streak != before || goldOf(killer) != paid {
			t.Fatalf("a related kill counted or paid: streak %d gold %d", killer.MostWanted.Streak-before, goldOf(killer)-paid)
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

/*
================
TestMostWantedBountyIsTakenFromTheVictim

With the transfer on, the kill's door moves no gold and owes the bounty;
paying it takes min(bounty, the victim's gold) from the victim, never
passes the killer's cap, and keeps the pair's total. A victim with
nothing pays nothing and the door writes nothing.
================
*/
func TestMostWantedBountyIsTakenFromTheVictim(t *testing.T) {
	const bounty = 2 * mostWantedBountyPerLevel
	for _, tc := range []struct {
		name               string
		killerGold, victim int64
		paid               int64
	}{
		{"carries more", 1_000, 500_000_000, bounty},
		{"carries less", 1_000, 50_000_000, 50_000_000},
		{"carries nothing", 1_000, 0, 0},
		{"killer at the cap", math.MaxInt64 - 10, 500_000_000, 10},
	} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted, rt.MostWantedBountyTransfer = true, true
		killer.Gold, victim.Gold = testInt64(tc.killerGold), testInt64(tc.victim)
		victim.MostWanted = &domain.MostWanted{Streak: 25}
		kill := playerKill{kind: pk.DeathPlayer, mostWantedStrangers: true}.withVictim(killer, victim)
		_, _, after := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0)
		if *killer.Gold != tc.killerGold || *victim.Gold != tc.victim || len(after.bounties) != 1 {
			t.Fatalf("%s: the kill's door moved gold or owed %+v", tc.name, after.bounties)
		}
		recipients := rt.payJobKillShares(after)
		if *killer.Gold-tc.killerGold != tc.paid || tc.victim-*victim.Gold != tc.paid {
			t.Fatalf("%s: killer +%d victim -%d, want %d", tc.name, *killer.Gold-tc.killerGold, tc.victim-*victim.Gold, tc.paid)
		}
		if want := 2; tc.paid == 0 && len(recipients) != 0 || tc.paid != 0 && len(recipients) != want {
			t.Fatalf("%s: recipients %+v", tc.name, recipients)
		}
	}
}

/*
================
TestMostWantedBountyTransferNeedsAStranger

A related victim owes nothing with the transfer on, as it is paid nothing
with it off.
================
*/
func TestMostWantedBountyTransferNeedsAStranger(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted, rt.MostWantedBountyTransfer = true, true
	victim.MostWanted = &domain.MostWanted{Streak: 25}
	kill := playerKill{kind: pk.DeathPlayer}.withVictim(killer, victim)
	if _, _, after := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0); len(after.bounties) != 0 {
		t.Fatalf("a related kill owed %+v", after.bounties)
	}
}

/*
================
TestKillingAWantedPlayerPaysTheBountyFromTheirGold

End to end through a fatal basic attack: the wanted victim's gold pays
the killer, and the two balances keep their sum.
================
*/
func TestKillingAWantedPlayerPaysTheBountyFromTheirGold(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted, rt.MostWantedBountyTransfer = true, true
	killer.AccountID, victim.AccountID = "a", "b"
	killer.Gold, victim.Gold = testInt64(1_000), testInt64(300_000_000)
	victim.MostWanted = &domain.MostWanted{Streak: 25}
	victim.CurrentHP = testInt64(1)
	rt.HandleTargetInteract(testDivision, killer, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(victim)}.Encode())
	if enterworld.CharacterAlive(victim) {
		t.Fatal("victim survived a one-HP hit")
	}
	if *killer.Gold != 1_000+2*mostWantedBountyPerLevel || *victim.Gold != 300_000_000-2*mostWantedBountyPerLevel {
		t.Fatalf("killer %d victim %d", *killer.Gold, *victim.Gold)
	}
}
