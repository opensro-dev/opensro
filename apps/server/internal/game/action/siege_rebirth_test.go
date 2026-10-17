/*
===========================================================================

siege_rebirth_test.go - the siege corpse clock and the rebirth refusal

Every death starts the corpse clock (CGObjPC+0x1CD4: 180 ticks from
CGObjPC::OnDeath 52CFBA), and while the clock still reads above 165 -
the first 15 ticks - a rebirth request inside an active siege world is
refused outright (50FEE0, 510086..5100D6), whatever the death kind.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestSiegeDeathStartsTheCorpseClock

CGObjPC::OnDeath (52CDB0) stamps the corpse clock on every death, so an
ordinary player kill and a team kill stamp it exactly like a fortress
kill; the fortress kill is the one that also records the special-world
kind.
================
*/
func TestSiegeDeathStartsTheCorpseClock(t *testing.T) {
	rt, clock, killer, victim, _ := fortressBattlePair(t)
	now := clock.NowMs()

	rt.settlePlayerDeathInDoor(testDivision, victim, deathKiller{player: killer}, now)
	if victim.DeathKind == uint8(pk.DeathSpecialWorld) {
		t.Fatalf("ordinary kill: kind %d, want no special-world kind", victim.DeathKind)
	}
	if victim.SiegeDeathAtMs != now {
		t.Fatalf("ordinary kill: corpse clock %d, want the death instant %d", victim.SiegeDeathAtMs, now)
	}

	rt.settlePlayerDeathInDoor(testDivision, victim,
		rt.prepareDeathKiller(testDivision, victim, deathKiller{player: killer}), now)
	if victim.DeathKind != uint8(pk.DeathSpecialWorld) {
		t.Fatalf("fortress kill: kind %d, want the special-world kind", victim.DeathKind)
	}
	if victim.SiegeDeathAtMs != now {
		t.Fatalf("fortress kill: corpse clock %d, want the death instant %d", victim.SiegeDeathAtMs, now)
	}

	// The #1151 item (a) case itself: a free-battle team death through the
	// same door stamps the clock, so the refusal can catch it in a siege.
	p := scornOpponentPair(t, shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01"))
	teamDeathAt := p.rt.Now().UnixMilli()
	p.rt.settlePlayerDeathInDoor(testDivision, p.m, deathKiller{player: p.c}, teamDeathAt)
	if p.m.DeathKind != uint8(pk.DeathTeam) {
		t.Fatalf("team kill: kind %d, want a team death", p.m.DeathKind)
	}
	if p.m.SiegeDeathAtMs != teamDeathAt {
		t.Fatalf("team kill: corpse clock %d, want the death instant %d", p.m.SiegeDeathAtMs, teamDeathAt)
	}
}

/*
================
siegeCorpse

A level-80 team-death corpse lying in the Jangan fortress world inside
a war-active division. The corpse arrives through the death door, which
stamps its clock (52CFBA); the kind byte is set to the free-battle kill
the refusal exists for.
================
*/
func siegeCorpse(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character) {
	t.Helper()
	c := rebirthTestCharacter(80, 0)
	rt, clock := newTestRuntime(c, testItems())
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	packed := uint32(instance.Pack(2, 1))
	c.World.PackedInstance = &packed
	rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, clock.NowMs())
	c.DeathKind = uint8(pk.DeathTeam)
	return rt, clock, c
}

/*
================
TestSiegeCorpseRebirthRefusedInsideTheWindow

Inside the first 15 ticks the request is refused whatever the choice;
once the clock runs down, the team death revives where it fell.
================
*/
func TestSiegeCorpseRebirthRefusedInsideTheWindow(t *testing.T) {
	rt, clock, c := siegeCorpse(t)
	for _, choice := range []byte{wire.RebirthAtPresentPoint, wire.RebirthAtSpecifiedPoint} {
		if result := rt.HandleLocalRebirth(testDivision, c, []byte{choice}); len(result.Frames) != 0 || enterworld.CharacterAlive(c) {
			t.Fatalf("choice %d: revived inside the refusal window: %+v", choice, opcodesOf(result.Frames))
		}
	}
	clock.Advance(16 * time.Second)
	result := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint})
	if !enterworld.CharacterAlive(c) || len(result.Frames) < 2 {
		t.Fatalf("after the window the team death did not revive: %q %+v", result.DiagnosticRefusal, opcodesOf(result.Frames))
	}
	if c.DeathKind != 0 {
		t.Fatalf("revival kept death kind %d", c.DeathKind)
	}
}

/*
================
TestSiegeCorpseClockNeedsTheSiegeWorldAndWar

The refusal needs the whole native conjunction: no active war, or a
corpse outside a siege world, and the team death revives at once.
================
*/
func TestSiegeCorpseClockNeedsTheSiegeWorldAndWar(t *testing.T) {
	// War over: the same fresh corpse revives immediately.
	rt, _, c := siegeCorpse(t)
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	if result := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint}); !enterworld.CharacterAlive(c) || len(result.Frames) < 2 {
		t.Fatalf("no war: team death did not revive: %q %+v", result.DiagnosticRefusal, opcodesOf(result.Frames))
	}

	// War active but the corpse lies in the overworld.
	rt2, _, c2 := siegeCorpse(t)
	c2.World.PackedInstance = nil
	if result := rt2.HandleLocalRebirth(testDivision, c2, []byte{wire.RebirthAtPresentPoint}); !enterworld.CharacterAlive(c2) || len(result.Frames) < 2 {
		t.Fatalf("overworld corpse: team death did not revive: %q %+v", result.DiagnosticRefusal, opcodesOf(result.Frames))
	}
}
