/*
===========================================================================

flagwar_match_test.go - the Capture the Flag match

Start hands a full house to the instance lane with alternating teams,
the pole cycle runs steal-carry-score, the carrier clock and the
carrier's death drop the flag, winners mint exactly one trophy each,
and a departed participant is reconciled out of the match.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// flagWarMatchTestStartPose stands a player outside the arena before
// the transfer (the return anchor reads it back).
var flagWarMatchTestStartPose = simulation.Spawn{RegionID: 25000, X: 10, Y: 0, Z: 10}

/*
================
flagWarMatchTestItems

The test item source plus the trophy the rewards mint.
================
*/
func flagWarMatchTestItems() staticItemSource {
	items := testItems()
	items[flagWarTrophyCodename] = &enterworld.ItemRef{
		RefObjID: 23353, Codename: flagWarTrophyCodename, TypeIDs: [4]int64{3, 3, 1, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50}),
	}
	return items
}

/*
================
flagWarPlayer

One match test player at a level.
================
*/
func flagWarPlayer(id int64, name string, level int64) *enterworld.Character {
	c := instanceWorldTestCharacter(id, name)
	lvl := level
	c.Level = &lvl
	c.MaxLevel = &lvl
	return c
}

/*
================
flagWarMatchRuntime

A runtime with the match fixtures: real monster state (empty
template: the arena monsters resolve no reference and stay home),
the trophy in the item source, and a capture of every pushed frame.
================
*/
type flagWarMatchRuntime struct {
	rt     *Runtime
	clock  *fakeClock
	pushed map[string][]wire.Frame
}

func newFlagWarMatchRuntime(t *testing.T, characters ...*enterworld.Character) *flagWarMatchRuntime {
	t.Helper()
	return newFlagWarMatchRuntimeWithItems(t, flagWarMatchTestItems(), characters...)
}

/*
================
newFlagWarMatchRuntimeWithItems

The match rig over a chosen item and character reference source: the
shipped-data test passes the real textdata so arena objects resolve.
================
*/
func newFlagWarMatchRuntimeWithItems(t *testing.T, items enterworld.ItemRefSource, characters ...*enterworld.Character) *flagWarMatchRuntime {
	t.Helper()
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
		Items:      items,
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: flagWarStart().Add(-time.Hour)}
	rt.Now = clock.Now
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rig := &flagWarMatchRuntime{rt: rt, clock: clock, pushed: map[string][]wire.Frame{}}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		rig.pushed[name] = append(rig.pushed[name], frames...)
	}
	for index, c := range characters {
		admitInstanceWorldCharacter(t, rt, c, uint64(index+1), flagWarMatchTestStartPose)
	}
	return rig
}

/*
================
flagWarStartMatch

Registers the players in slice order and fires the start hook.
================
*/
func (rig *flagWarMatchRuntime) startMatch(t *testing.T, players ...*enterworld.Character) *flagWarMatch {
	t.Helper()
	registrants := make([]FlagWarRegistrant, 0, len(players))
	for index, c := range players {
		registrants = append(registrants, FlagWarRegistrant{
			Division:    testDivision,
			CharacterID: c.ID,
			Name:        c.Name,
			Gid:         enterworld.ObjectIDForCharacter(c),
			Seq:         int64(index + 1),
		})
	}
	rig.rt.startFlagWarMatch(registrants)
	match := rig.rt.flagWarLive.byDivision[testDivision]
	if match == nil {
		t.Fatal("no live match after the start")
	}
	return match
}

/*
================
flagWarMovePlayer

Stands a player on a spot inside the arena.
================
*/
func (rig *flagWarMatchRuntime) movePlayer(c *enterworld.Character, pose simulation.Spawn) {
	rig.rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = pose
			w.SpawnSet = true
		})
}

/*
================
flagWarTrophies
================
*/
func flagWarTrophies(c *enterworld.Character) int64 {
	var total int64
	for _, row := range c.MissionInventory {
		if row.Codename == flagWarTrophyCodename {
			total += row.StackCount
		}
	}
	return total
}

/*
================
flagWarSelectPole

Selects one arena object for a player standing on it.
================
*/
func (rig *flagWarMatchRuntime) selectObject(t *testing.T, c *enterworld.Character, gid uint32) SelectOutcome {
	t.Helper()
	outcome, handled := rig.rt.flagWarSelect(testDivision, c, gid)
	if !handled {
		t.Fatalf("gid %d was not a match object", gid)
	}
	return outcome
}

