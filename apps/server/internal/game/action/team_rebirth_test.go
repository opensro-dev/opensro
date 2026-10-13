/*
===========================================================================

team_rebirth_test.go - tests for the death notice and the team-death rebirth

A death tells its victim its kind (529D6D, 0x37CE on the v1.150 client),
and a team death (free-battle capes, arena) revives where it fell at any
level whatever the choice (50FEE0, 5100D6); any other death keeps the
beginner gate on the present point.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
)

/*
================
TestCapeDeathTellsTheVictimItIsATeamDeath

A player killed by an opposing free-battle cape records kind 2 and its
private progression ends with the 0x37CE notice carrying it.
================
*/
func TestCapeDeathTellsTheVictimItIsATeamDeath(t *testing.T) {
	p := scornOpponentPair(t, shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01"))
	if kind := p.rt.playerKillKind(testDivision, p.m, p.c); kind != pk.DeathTeam {
		t.Fatalf("fixture: opposing capes are kind %d, want a team death", kind)
	}
	_, progression := p.rt.settlePlayerDeathInDoor(testDivision, p.m, deathKiller{player: p.c}, p.rt.Now().UnixMilli())
	if len(progression) == 0 {
		t.Fatal("a death sent its victim nothing")
	}
	last := progression[len(progression)-1]
	if last.Opcode != wire.OpDeathNotice || len(last.Payload) != 1 || last.Payload[0] != uint8(pk.DeathTeam) {
		t.Fatalf("death notice = %#x %v, want 0x37CE {2}", last.Opcode, last.Payload)
	}
	if p.m.DeathKind != uint8(pk.DeathTeam) {
		t.Fatalf("recorded death kind %d, want 2", p.m.DeathKind)
	}
}

/*
================
TestTeamDeathRevivesInPlaceAtAnyLevel

At level 80 the town choice (1) of a team death revives on the spot with
the present point's 40 percent, builds no re-entry, and clears the kind; a
monster death at the same level still cannot take the present point.
================
*/
func TestTeamDeathRevivesInPlaceAtAnyLevel(t *testing.T) {
	team := rebirthTestCharacter(80, 0)
	team.DeathKind = uint8(pk.DeathTeam)
	rt, _ := newTestRuntime(team, testItems())
	result := rt.HandleLocalRebirth(testDivision, team, []byte{wire.RebirthAtSpecifiedPoint})
	if !enterworld.CharacterAlive(team) || len(result.Frames) < 2 {
		t.Fatalf("team death did not revive: %q %+v", result.DiagnosticRefusal, opcodesOf(result.Frames))
	}
	for _, frame := range result.Frames {
		if frame.Opcode == enterworld.OpcodeResetClient {
			t.Fatal("team death revived through a town re-entry")
		}
	}
	if result.Frames[0].Opcode != wire.OpObjectSourceCorrection || result.Frames[1].Opcode != enterworld.OpcodeVitalsUpdate {
		t.Fatalf("team death frames %+v, want the in-place transaction", opcodesOf(result.Frames))
	}
	wantHP := 1 + int64(float64(float32(enterworld.DerivedMaxHP(team)))*float64(float32(0.4)))
	if enterworld.CurrentHP(team) != wantHP {
		t.Fatalf("team death revived with %d HP, want %d (one plus 40 percent)", enterworld.CurrentHP(team), wantHP)
	}
	if team.DeathKind != 0 {
		t.Fatalf("revival kept death kind %d", team.DeathKind)
	}

	monster := rebirthTestCharacter(80, 0)
	monster.DeathKind = uint8(pk.DeathMonster)
	rt, _ = newTestRuntime(monster, testItems())
	if result := rt.HandleLocalRebirth(testDivision, monster, []byte{wire.RebirthAtPresentPoint}); len(result.Frames) != 0 || enterworld.CharacterAlive(monster) {
		t.Fatalf("level 80 monster death took the present point: %+v", opcodesOf(result.Frames))
	}
}
