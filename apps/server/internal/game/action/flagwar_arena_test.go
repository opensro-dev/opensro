/*
===========================================================================

flagwar_arena_test.go - the Capture the Flag arena, under fire

The arena half's pins: every bracket codename resolves in the shipped
data, a flag fall resets the score channel, the killer's mint
survives being decided under the division lane, a completing steal
never makes a second carrier, the master key banks for the clicker's
side, and the match book holds together while the tick and the
select lane race over it (this file earns its keep under -race).
    10|
===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestFlagWarBracketCodenamesResolveInShippedData

Every bracket codename must resolve through the exact lookup the
spawn lane uses, against the shipped v1.150 textdata - a codename
that rots darkens the side monsters and the master-key lane
silently. End to end: a match on the shipped template fields both
key carriers.
================
*/
func TestFlagWarBracketCodenamesResolveInShippedData(t *testing.T) {
	refs := monster.LoadMonsterRefs(gamedatatest.TextdataDir(t))
	state := simulation.NewMonsterState(monster.TemplateFromParts(refs, nil))
	for bracket, codename := range flagWarBracketCodenames {
		ref, ok := state.ReferenceByCodename(codename)
		if !ok {
			t.Fatalf("bracket %d codename %s does not resolve in the shipped data", bracket, codename)
		}
		if ref.MaxHP == 0 {
			t.Fatalf("bracket %d codename %s has no HP; the GM spawn lane refuses it", bracket, codename)
		}
	}

	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	// The production reference source: the arena poles, flag, key and doors
	// are NPC-class rows only the full character data carries.
	rig := newFlagWarMatchRuntimeWithItems(t, enterworld.NewTextdataItems(gamedatatest.TextdataDir(t)), players...)
	rig.rt.Monsters = state
	match := rig.startMatch(t, players...)
	if match.keyMonsterGid[0] == 0 || match.keyMonsterGid[1] == 0 {
		t.Fatalf("key carriers on the shipped template %v, want both sides fielded", match.keyMonsterGid)
	}
	for _, object := range match.sortedObjects() {
		if object.refObjID == 0 {
			t.Fatalf("arena object %s resolved no reference in the shipped data", object.codename)
		}
	}
}

/*
================
TestFlagWarDropResetsTheScoreChannel

The native drop resets every pole channel: a carrier who starts the
score channel in the last seconds of the minute must not complete
it after the flag has fallen (the flag NPC stands AND the pole
flies was the failure shape).
================
*/
func TestFlagWarDropResetsTheScoreChannel(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	bravo := players[1] // team 1, own pole 2
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	rig.movePlayer(bravo, flagWarPoleSpawns[0])
	rig.clock.now = time.UnixMilli(match.startMs)
	rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0))
	rig.rt.advanceFlagWarMatches(match.startMs + 6000) // carries since +6000
	if match.carrierID != bravo.ID {
		t.Fatal("Bravo never carried")
	}

	// At 58 s of carry, start the score channel on his own pole.
	rig.movePlayer(bravo, flagWarPoleSpawns[2])
	rig.clock.now = time.UnixMilli(match.startMs + 64000)
	rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0+2))
	if match.channels[2] == nil {
		t.Fatal("no score channel")
	}

	// The sixty-second drop fires mid-channel.
	rig.rt.advanceFlagWarMatches(match.startMs + 66500)
	if match.carrierID != 0 || !match.flagLive {
		t.Fatalf("after the drop: carrier %d flagLive %v, want the flag down", match.carrierID, match.flagLive)
	}
	if match.channels[2] != nil {
		t.Fatal("the drop left the score channel running")
	}

	// The channel deadline passes: nothing completes.
	rig.rt.advanceFlagWarMatches(match.startMs + 70000)
	if match.poles[2] {
		t.Fatal("the pole flew for a score nobody carried home")
	}
	if !match.flagLive {
		t.Fatal("the dropped flag vanished when the dead channel closed")
	}
}

/*
================
TestFlagWarKillerTrophySurvivesTheDivisionLane

The fatal-blow publish holds the division lane when it reaches the
death hook; the mint lane takes that same lock. The hook must queue
the mint (the tick drains it) instead of granting inline - and the
level window reads the victim's LIVE level, not the match-start
snapshot.
================
*/
func TestFlagWarKillerTrophySurvivesTheDivisionLane(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	bravo := players[1]
	rig.rt.advanceFlagWarMatches(match.startMs + 1)
	rig.movePlayer(bravo, flagWarPoleSpawns[0])
	rig.clock.now = time.UnixMilli(match.startMs)
	rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0))
	rig.rt.advanceFlagWarMatches(match.startMs + 6000)
	if match.carrierID != bravo.ID {
		t.Fatal("Bravo never carried")
	}

	// Bravo levelled on arena monsters: his live level is 35, far
	// above Alpha's 20, though the match-start snapshot says 20.
	risen := int64(35)
	bravo.Level = &risen

	done := make(chan struct{})
	go func() {
		unlock := rig.rt.lockDivision(testDivision)
		rig.rt.recordFlagWarDeath(testDivision, bravo, players[0], match.startMs+7000)
		unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the death hook never returned under the division lane (deadlock)")
	}
	if match.carrierID != 0 || !match.flagLive {
		t.Fatal("the carrier's death did not drop the flag")
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 8000)
	if got := flagWarTrophies(players[0]); got != 0 {
		t.Fatalf("a kill fifteen levels below the carrier's live level minted %d trophies, want 0", got)
	}

	// Delta carries at 20, Alpha (25) kills in-window: one trophy,
	// minted by the tick drain after the lane is free.
	delta := players[3]
	rig.movePlayer(delta, match.flagSpawn)
	rig.clock.now = time.UnixMilli(match.startMs + 9000)
	rig.selectObject(t, delta, flagWarObjectGID(flagWarSlotFlag))
	if match.carrierID != delta.ID {
		t.Fatal("Delta never picked the flag up")
	}
	near := int64(25)
	players[0].Level = &near
	unlock := rig.rt.lockDivision(testDivision)
	rig.rt.recordFlagWarDeath(testDivision, delta, players[0], match.startMs+10000)
	unlock()
	if got := flagWarTrophies(players[0]); got != 0 {
		t.Fatalf("the killer's mint landed inline on the death lane: %d trophies", got)
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 11000)
	if got := flagWarTrophies(players[0]); got != 1 {
		t.Fatalf("the carrier's killer holds %d trophies, want exactly 1", got)
	}
}