/*
================
TestFlagWarMatchShortHouseCancels
================
*/
func TestFlagWarMatchShortHouseCancels(t *testing.T) {
	a := flagWarPlayer(11, "Alpha", 20)
	b := flagWarPlayer(12, "Bravo", 20)
	c := flagWarPlayer(13, "Charly", 20)
	rig := newFlagWarMatchRuntime(t, a, b, c)

	registrants := []FlagWarRegistrant{
		{Division: testDivision, CharacterID: a.ID, Name: a.Name, Gid: enterworld.ObjectIDForCharacter(a), Seq: 1},
		{Division: testDivision, CharacterID: b.ID, Name: b.Name, Gid: enterworld.ObjectIDForCharacter(b), Seq: 2},
		{Division: testDivision, CharacterID: c.ID, Name: c.Name, Gid: enterworld.ObjectIDForCharacter(c), Seq: 3},
	}
	out := rig.rt.startFlagWarMatch(registrants)
	if len(out) != 3 {
		t.Fatalf("short house frames %+v, want three cancels", out)
	}
	for _, batch := range out {
		if len(batch.Frames) != 1 ||
			string(batch.Frames[0].Payload) != string(wire.EncodeFlagWarEventSubtype(wire.FlagWarGameCancel)) {
			t.Fatalf("short house batch %+v, want a game-cancel notice", batch)
		}
	}
	if rig.rt.flagWarLive.byDivision[testDivision] != nil {
		t.Fatal("a short house opened a match")
	}
}

/*
================
TestFlagWarMatchTurnsAwayTheTailPastTheCap
================
*/
func TestFlagWarMatchTurnsAwayTheTailPastTheCap(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	registrants := make([]FlagWarRegistrant, 0, 17)
	for index := 0; index < 17; index++ {
		id := int64(100 + index)
		name := "Ghost"
		if index < len(players) {
			id, name = players[index].ID, players[index].Name
		}
		registrants = append(registrants, FlagWarRegistrant{
			Division: testDivision, CharacterID: id, Name: name, Seq: int64(index + 1),
		})
	}
	out := rig.rt.startFlagWarMatch(registrants)
	sawTurned := false
	for _, batch := range out {
		if batch.OnlyCharacterID == 116 && len(batch.Frames) == 1 &&
			string(batch.Frames[0].Payload) == string(wire.EncodeFlagWarEventSubtype(wire.FlagWarStartNotEnter)) {
			sawTurned = true
		}
	}
	if !sawTurned {
		t.Fatalf("the 17th registrant was not turned away: %+v", out)
	}
	match := rig.rt.flagWarLive.byDivision[testDivision]
	if match == nil || len(match.order) != 4 {
		t.Fatalf("match seats %+v, want the four real players", match)
	}
}

