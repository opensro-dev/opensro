/*
===========================================================================

gmadmin_test.go - /WORLDSTATUS counts and /RECALLGUILD

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
oneGuild

A guild store holding one guild; every other method is the embedded nil
interface and must not be reached.
================
*/
type oneGuild struct {
	domain.GuildStore
	id      int64
	name    string
	members []string
}

func (g oneGuild) Guild(_ string, id int64) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
	if id != g.id {
		return enterworld.GuildRecord{}, nil, false
	}
	rows := make([]enterworld.GuildMemberRecord, 0, len(g.members))
	for _, name := range g.members {
		rows = append(rows, enterworld.GuildMemberRecord{Name: name})
	}
	return enterworld.GuildRecord{Name: g.name}, rows, true
}

/*
================
TestWorldStatusCountsTheDivision

Players are the admitted ones; others are the resident monsters and the
NPC roster; items are the ground registry's.
================
*/
func TestWorldStatusCountsTheDivision(t *testing.T) {
	f := newGMTeleportFixture(t)
	players, others, items := f.rt.WorldStatusGM(testDivision)
	if players != 2 || others != uint32(len(f.rt.NpcRoster)) || items != uint32(f.rt.Ground.Count(testDivision)) {
		t.Fatalf("status %d/%d/%d, roster %d", players, others, items, len(f.rt.NpcRoster))
	}
	if p, _, _ := f.rt.WorldStatusGM("other-division"); p != 0 {
		t.Fatalf("another division counted %d players", p)
	}
}

/*
================
TestRecallGuildBringsItsMembersToTheGM

The member in the world comes to where the GM stands; an unknown guild
is refused and moves nobody.
================
*/
func TestRecallGuildBringsItsMembersToTheGM(t *testing.T) {
	f := newGMTeleportFixture(t)
	guildID := int64(9)
	f.target.GuildID = &guildID
	f.deps.Guilds = oneGuild{id: guildID, name: "OakWard", members: []string{f.target.Name, "Elsewhere"}}
	installMidMove(f.rt, f.gm, f.clock)
	standing := f.rt.liveSpawn(simulation.WorldKey(testDivision, f.gm.Name), f.gm, f.clock.NowMs())
	gmAt := spawnOf(f.gm)
	if f.rt.RecallGuildGM(testDivision, f.gm.Name, "NoSuchGuild") || len(f.sent) != 0 {
		t.Fatal("an unknown guild was recalled")
	}
	if !f.rt.RecallGuildGM(testDivision, f.gm.Name, "oakward") {
		t.Fatal("recall refused")
	}
	f.movedOnly(t, f.target, f.gm, gmAt, standing)
}
