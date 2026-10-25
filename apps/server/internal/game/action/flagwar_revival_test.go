/*
===========================================================================

flagwar_revival_test.go - the flag war camp revive (#1558)

A killed participant revives at their team spawn and stays in the
match, on both teams, whichever rebirth choice they send; a killed
carrier's flag stays dropped where they fell.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
flagWarKill

Lays a participant's corpse on a spot inside the arena.
================
*/
func flagWarKill(rig *flagWarMatchRuntime, c *enterworld.Character, pose simulation.Spawn) {
	rig.movePlayer(c, pose)
	zero := int64(0)
	c.CurrentHP = &zero
	c.DeathKind = uint8(pk.DeathSpecialWorld)
}

/*
================
flagWarRevive

Sends one local-rebirth choice for a corpse and answers the settled
world spawn afterwards.
================
*/
func flagWarRevive(t *testing.T, rig *flagWarMatchRuntime, c *enterworld.Character, choice byte) simulation.Spawn {
	t.Helper()
	result := rig.rt.HandleLocalRebirth(testDivision, c, []byte{choice})
	if len(result.Frames) == 0 {
		t.Fatalf("%s did not revive on choice %d", c.Name, choice)
	}
	if !enterworld.CharacterAlive(c) {
		t.Fatalf("%s is still dead after the revive", c.Name)
	}
	return rig.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) }).Spawn
}

/*
================
TestFlagWarCampReviveKeepsTheParticipant
================
*/
func TestFlagWarCampReviveKeepsTheParticipant(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// One corpse per team, mid-arena, both sending the town choice
	// that used to eject them from the match.
	midArena := simulation.Spawn{RegionID: flagWarArenaRegionID, X: 1000, Y: 0, Z: -100}
	cases := []struct {
		player *enterworld.Character
		team   int
		choice byte
	}{
		{players[0], 0, 1}, // town choice
		{players[1], 1, 2}, // present-point choice
	}
	for _, tc := range cases {
		flagWarKill(rig, tc.player, midArena)
		spawn := flagWarRevive(t, rig, tc.player, tc.choice)
		if spawn != flagWarTeamSpawns[tc.team] {
			t.Fatalf("%s revived at %+v, want team %d spawn %+v",
				tc.player.Name, spawn, tc.team, flagWarTeamSpawns[tc.team])
		}
		if got := instance.ID(domain.CharacterWorldInstance(tc.player)); got != match.lease.ID {
			t.Fatalf("%s left the arena world on revive (world %v)", tc.player.Name, got)
		}
		if p := match.participants[tc.player.ID]; p == nil || !p.entered {
			t.Fatalf("%s was ejected from the match by the revive", tc.player.Name)
		}
	}
	// The reconcile agrees: nobody departed.
	rig.rt.advanceFlagWarMatches(match.startMs + 2000)
	for _, tc := range cases {
		if p := match.participants[tc.player.ID]; p == nil || !p.entered {
			t.Fatalf("%s was reconciled out after the camp revive", tc.player.Name)
		}
	}

	// The lookup itself refuses a body that already left the arena.
	stranger := players[3]
	if _, ok := rig.rt.flagWarRevivalPoint(testDivision, stranger); !ok {
		t.Fatal("an entered participant lost the camp revive")
	}
	if status := rig.rt.ReturnCharacterFromInstance(testDivision, stranger); status != instance.Success {
		t.Fatalf("return from the match world: status %d", status)
	}
	if _, ok := rig.rt.flagWarRevivalPoint(testDivision, stranger); ok {
		t.Fatal("a returned participant still revives at the camp")
	}
}

/*
================
TestFlagWarCampReviveOfTheCarrier
================
*/
func TestFlagWarCampReviveOfTheCarrier(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// Charly (side 0) steals the centre pole and carries.
	charly := players[2]
	rig.movePlayer(charly, flagWarPoleSpawns[0])
	rig.selectObject(t, charly, flagWarObjectGID(flagWarSlotPole0))
	rig.rt.advanceFlagWarMatches(match.startMs + 6000)
	if match.carrierID != charly.ID {
		t.Fatal("Charly never carried")
	}

	// Bravo kills the carrier beside the centre pole: the flag drops
	// where Charly fell, and the revive must not pick it back up.
	deathPose := simulation.Spawn{RegionID: flagWarArenaRegionID, X: 1100, Y: 0, Z: -200}
	rig.movePlayer(charly, deathPose)
	rig.rt.recordFlagWarDeath(testDivision, charly, players[1], match.startMs+7000)
	zero := int64(0)
	charly.CurrentHP = &zero
	charly.DeathKind = uint8(pk.DeathSpecialWorld)
	if match.carrierID != 0 || !match.flagLive {
		t.Fatal("the carrier's death did not drop the flag")
	}
	spawn := flagWarRevive(t, rig, charly, 1)
	if spawn != flagWarTeamSpawns[0] {
		t.Fatalf("the carrier revived at %+v, want team 0 spawn %+v", spawn, flagWarTeamSpawns[0])
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 8000)
	if p := match.participants[charly.ID]; p == nil || !p.entered {
		t.Fatal("the carrier was ejected from the match by the revive")
	}
	if match.carrierID != 0 || !match.flagLive {
		t.Fatal("the revive disturbed the dropped flag")
	}
}