/*
================
TestFlagWarMatchStartsFromTheScheduler

The full seam: the queue drains at the start instant, the hook opens
the arena, teams alternate by sign-up order and everyone lands on
their own side's spawn.
================
*/
func TestFlagWarMatchStartsFromTheScheduler(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	for _, c := range players {
		rig.rt.flagWarToggle(testDivision, c)
	}
	start := flagWarStart()
	rig.rt.advanceFlagWar(start.Add(-11 * time.Minute).UnixMilli())
	rig.rt.advanceFlagWar(start.UnixMilli())

	match := rig.rt.flagWarLive.byDivision[testDivision]
	if match == nil {
		t.Fatal("the scheduler start opened no match")
	}
	wantTeams := []int{0, 1, 0, 1}
	for index, c := range players {
		p := match.participants[c.ID]
		if p == nil || !p.entered {
			t.Fatalf("player %s never entered", c.Name)
		}
		if p.team != wantTeams[index] {
			t.Fatalf("player %s team %d, want %d (alternating by sign-up order)", c.Name, p.team, wantTeams[index])
		}
		if got := domain.CharacterWorldInstance(c); got != uint32(match.lease.ID) {
			t.Fatalf("player %s world %d, want the match lease %d", c.Name, got, match.lease.ID)
		}
		pose := rig.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(c) }).Spawn
		want := flagWarTeamSpawns[wantTeams[index]]
		if pose.RegionID != want.RegionID || pose.X != want.X || pose.Z != want.Z {
			t.Fatalf("player %s landed at %+v, want team spawn %+v", c.Name, pose, want)
		}
		started := false
		for _, frame := range rig.pushed[c.Name] {
			if frame.Opcode == wire.OpFlagWarEvent &&
				string(frame.Payload) == string(wire.EncodeFlagWarEventType(wire.FlagWarGameStart)) {
				started = true
			}
		}
		if !started {
			t.Fatalf("player %s never saw the game start", c.Name)
		}
	}
	if !match.poles[0] || match.poles[1] || match.poles[2] {
		t.Fatalf("poles at start %+v, want only the centre flying", match.poles)
	}

	// The next tick announces mission start per team plus the rows.
	rig.pushed = map[string][]wire.Frame{}
	out := rig.rt.advanceFlagWarMatches(start.Add(time.Second).UnixMilli())
	announced := map[int64]uint16{}
	rows := 0
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpFlagWarEvent && len(frame.Payload) == 3 && frame.Payload[0] == wire.FlagWarMissionStart {
				announced[batch.OnlyCharacterID] = uint16(frame.Payload[1]) | uint16(frame.Payload[2])<<8
			}
			if frame.Opcode == wire.OpObjectListChunk {
				rows++
			}
		}
	}
	if len(announced) != 4 || rows != 20 {
		t.Fatalf("announce: teams %v rows %d, want 4 teams and 5 objects x 4 players", announced, rows)
	}
	for index, c := range players {
		if announced[c.ID] != uint16(wantTeams[index]) {
			t.Fatalf("player %s was told team %d, want %d", c.Name, announced[c.ID], wantTeams[index])
		}
	}
}

/*
================
TestFlagWarPoleCycleStealScoreAndWin

The whole match in miniature: steal the centre, score on the home
pole, and the flying home pole wins when the clock ends - one trophy
per winner, none per loser, then the world closes.
================
*/
func TestFlagWarPoleCycleStealScoreAndWin(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.clock.now = time.UnixMilli(match.startMs)
	bravo, delta := players[1], players[3] // team 1
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// Bravo steals the flying centre pole.
	rig.movePlayer(bravo, flagWarPoleSpawns[0])
	outcome := rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0))
	marked := false
	for _, frame := range outcome.Frames {
		if frame.Opcode == wire.OpFlagWarEvent &&
			string(frame.Payload) == string(wire.EncodeFlagWarCarrier(enterworld.ObjectIDForCharacter(bravo))) {
			marked = true
		}
	}
	if !marked {
		t.Fatal("the steal start did not mark the channeler")
	}
	if match.channels[0] == nil || match.channels[0].mode != 1 {
		t.Fatalf("channel after steal click: %+v", match.channels[0])
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 6000)
	if match.carrierID != bravo.ID || match.poles[0] {
		t.Fatalf("after the steal: carrier %d poles %v, want Bravo carrying and the centre down",
			match.carrierID, match.poles)
	}

	// Bravo scores on team 1's pole.
	rig.movePlayer(bravo, flagWarPoleSpawns[2])
	rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0+2))
	if match.channels[2] == nil || match.channels[2].mode != 0 {
		t.Fatalf("channel after score click: %+v", match.channels[2])
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 12000)
	if !match.poles[2] || match.carrierID != 0 {
		t.Fatalf("after the score: poles %v carrier %d, want pole 2 flying and no carrier", match.poles, match.carrierID)
	}

	// The clock ends: team 1 wins.
	out := rig.rt.advanceFlagWarMatches(match.endsMs)
	won := map[int64]bool{}
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpFlagWarEvent &&
				string(frame.Payload) == string(wire.EncodeFlagWarEventSubtype(wire.FlagWarGameWin)) {
				won[batch.OnlyCharacterID] = true
			}
		}
	}
	if !won[bravo.ID] || !won[delta.ID] || won[players[0].ID] || won[players[2].ID] {
		t.Fatalf("win notices %v, want team 1 only", won)
	}
	for _, c := range players {
		want := int64(0)
		if c == bravo || c == delta {
			want = 1
		}
		if got := flagWarTrophies(c); got != want {
			t.Fatalf("%s carries %d trophies, want %d", c.Name, got, want)
		}
	}

	// A minute later the world closes and the book forgets the match.
	rig.rt.advanceFlagWarMatches(match.closeMs + 1)
	if rig.rt.flagWarLive.byDivision[testDivision] != nil {
		t.Fatal("the match survived its close")
	}
	for _, c := range players {
		if got := domain.CharacterWorldInstance(c); got != domain.DefaultWorldInstance {
			t.Fatalf("%s world %d after the close, want the default world", c.Name, got)
		}
	}
}

