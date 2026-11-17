/*
===========================================================================

roc_test.go - the Roc worlds: the window, the summit's steps, the Front

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

var (
	rocTestZone = time.FixedZone("shard", 9*3600)
	// 2026-11-21 is a Saturday: the window opens at 22:00.
	rocTestOpen     = time.Date(2026, 11, 21, 22, 0, 0, 0, rocTestZone)
	rocTestMountain = simulation.Spawn{RegionID: 25793, X: 500, Y: 10, Z: 600}
	rocTestFrontOut = simulation.Spawn{RegionID: 25792, X: 900, Y: 0, Z: 900}
)

/*
================
rocTestMonsters

The monster seam: every spawn lands with a fresh gid and lives until
the test kills it.
================
*/
type rocTestMonsters struct {
	next    uint32
	spawned map[uint32]simulation.QuestMonsterSpawn
	alive   map[uint32]bool
}

func (m *rocTestMonsters) spawn(request simulation.QuestMonsterSpawn) (uint32, bool) {
	m.next++
	m.spawned[m.next] = request
	m.alive[m.next] = true
	return m.next, true
}

func (m *rocTestMonsters) gidOf(codename string) uint32 {
	for gid, request := range m.spawned {
		if request.Codename == codename {
			return gid
		}
	}
	return 0
}

/*
================
rocFixture

A runtime with the Roc on, the gate catalogue holding the two exits, the
clock just before the window and one character standing in the field.
================
*/
func rocFixture(t *testing.T) (*Runtime, *fakeClock, *rocTestMonsters, *enterworld.Character) {
	t.Helper()
	c := instanceWorldTestCharacter(41, "Climber")
	rt, clock := newInstanceWorldRuntime(t, c)
	rt.portals = &portalCatalog{
		questGate: &questGateBuilding{ref: 19076, tid: 0x130, height: 16, radius: 10},
		destinations: map[uint32]portalDestination{
			1: {id: 1, code: rocMountainGateCode, world: 1, spawn: rocTestMountain},
			2: {id: 2, code: rocFrontGateCode, world: 1, spawn: rocTestFrontOut},
		},
	}
	rt.questGates.roll = func() float64 { return 0 }
	admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)
	rt.ConfigureRoc(true)
	monsters := &rocTestMonsters{spawned: map[uint32]simulation.QuestMonsterSpawn{}, alive: map[uint32]bool{}}
	rt.roc.loc = rocTestZone
	rt.roc.spawn = monsters.spawn
	rt.roc.alive = func(_ string, gid uint32) bool { return monsters.alive[gid] }
	clock.now = rocTestOpen.Add(-time.Second)
	return rt, clock, monsters, c
}

/*
================
rocTick

One tick at the clock's time; answers the shard notices it sent.
================
*/
func rocTick(rt *Runtime, clock *fakeClock) []string {
	var symbols []string
	for _, out := range rt.advanceRoc(clock.now.UnixMilli()) {
		for _, frame := range out.Frames {
			if frame.Opcode == rocNoticeOpcode {
				symbols = append(symbols, string(frame.Payload[2:]))
			}
		}
	}
	return symbols
}

/*
================
rocLayerOpen
================
*/
func rocLayerOpen(rt *Runtime, world instance.DefinitionID) bool {
	_, open := rt.Monsters.PopulationLease(testDivision, instance.Pack(world, portalWorldLayer))
	return open
}

/*
================
TestRocOffOpensNothing

SRO_ROC off: the window passes and nothing opens or refuses.
================
*/
func TestRocOffOpensNothing(t *testing.T) {
	rt, clock, _, c := rocFixture(t)
	rt.ConfigureRoc(false)
	clock.now = rocTestOpen
	if got := rocTick(rt, clock); len(got) != 0 || rocLayerOpen(rt, rocTopWorld) {
		t.Fatalf("off: notices %v, summit open %v", got, rocLayerOpen(rt, rocTopWorld))
	}
	if refusal := rt.rocEntry(testDivision, c, rocTopWorld); refusal != 0 {
		t.Fatalf("off: entry refused %#x", refusal)
	}
}

/*
================
TestRocWindowOpensBothWorldsOnce

Saturday 21:59:59 opens nothing; 22:00 opens the summit and the Front on
layer 1 with the seal stones and Tahomet, and tells the shard once.
================
*/
func TestRocWindowOpensBothWorldsOnce(t *testing.T) {
	rt, clock, monsters, _ := rocFixture(t)
	if got := rocTick(rt, clock); len(got) != 0 || rocLayerOpen(rt, rocTopWorld) {
		t.Fatalf("before the window: notices %v", got)
	}
	clock.now = rocTestOpen
	if got := rocTick(rt, clock); len(got) != 1 || got[0] != rocSymAppear {
		t.Fatalf("at the window: notices %v, want [%s]", got, rocSymAppear)
	}
	if !rocLayerOpen(rt, rocTopWorld) || !rocLayerOpen(rt, rocFrontWorld) {
		t.Fatal("the window did not open both worlds on layer 1")
	}
	for _, stone := range rocSealStones {
		gid := monsters.gidOf(stone.codename)
		if gid == 0 || monsters.spawned[gid].Position != stone.spawn {
			t.Fatalf("%s: spawned %+v", stone.codename, monsters.spawned[gid])
		}
	}
	if gid := monsters.gidOf(rocTahometCodename); gid == 0 || monsters.spawned[gid].Position != rocTahometSpawn {
		t.Fatal("Tahomet was not placed in the Front")
	}
	clock.now = clock.now.Add(time.Minute)
	if got := rocTick(rt, clock); len(got) != 0 {
		t.Fatalf("the open repeated: %v", got)
	}
}

