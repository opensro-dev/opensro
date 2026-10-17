/*
===========================================================================

siege_rebirth_test.go - the siege corpse clock and the rebirth refusal

A siege death starts the corpse clock (CGObjPC+0x1CD4: 180 ticks from
ProcessSiegeDeath 52CFBA), and while the clock still reads above 165 -
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

A fortress death records the special-world kind and stamps the corpse
clock at the death instant; an ordinary player death stamps nothing.
================
*/
func TestSiegeDeathStartsTheCorpseClock(t *testing.T) {
	rt, clock, killer, victim, _ := fortressBattlePair(t)
	now := clock.NowMs()

	rt.settlePlayerDeathInDoor(testDivision, victim, deathKiller{player: killer}, now)
	if victim.DeathKind == uint8(pk.DeathSpecialWorld) || victim.SiegeDeathAtMs != 0 {
		t.Fatalf("ordinary kill: kind %d stamp %d, want no siege clock", victim.DeathKind, victim.SiegeDeathAtMs)
	}

	rt.settlePlayerDeathInDoor(testDivision, victim,
		rt.prepareDeathKiller(testDivision, victim, deathKiller{player: killer}), now)
	if victim.DeathKind != uint8(pk.DeathSpecialWorld) {
		t.Fatalf("fortress kill: kind %d, want the special-world kind", victim.DeathKind)
	}
	if victim.SiegeDeathAtMs != now {
		t.Fatalf("fortress kill: corpse clock %d, want the death instant %d", victim.SiegeDeathAtMs, now)
	}
}

/*
================
siegeCorpse

A level-80 team-death corpse lying in the Jangan fortress world with
its siege clock freshly stamped, inside a war-active division.
================
*/
func siegeCorpse(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character) {
	t.Helper()
	c := rebirthTestCharacter(80, 0)
	c.DeathKind = uint8(pk.DeathTeam)
	rt, clock := newTestRuntime(c, testItems())
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	packed := uint32(instance.Pack(2, 1))
	c.World.PackedInstance = &packed
	c.SiegeDeathAtMs = clock.NowMs()
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
	c := rebirthTestCharacter(80, 0)
	c.DeathKind = uint8(pk.DeathTeam)
	rt, clock := newTestRuntime(c, testItems())
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: 1}})
	packed := uint32(instance.Pack(2, 1))
	c.World.PackedInstance = &packed
	c.SiegeDeathAtMs = clock.NowMs()
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