/*
================
TestFlagWarBusyPoleAnswers
================
*/
func TestFlagWarBusyPoleAnswers(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	rig.movePlayer(players[1], flagWarPoleSpawns[0])
	rig.selectObject(t, players[1], flagWarObjectGID(flagWarSlotPole0))
	rig.movePlayer(players[3], flagWarPoleSpawns[0])
	outcome := rig.selectObject(t, players[3], flagWarObjectGID(flagWarSlotPole0))
	busy := false
	for _, frame := range outcome.Frames {
		if frame.Opcode == wire.OpFlagWarEvent &&
			string(frame.Payload) == string(wire.EncodeFlagWarEventSubtype(wire.FlagWarStealBusy)) {
			busy = true
		}
	}
	if !busy {
		t.Fatal("the second stealer was not told the pole is busy")
	}
	if match.channels[0] == nil || match.channels[0].characterID != players[1].ID {
		t.Fatal("the busy answer stole the channel")
	}
}

/*
================
TestFlagWarCarrierDropsAfterASixtySeconds

And the killer within nine levels takes the killer's trophy at the
drop; a killer too far above the carrier takes nothing.
================
*/
func TestFlagWarCarrierDropsAfterASixtySeconds(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 20), flagWarPlayer(12, "Bravo", 20),
		flagWarPlayer(13, "Charly", 20), flagWarPlayer(14, "Delta", 20),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	bravo := players[1]
	rig.rt.advanceFlagWarMatches(match.startMs + 1)
	rig.movePlayer(bravo, flagWarPoleSpawns[0])
	rig.selectObject(t, bravo, flagWarObjectGID(flagWarSlotPole0))
	rig.rt.advanceFlagWarMatches(match.startMs + 6000)
	if match.carrierID != bravo.ID {
		t.Fatal("Bravo never carried")
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 6000 + 61000)
	if match.carrierID != 0 || !match.flagLive {
		t.Fatalf("after sixty seconds: carrier %d flagLive %v, want a dropped flag", match.carrierID, match.flagLive)
	}

	// Charly (team 0) picks the flag up; Alpha (level 60 now, team 0)
	// kills her from far above: no killer trophy.
	charly := players[2]
	rig.movePlayer(charly, match.flagSpawn)
	rig.selectObject(t, charly, flagWarObjectGID(flagWarSlotFlag))
	if match.carrierID != charly.ID {
		t.Fatal("Charly never picked the flag up")
	}
	high := int64(60)
	players[0].Level = &high
	rig.rt.recordFlagWarDeath(testDivision, charly, players[0], match.startMs+70000)
	if got := flagWarTrophies(players[0]); got != 0 {
		t.Fatalf("a far-above killer minted %d trophies, want 0", got)
	}
	if match.carrierID != 0 || !match.flagLive {
		t.Fatal("the carrier's death did not drop the flag again")
	}

	// Delta (team 1) carries, Alpha kills within nine levels: trophy.
	delta := players[3]
	rig.movePlayer(delta, match.flagSpawn)
	rig.selectObject(t, delta, flagWarObjectGID(flagWarSlotFlag))
	near := int64(25)
	players[0].Level = &near
	rig.rt.recordFlagWarDeath(testDivision, delta, players[0], match.startMs+80000)
	// The mint is queued on the death lane and drained by the tick.
	if got := flagWarTrophies(players[0]); got != 0 {
		t.Fatalf("the killer's mint landed inline on the death lane: %d trophies", got)
	}
	rig.rt.advanceFlagWarMatches(match.startMs + 81000)
	if got := flagWarTrophies(players[0]); got != 1 {
		t.Fatalf("the carrier's killer holds %d trophies, want exactly 1", got)
	}
}

