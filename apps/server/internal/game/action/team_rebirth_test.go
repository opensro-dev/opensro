/*
===========================================================================

team_rebirth_test.go - tests for the death notice and the team-death rebirth

A death tells its victim its kind (0x37CE on the v1.150 client),
and a team death (free-battle capes, arena) revives where it fell at any
level whatever the choice (reference server); any other death keeps the
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

/*
================
TestTeamDeathTownRuleSetting

SRO_TEAM_DEATH_REVIVE off (port-only): a team death keeps its recorded kind
but its notice carries none, so the box reads "point", and at level 80 it
cannot take the present point. Unset and "on" are native; anything else is
refused.
================
*/
func TestTeamDeathTownRuleSetting(t *testing.T) {
	for text, want := range map[string]bool{"": true, "on": true, " ON ": true, "off": false, "false": false} {
		if got, err := ParseTeamDeathRevive(text); err != nil || got != want {
			t.Fatalf("ParseTeamDeathRevive(%q) = %v, %v; want %v", text, got, err, want)
		}
	}
	if _, err := ParseTeamDeathRevive("maybe"); err == nil {
		t.Fatal("a malformed setting was accepted")
	}

	p := scornOpponentPair(t, shippedOffense(t, "SKILL_EU_ROG_STEALTHA_CHANGE_A_01"))
	p.rt.TeamDeathTownRule = true
	_, progression := p.rt.settlePlayerDeathInDoor(testDivision, p.m, deathKiller{player: p.c}, p.rt.Now().UnixMilli())
	last := progression[len(progression)-1]
	if p.m.DeathKind != uint8(pk.DeathTeam) || last.Opcode != wire.OpDeathNotice || last.Payload[0] != uint8(pk.DeathNone) {
		t.Fatalf("town rule: kind %d notice %#x %v, want kind 2 and 0x37CE {0}", p.m.DeathKind, last.Opcode, last.Payload)
	}

	team := rebirthTestCharacter(80, 0)
	team.DeathKind = uint8(pk.DeathTeam)
	rt, _ := newTestRuntime(team, testItems())
	rt.TeamDeathTownRule = true
	if result := rt.HandleLocalRebirth(testDivision, team, []byte{wire.RebirthAtPresentPoint}); len(result.Frames) != 0 || enterworld.CharacterAlive(team) {
		t.Fatalf("town rule: a level 80 team death took the present point: %+v", opcodesOf(result.Frames))
	}
}

/*
================
TestSessionEndForgetsTheDeathKind

The reference server's runtime reset zeroes the death kind and the
last EXP loss with the actor, so a corpse that relogs takes the town rule
and refunds no earlier loss.
================
*/
func TestSessionEndForgetsTheDeathKind(t *testing.T) {
	c := rebirthTestCharacter(80, 0)
	c.DeathKind, c.LastExpLoss = uint8(pk.DeathTeam), 5000
	rt, _ := newTestRuntime(c, testItems())
	rt.ForgetCharacter(testDivision, c.Name)
	if c.DeathKind != 0 || c.LastExpLoss != 0 {
		t.Fatalf("after the session: kind %d loss %d", c.DeathKind, c.LastExpLoss)
	}
	if result := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint}); len(result.Frames) != 0 || enterworld.CharacterAlive(c) {
		t.Fatalf("a relogged corpse revived in place: %+v", opcodesOf(result.Frames))
	}
}
