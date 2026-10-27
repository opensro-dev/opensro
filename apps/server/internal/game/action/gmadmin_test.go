/*
===========================================================================

gmadmin_test.go - /WORLDSTATUS counts and /RECALLGUILD

===========================================================================
*/
package action

import (
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/calendar"
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

/*
================
TestSetGameTimeTellsEveryPlayerInTheWorld

The clock moves once and every admitted player gets the 0x31AD push with
the new time.
================
*/
func TestSetGameTimeTellsEveryPlayerInTheWorld(t *testing.T) {
	f := newGMTeleportFixture(t)
	var asked []uint8
	f.rt.SetWorldHour = func(hour uint8) calendar.Value {
		asked = append(asked, hour)
		return calendar.Value{Day: 7, Hour: hour}
	}
	f.rt.SetGameTimeGM(21)
	want := calendar.Value{Day: 7, Hour: 21}.Payload()
	if len(asked) != 1 || asked[0] != 21 {
		t.Fatalf("clock asked %v", asked)
	}
	for _, name := range []string{f.gm.Name, f.target.Name} {
		frames := f.sent[name]
		if len(frames) != 1 || frames[0].Opcode != simulation.OpGameTime || string(frames[0].Payload) != string(want) {
			t.Fatalf("%s got %+v", name, frames)
		}
	}
}

/*
================
TestSetGameTimeWaitsForAnEntryPublication

An entry holds its division's publication gate from binding its session
until its EnterWorld result is out. A clock push inside that span would
reach a client with no world yet and drop it, so the push waits for the
gate; the clock itself moves at once.
================
*/
func TestSetGameTimeWaitsForAnEntryPublication(t *testing.T) {
	f := newGMTeleportFixture(t)
	moved := make(chan struct{}, 1)
	f.rt.SetWorldHour = func(hour uint8) calendar.Value {
		moved <- struct{}{}
		return calendar.Value{Day: 7, Hour: hour}
	}
	var mu sync.Mutex
	pushed := 0
	f.rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {
		mu.Lock()
		pushed++
		mu.Unlock()
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return pushed
	}
	unlock := f.rt.LockPublication(testDivision, "test")
	done := make(chan struct{})
	go func() {
		f.rt.SetGameTimeGM(21)
		close(done)
	}()
	select {
	case <-moved:
	case <-time.After(5 * time.Second):
		unlock()
		t.Fatal("the clock did not move while an entry held the publication gate")
	}
	select {
	case <-done:
		unlock()
		t.Fatal("the clock push went out while an entry held the publication gate")
	case <-time.After(200 * time.Millisecond):
	}
	if n := count(); n != 0 {
		unlock()
		t.Fatalf("%d clock push(es) under a held gate", n)
	}
	unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the clock push never went out after the gate was released")
	}
	if count() != 2 {
		t.Fatalf("pushed %d, want one to each player", count())
	}
}