/*
================
TestRocSummitStepsToTheRocAndItsGate

Stones down: the break notice and ten minutes; then entry closes and the
shard hears it; 42 s later the Roc; its fall raises a gate to the
mountain gate; thirty minutes later the layer closes with its resident
sent to the mountain gate.
================
*/
func TestRocSummitStepsToTheRocAndItsGate(t *testing.T) {
	rt, clock, monsters, c := rocFixture(t)
	var pushed []string
	rt.PushCharacterFrames = func(_ string, _ string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode == rocNoticeOpcode {
				pushed = append(pushed, string(frame.Payload[2:]))
			}
		}
	}
	clock.now = rocTestOpen
	rocTick(rt, clock)
	top, _ := rt.roc.layers(testDivision)
	if status := rt.TransferCharacterToInstance(testDivision, c, top.lease, rocSpawn); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}

	for _, stone := range rocSealStones[:7] {
		monsters.alive[monsters.gidOf(stone.codename)] = false
	}
	rocTick(rt, clock)
	if top.step != rocStepStones {
		t.Fatal("a standing stone did not hold the summit")
	}
	monsters.alive[monsters.gidOf(rocSealStones[7].codename)] = false
	rocTick(rt, clock)
	if len(pushed) != 1 || pushed[0] != rocSymBreakStone {
		t.Fatalf("residents heard %v, want [%s]", pushed, rocSymBreakStone)
	}

	clock.now = clock.now.Add(rocBreakStoneMs*time.Millisecond - time.Second)
	if rocTick(rt, clock); top.entryClosed {
		t.Fatal("entry closed before the ten minutes")
	}
	clock.now = clock.now.Add(time.Second)
	if got := rocTick(rt, clock); len(got) != 1 || got[0] != rocSymDisappear || !top.entryClosed {
		t.Fatalf("entrance: notices %v, closed %v", got, top.entryClosed)
	}
	if refusal := rt.rocEntry(testDivision, c, rocTopWorld); refusal == 0 {
		t.Fatal("a closed summit admitted a traveller")
	}

	clock.now = clock.now.Add(rocEntranceMs * time.Millisecond)
	rocTick(rt, clock)
	gid := monsters.gidOf(rocCodename)
	if gid == 0 || monsters.spawned[gid].Position != rocSpawn || monsters.spawned[gid].Population != top.lease {
		t.Fatalf("the Roc: %+v", monsters.spawned[gid])
	}

	fell := monster.Pose{RegionID: 26045, X: 1100, Y: -7, Z: 1700}
	rt.noteRocKill(rocCodename, gid, fell)
	monsters.alive[gid] = false
	rocTick(rt, clock)
	gates := rt.QuestGateNpcs(uint32(top.lease.ID))
	if len(gates) != 1 || gates[0].Spawn.X != fell.X+questGateOffsetMin || gates[0].Spawn.RegionID != fell.RegionID {
		t.Fatalf("the Roc's gate: %+v", gates)
	}
	for _, gate := range rt.questGates.byGID {
		if gate.to == nil || gate.to.spawn != rocTestMountain || gate.to.world != instance.ID(domain.DefaultWorldInstance) {
			t.Fatalf("the gate leads to %+v, want the mountain gate", gate.to)
		}
	}

	clock.now = clock.now.Add(rocAfterKillMs*time.Millisecond - time.Second)
	if rocTick(rt, clock); !rocLayerOpen(rt, rocTopWorld) {
		t.Fatal("the summit closed before the thirty minutes")
	}
	clock.now = clock.now.Add(time.Second)
	rocTick(rt, clock)
	if rocLayerOpen(rt, rocTopWorld) || len(rt.QuestGateNpcs(uint32(top.lease.ID))) != 0 {
		t.Fatal("the summit or its gate outlived the thirty minutes")
	}
	if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || c.World.InstanceReturn != nil {
		t.Fatalf("the resident was not sent out: %+v", c.World)
	}
	if pose := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.now.UnixMilli()); pose.RegionID != rocTestMountain.RegionID {
		t.Fatalf("the resident stands at %+v, want the mountain gate", pose)
	}
}