/*
================
TestFlagWarStealCompletionKeepsSingleCarrier

The single-flag guard at completion: a steal that finishes while a
carrier carries (or while the dropped flag stands) only grounds
its pole. Two carriers would desync the client's one carrier mark.
The channel states are planted where the arena's own flow cannot
put two poles in the air at once; the guard itself is what is on
trial.
================
*/
func TestFlagWarStealCompletionKeepsSingleCarrier(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// Bravo carries for real.
	bravo := players[1]
	rig.movePlayer(bravo, flagWarPoleSpawns[0])
	rig.clock.now = time.UnixMilli(match.startMs)
	rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0))
	rig.rt.advanceFlagWarMatches(match.startMs + 6000)
	if match.carrierID != bravo.ID {
		t.Fatal("Bravo never carried")
	}

	// A second steal completes into the live carry: pole grounds,
	// nobody new carries.
	match.poles[2] = true
	match.channels[2] = &flagWarChannel{
		characterID: players[0].ID, gid: enterworld.ObjectIDForCharacter(players[0]),
		name: players[0].Name, team: 0, mode: 1, deadlineMs: match.startMs + 6500,
		startX: flagWarPoleSpawns[2].X, startZ: flagWarPoleSpawns[2].Z,
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 7000)
	if match.carrierID != bravo.ID {
		t.Fatalf("carrier %d after the second steal, want Bravo still carrying", match.carrierID)
	}
	if match.poles[2] {
		t.Fatal("the grounded pole kept flying")
	}

	// And while the dropped flag stands: same story.
	rig.rt.advanceFlagWarMatches(match.startMs + 6000 + 61000)
	if !match.flagLive || match.carrierID != 0 {
		t.Fatal("the sixty-second drop never stood the flag up")
	}
	match.poles[0] = true
	match.channels[0] = &flagWarChannel{
		characterID: players[3].ID, gid: enterworld.ObjectIDForCharacter(players[3]),
		name: players[3].Name, team: 1, mode: 1, deadlineMs: match.startMs + 68000,
		startX: flagWarPoleSpawns[0].X, startZ: flagWarPoleSpawns[0].Z,
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 69000)
	if match.carrierID != 0 || !match.flagLive {
		t.Fatalf("after the steal into a standing flag: carrier %d flagLive %v, want nobody and the flag up",
			match.carrierID, match.flagLive)
	}
	if match.poles[0] {
		t.Fatal("the grounded centre pole kept flying")
	}
}

/*
================
TestFlagWarKeyBanksForTheClickersSide

Native (the key-click handler): a key click banks the
CLICKER's side and the broadcast names the clicker's side, even
when the corpse was the other side's carrier.
================
*/
func TestFlagWarKeyBanksForTheClickersSide(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// Side 0's key carrier dies; the key rises on side 0's anchor.
	match.keyMonsterGid[0] = 424242
	rig.rt.advanceFlagWarMatches(match.startMs + 2000)
	key := match.objects[flagWarObjectGID(flagWarSlotKey0)]
	if key == nil || !key.live {
		t.Fatal("the key NPC never rose")
	}

	// Bravo (team 1) clicks the other side's key: HIS side banks it.
	rig.movePlayer(players[1], key.spawn)
	rig.pushed = map[string][]wire.Frame{}
	rig.selectObject(t, players[1], key.gid)
	if match.keyTaken[0] {
		t.Fatal("side 0 banked a key its players never claimed")
	}
	if !match.keyTaken[1] {
		t.Fatal("the clicker's side never banked the key")
	}
	noted := false
	for _, frame := range rig.pushed[players[1].Name] {
		if frame.Opcode == wire.OpFlagWarEvent && len(frame.Payload) >= 7 &&
			frame.Payload[0] == wire.FlagWarSubtypeTag && frame.Payload[1] == wire.FlagWarMasterKey &&
			frame.Payload[6] == 1 {
			noted = true
		}
	}
	if !noted {
		t.Fatal("the master-key broadcast did not carry the clicker's team byte")
	}
}

/*
================
TestFlagWarSelectRacesTheTick

The tick (tick-publication lock only) and a player's select (the
division lane) walk the same match book. Without the match lock
this reports on the pole table and the objects map; the concurrent
map access alone would crash the world in production. Run under
-race for the teeth.
================
*/
func TestFlagWarSelectRacesTheTick(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)
	rig.movePlayer(players[1], flagWarPoleSpawns[0])

	done := make(chan struct{})
	go func() {
		for i := int64(0); i < 3000; i++ {
			unlock := rig.rt.lockTickPublication()
			rig.rt.advanceFlagWarMatches(match.startMs + 2 + i*100)
			unlock()
		}
		close(done)
	}()
	for i := 0; i < 3000; i++ {
		unlock := rig.rt.lockDivision(testDivision)
		rig.rt.flagWarSelect(testDivision, players[1], flagWarObjectGID(flagWarSlotPole0))
		unlock()
	}
	<-done
}