/*
================
TestFlagWarDepartureReconcilesOut
================
*/
func TestFlagWarDepartureReconcilesOut(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// Charly carries, then leaves the world through the return lane.
	charly := players[2]
	rig.movePlayer(charly, flagWarPoleSpawns[0])
	rig.selectObject(t, charly, flagWarObjectGID(flagWarSlotPole0))
	rig.rt.advanceFlagWarMatches(match.startMs + 6000)
	if match.carrierID != charly.ID {
		t.Fatal("Charly never carried")
	}
	if status := rig.rt.ReturnCharacterFromInstance(testDivision, charly); status != instance.Success {
		t.Fatalf("return from the match world: status %d", status)
	}
	// A stale anchor appears (a lane that left by another road), then
	// one tick reconciles the departure.
	anchor := domain.InstanceReturnAnchor{RegionID: 25000}
	charly.World.InstanceReturn = &anchor
	rig.rt.advanceFlagWarMatches(match.startMs + 7000)
	p := match.participants[charly.ID]
	if p == nil || p.entered {
		t.Fatal("the departed participant stayed in the match")
	}
	if charly.World.InstanceReturn != nil {
		t.Fatal("the departure kept the return anchor")
	}
	if match.carrierID != 0 || !match.flagLive {
		t.Fatal("the departing carrier did not drop the flag")
	}
}

/*
================
TestFlagWarHostilityAndDeathKind
================
*/
func TestFlagWarHostilityAndDeathKind(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	rig.startMatch(t, players...)
	rt := rig.rt

	if !rt.flagWarHostile(testDivision, players[0], players[1]) {
		t.Fatal("opposing participants are not hostile")
	}
	if rt.flagWarHostile(testDivision, players[0], players[2]) {
		t.Fatal("teammates are hostile")
	}
	if !rt.hostilePlayerRelation(testDivision, players[0], players[1]) {
		t.Fatal("the relation lane does not see arena hostility")
	}
	if code := rt.playerAttackRefusal(testDivision, players[0], players[1], rig.clock.now.UnixMilli(), 0); code != 0 {
		t.Fatalf("an arena attack was refused with %#x", code)
	}
	if kind := rt.deathKind(testDivision, players[1], deathKiller{player: players[0]}); kind != pk.DeathSpecialWorld {
		t.Fatalf("an arena kill settled as %v, want the special-world kind", kind)
	}
	if kind := rt.deathKind(testDivision, players[2], deathKiller{player: players[0]}); kind == pk.DeathSpecialWorld {
		t.Fatal("a same-team kill settled as a special-world death")
	}
}

/*
================
TestFlagWarKeyNpcRisesAndBanks
================
*/
func TestFlagWarKeyNpcRisesAndBanks(t *testing.T) {
	players := []*enterworld.Character{
		flagWarPlayer(11, "Alpha", 30), flagWarPlayer(12, "Bravo", 40),
		flagWarPlayer(13, "Charly", 50), flagWarPlayer(14, "Delta", 60),
	}
	rig := newFlagWarMatchRuntime(t, players...)
	match := rig.startMatch(t, players...)
	rig.rt.advanceFlagWarMatches(match.startMs + 1)

	// Side 0's key carrier is gone from the monster state.
	match.keyMonsterGid[0] = 424242
	rig.rt.advanceFlagWarMatches(match.startMs + 2000)
	key := match.objects[flagWarObjectGID(flagWarSlotKey0)]
	if key == nil || !key.live {
		t.Fatal("the key NPC never rose")
	}
	rig.movePlayer(players[0], key.spawn)
	rig.pushed = map[string][]wire.Frame{}
	rig.selectObject(t, players[0], key.gid)
	if !match.keyTaken[0] || key.live {
		t.Fatal("the key click did not bank side 0's key")
	}
	noted := false
	for _, frame := range rig.pushed[players[1].Name] {
		if frame.Opcode == wire.OpFlagWarEvent && len(frame.Payload) >= 2 &&
			frame.Payload[0] == wire.FlagWarSubtypeTag && frame.Payload[1] == wire.FlagWarMasterKey {
			noted = true
		}
	}
	if !noted {
		t.Fatal("the master-key broadcast never went out")
	}
}

/*
================
TestFlagWarBracketFor
================
*/
func TestFlagWarBracketFor(t *testing.T) {
	cases := []struct {
		name   string
		levels []int64
		want   int
	}{
		{"empty", nil, 0},
		{"twenties", []int64{20, 20, 20, 20}, 0},
		{"fifties", []int64{50, 50}, 2},
		{"maxed", []int64{110, 110}, 6},
	}
	for _, tc := range cases {
		if got := flagWarBracketFor(tc.levels); got != tc.want {
			t.Fatalf("%s: bracket %d, want %d", tc.name, got, tc.want)
		}
	}
}