/*
================
TestRocWindowEndClosesTheSummit

Without a kill, the job's end closes entry and tells the shard; the
layer closes then, an hour having passed, and sends a gate traveller
(no return anchor) to the mountain gate. The Front follows it.
================
*/
func TestRocWindowEndClosesTheSummit(t *testing.T) {
	rt, clock, _, c := rocFixture(t)
	clock.now = rocTestOpen
	rocTick(rt, clock)
	top, _ := rt.roc.layers(testDivision)
	if status := rt.TransferCharacterToInstance(testDivision, c, top.lease, rocSpawn); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}
	c.World.InstanceReturn = nil // as a gate traveller enters

	clock.now = rocTestOpen.Add(4*time.Hour - time.Second)
	if got := rocTick(rt, clock); len(got) != 0 || !rocLayerOpen(rt, rocTopWorld) {
		t.Fatalf("inside the window: notices %v", got)
	}
	clock.now = rocTestOpen.Add(4 * time.Hour)
	if got := rocTick(rt, clock); len(got) != 1 || got[0] != rocSymDisappear {
		t.Fatalf("the end: notices %v, want [%s]", got, rocSymDisappear)
	}
	if rocLayerOpen(rt, rocTopWorld) {
		t.Fatal("the summit outlived the window")
	}
	if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
		t.Fatalf("the traveller was left in %#x", domain.CharacterWorldInstance(c))
	}
	rocTick(rt, clock)
	if rocLayerOpen(rt, rocTopWorld) || rocLayerOpen(rt, rocFrontWorld) {
		t.Fatal("a Roc world reopened or the Front outlived the summit")
	}
}

/*
================
TestRocFrontGateAndSendOut

Tahomet's fall raises a gate to the front gate and refuses new entries;
sixty seconds later whoever stayed is sent there.
================
*/
func TestRocFrontGateAndSendOut(t *testing.T) {
	rt, clock, monsters, c := rocFixture(t)
	clock.now = rocTestOpen
	rocTick(rt, clock)
	_, front := rt.roc.layers(testDivision)
	if status := rt.TransferCharacterToInstance(testDivision, c, front.lease, rocTahometSpawn); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}
	if refusal := rt.rocEntry(testDivision, c, rocFrontWorld); refusal != 0 {
		t.Fatalf("the Front refused %#x while Tahomet lives", refusal)
	}
	monsters.alive[front.tahomet] = false
	rocTick(rt, clock)
	if len(rt.QuestGateNpcs(uint32(front.lease.ID))) != 1 {
		t.Fatal("Tahomet's fall raised no gate")
	}
	if refusal := rt.rocEntry(testDivision, c, rocFrontWorld); refusal == 0 {
		t.Fatal("the Front admitted a traveller after Tahomet fell")
	}
	clock.now = clock.now.Add(rocFrontSendOutMs*time.Millisecond - time.Second)
	if rocTick(rt, clock); domain.CharacterWorldInstance(c) == domain.DefaultWorldInstance {
		t.Fatal("sent out before the sixty seconds")
	}
	clock.now = clock.now.Add(time.Second)
	rocTick(rt, clock)
	if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
		t.Fatalf("the resident stayed in %#x", domain.CharacterWorldInstance(c))
	}
}

/*
================
TestRocGateTravelsToItsDestination

A Roc gate's row moves the selecting resident to the gate's fixed
destination, not to the last recall point the quest gates use.
================
*/
func TestRocGateTravelsToItsDestination(t *testing.T) {
	rt, c, lease, _ := questGateFixture(t)
	exit := travelPoint{spawn: rocTestMountain, world: instance.ID(domain.DefaultWorldInstance)}
	gid, ok := rt.spawnExitGate(lease.ID, instanceWorldTestEntry, &exit)
	if !ok {
		t.Fatal("the gate was not raised")
	}
	rt.Selected.Set(testDivision, c.Name, gid)
	out := questGateTravel(rt, c, gid, 0)
	if len(out.Frames) > 0 && bytes.Equal(out.Frames[0].Payload, portalFailure(errCodeGateMoveFailed).Frames[0].Payload) {
		t.Fatal("the gate refused the move")
	}
	if pose := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, 0); pose.RegionID != rocTestMountain.RegionID || pose.X != rocTestMountain.X {
		t.Fatalf("arrived at %+v, want the mountain gate", pose)
	}
}

/*
================
TestRocSummitGateAdmission

The gate admission runs the summit's rule: a closed summit refuses the
traveller before the transfer check.
================
*/
func TestRocSummitGateAdmission(t *testing.T) {
	rt, clock, _, c := rocFixture(t)
	clock.now = rocTestOpen
	rocTick(rt, clock)
	arrival := portalDestination{code: "GATE_RC_ROC_GATE", world: rocTopWorld, spawn: rocSpawn}
	if _, refusal := rt.portalWorldAdmission(testDivision, c, arrival); refusal != 0 {
		t.Fatalf("an open summit refused %#x", refusal)
	}
	top, _ := rt.roc.layers(testDivision)
	top.entryClosed = true
	if _, refusal := rt.portalWorldAdmission(testDivision, c, arrival); refusal != portalTransferError(instance.MissingLayer) {
		t.Fatalf("a closed summit answered %#x", refusal)
	}
}
