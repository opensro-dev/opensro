/*
===========================================================================

instance_world_test.go - instanced world transfers, returns and lifetimes

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The shipped instance definitions these tests pin: a party-sized quest
// instance and a single-player one (v1150_gameworld.tsv).
const (
	instanceWorldTestPartyDefinition  = instance.DefinitionID(10) // INS_QNO_RM_OLDWOMAN_6, 8 players
	instanceWorldTestSingleDefinition = instance.DefinitionID(19) // INS_QNO_TQ_BAEKRYEONG_10, 1 player
)

var (
	instanceWorldTestStart = simulation.Spawn{RegionID: 23644, X: 100, Y: 20, Z: 300, Angle: 45}
	instanceWorldTestEntry = simulation.Spawn{RegionID: 25000, X: -50, Y: 5, Z: 60, Angle: 270}
)

/*
================
instanceWorldTestCharacter
================
*/
func instanceWorldTestCharacter(id int64, name string) *enterworld.Character {
	c := testCharacter()
	c.ID = id
	c.Name = name
	hp := int64(100)
	c.CurrentHP = &hp
	mp := int64(75)
	c.CurrentMP = &mp
	writeBackWorld(c, simulation.DefaultWorldState(simulation.ChinaStartProfile()))
	return c
}

/*
================
newInstanceWorldRuntime
================
*/
func newInstanceWorldRuntime(t *testing.T, characters ...*enterworld.Character) (*Runtime, *fakeClock) {
	t.Helper()
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	return rt, clock
}

/*
================
admitInstanceWorldCharacter

Admits the character in the default world and stands it at pose in the
movement lane.
================
*/
func admitInstanceWorldCharacter(t *testing.T, rt *Runtime, c *enterworld.Character, session uint64, pose simulation.Spawn) {
	t.Helper()
	if err := rt.AdmitCharacterSession(testDivision, c.Name, session); err != nil {
		t.Fatal(err)
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = pose
			w.SpawnSet = true
		})
}

/*
================
openInstanceWorldOrFail
================
*/
func openInstanceWorldOrFail(t *testing.T, rt *Runtime, definition instance.DefinitionID, deadlineMs int64) instance.Lease {
	t.Helper()
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: testDivision, Definition: definition, DeadlineMs: deadlineMs})
	if status != instance.Success {
		t.Fatalf("open %d: status %d", definition, status)
	}
	return lease
}

/*
================
TestOpenInstanceWorldRefusals
================
*/
func TestOpenInstanceWorldRefusals(t *testing.T) {
	rt, _ := newInstanceWorldRuntime(t, instanceWorldTestCharacter(11, "Opener"))

	if _, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: testDivision, Definition: instance.DefinitionID(0xFFFF)}); status != instance.MissingWorld {
		t.Fatalf("unknown definition: status %d, want MissingWorld", status)
	}
	permanent := instance.DefinitionID(0)
	for _, definition := range instance.Shipped() {
		if definition.NativeType == 0 {
			permanent = definition.ID
			break
		}
	}
	if permanent == 0 {
		t.Fatal("no permanent definition shipped")
	}
	if _, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: testDivision, Definition: permanent}); status != instance.InvalidLayer {
		t.Fatalf("permanent definition: status %d, want InvalidLayer", status)
	}

	// Layers are finite: the tightest instanced definition refuses once
	// every authored layer is open.
	tightest := instance.Definition{}
	for _, definition := range instance.Shipped() {
		if definition.NativeType == 1 && (tightest.ID == 0 || definition.LayerLimit < tightest.LayerLimit) {
			tightest = definition
		}
	}
	if tightest.ID == 0 {
		t.Fatal("no instanced definition shipped")
	}
	for layer := uint16(0); layer < tightest.LayerLimit; layer++ {
		openInstanceWorldOrFail(t, rt, tightest.ID, 0)
	}
	if _, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: testDivision, Definition: tightest.ID}); status != instance.AllocationFull {
		t.Fatalf("exhausted definition: status %d, want AllocationFull", status)
	}
}

/*
================
TestTransferCharacterToInstanceAndBack
================
*/
func TestTransferCharacterToInstanceAndBack(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Traveller")
	rt, _ := newInstanceWorldRuntime(t, c)
	admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)
	frames := map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, f []wire.Frame) { frames[name] = append(frames[name], f...) }

	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	if status := rt.TransferCharacterToInstance(testDivision, c, lease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}
	if c.World.PackedInstance == nil || *c.World.PackedInstance != uint32(lease.ID) {
		t.Fatalf("packed instance after transfer: %+v", c.World.PackedInstance)
	}
	anchor := c.World.InstanceReturn
	if anchor == nil {
		t.Fatal("transfer recorded no return anchor")
	}
	if anchor.PackedInstance != domain.DefaultWorldInstance || anchor.RegionID != instanceWorldTestStart.RegionID ||
		anchor.X != float32(instanceWorldTestStart.X) || anchor.Y != float32(instanceWorldTestStart.Y) ||
		anchor.Z != float32(instanceWorldTestStart.Z) || anchor.Angle != instanceWorldTestStart.Angle {
		t.Fatalf("anchor = %+v, want the pre-move world and pose", anchor)
	}
	if len(frames[c.Name]) == 0 || frames[c.Name][0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("transfer did not open with a client reset: %+v", frames[c.Name])
	}
	if live, admitted := rt.EntryPopulationLease(testDivision, c.Name); !admitted || live != lease {
		t.Fatalf("membership after transfer: %+v admitted %v", live, admitted)
	}
	pose := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) }).Spawn
	if pose != instanceWorldTestEntry {
		t.Fatalf("pose after transfer = %+v, want %+v", pose, instanceWorldTestEntry)
	}

	if status := rt.ReturnCharacterFromInstance(testDivision, c); status != instance.Success {
		t.Fatalf("return: status %d", status)
	}
	if got := domain.CharacterWorldInstance(c); got != domain.DefaultWorldInstance {
		t.Fatalf("world after return = %d, want the default world", got)
	}
	if c.World.InstanceReturn != nil {
		t.Fatal("return kept the anchor")
	}
	pose = rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) }).Spawn
	if pose != instanceWorldTestStart {
		t.Fatalf("pose after return = %+v, want %+v", pose, instanceWorldTestStart)
	}
}

/*
================
TestInstanceTransferRefusals
================
*/
func TestInstanceTransferRefusals(t *testing.T) {
	a := instanceWorldTestCharacter(11, "First")
	b := instanceWorldTestCharacter(12, "Second")
	rt, _ := newInstanceWorldRuntime(t, a, b)
	admitInstanceWorldCharacter(t, rt, a, 1, instanceWorldTestStart)
	admitInstanceWorldCharacter(t, rt, b, 2, instanceWorldTestStart)

	if status := rt.ReturnCharacterFromInstance(testDivision, a); status != instance.NotMember {
		t.Fatalf("return without anchor: status %d, want NotMember", status)
	}
	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestSingleDefinition, 0)
	if status := rt.TransferCharacterToInstance(testDivision, a, lease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("first transfer: status %d", status)
	}
	if status := rt.TransferCharacterToInstance(testDivision, a, lease, instanceWorldTestEntry); status != instance.AlreadyMember {
		t.Fatalf("second transfer, same layer: status %d, want AlreadyMember", status)
	}
	other := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	if status := rt.TransferCharacterToInstance(testDivision, a, other, instanceWorldTestEntry); status != instance.AlreadyMember {
		t.Fatalf("second transfer, other layer: status %d, want AlreadyMember", status)
	}
	// The single-player layer is now full for everyone else, without bypass.
	if status := rt.TransferCharacterToInstance(testDivision, b, lease, instanceWorldTestEntry); status != instance.PlayerLimitReached {
		t.Fatalf("transfer into a full layer: status %d, want PlayerLimitReached", status)
	}
	if b.World.InstanceReturn != nil || domain.CharacterWorldInstance(b) != domain.DefaultWorldInstance {
		t.Fatal("a refused transfer moved or anchored the second character")
	}

	// A released layer refuses by generation, not by id.
	stale := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	rt.CloseInstanceWorld(testDivision, stale)
	if status := rt.TransferCharacterToInstance(testDivision, b, stale, instanceWorldTestEntry); status != instance.MissingLayer {
		t.Fatalf("transfer on a stale lease: status %d, want MissingLayer", status)
	}
	// A character that never entered the world is not a member anywhere.
	stranger := instanceWorldTestCharacter(13, "Stranger")
	if status := rt.TransferCharacterToInstance(testDivision, stranger, other, instanceWorldTestEntry); status != instance.NotMember {
		t.Fatalf("transfer of an unadmitted character: status %d, want NotMember", status)
	}
}

/*
================
TestTransferPartyToInstanceReportsEachMember
================
*/
func TestTransferPartyToInstanceReportsEachMember(t *testing.T) {
	leader := instanceWorldTestCharacter(11, "Leader")
	mate := instanceWorldTestCharacter(12, "Mate")
	away := instanceWorldTestCharacter(13, "Away")
	rt, _ := newInstanceWorldRuntime(t, leader, mate, away)
	admitInstanceWorldCharacter(t, rt, leader, 1, instanceWorldTestStart)
	admitInstanceWorldCharacter(t, rt, mate, 2, instanceWorldTestStart)
	admitInstanceWorldCharacter(t, rt, away, 3, instanceWorldTestStart)

	// One member is already inside another instance when the party moves.
	awayLease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	if status := rt.TransferCharacterToInstance(testDivision, away, awayLease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("away transfer: status %d", status)
	}
	const offlineGid = uint32(0x7FFFFFF0)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{
			enterworld.ObjectIDForCharacter(leader),
			enterworld.ObjectIDForCharacter(mate),
			enterworld.ObjectIDForCharacter(away),
			offlineGid,
		}}}
	}
	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	results := rt.TransferPartyToInstance(testDivision, leader, lease, instanceWorldTestEntry)
	if len(results) != 4 {
		t.Fatalf("party results: %+v", results)
	}
	byGid := map[uint32]InstanceTransferResult{}
	for _, result := range results {
		byGid[result.Gid] = result
	}
	if got := byGid[enterworld.ObjectIDForCharacter(leader)]; got.Status != instance.Success || got.Name != leader.Name {
		t.Fatalf("leader result: %+v", got)
	}
	if got := byGid[enterworld.ObjectIDForCharacter(mate)]; got.Status != instance.Success || got.Name != mate.Name {
		t.Fatalf("mate result: %+v", got)
	}
	if got := byGid[enterworld.ObjectIDForCharacter(away)]; got.Status != instance.NotMember {
		t.Fatalf("away result: %+v, want NotMember (another world)", got)
	}
	if got := byGid[offlineGid]; got.Status != instance.NotMember || got.Name != "" {
		t.Fatalf("offline result: %+v, want an unnamed NotMember", got)
	}
	if domain.CharacterWorldInstance(away) != uint32(awayLease.ID) || away.World.InstanceReturn == nil {
		t.Fatal("the party transfer disturbed the member in another instance")
	}
	if leader.World.InstanceReturn == nil || mate.World.InstanceReturn == nil {
		t.Fatal("moved members hold no return anchor")
	}
}

/*
================
TestCloseInstanceWorldEvacuatesResidents
================
*/
func TestCloseInstanceWorldEvacuatesResidents(t *testing.T) {
	a := instanceWorldTestCharacter(11, "First")
	b := instanceWorldTestCharacter(12, "Second")
	rt, _ := newInstanceWorldRuntime(t, a, b)
	admitInstanceWorldCharacter(t, rt, a, 1, instanceWorldTestStart)
	admitInstanceWorldCharacter(t, rt, b, 2, instanceWorldTestStart)

	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	for _, c := range []*enterworld.Character{a, b} {
		if status := rt.TransferCharacterToInstance(testDivision, c, lease, instanceWorldTestEntry); status != instance.Success {
			t.Fatalf("transfer %s: status %d", c.Name, status)
		}
	}
	rt.CloseInstanceWorld(testDivision, lease)
	for _, c := range []*enterworld.Character{a, b} {
		if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || c.World.InstanceReturn != nil {
			t.Fatalf("%s was not returned by the close: world %+v", c.Name, c.World)
		}
	}
	if _, open := rt.Monsters.PopulationLease(testDivision, lease.ID); open {
		t.Fatal("the closed layer's population survived")
	}
}

/*
================
TestInstanceWorldDeadlineClosesTheLayer
================
*/
func TestInstanceWorldDeadlineClosesTheLayer(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Timed")
	rt, clock := newInstanceWorldRuntime(t, c)
	admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)

	deadline := clock.NowMs() + 1000
	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, deadline)
	if status := rt.TransferCharacterToInstance(testDivision, c, lease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}
	rt.tickInstanceWorlds(deadline - 1)
	if domain.CharacterWorldInstance(c) != uint32(lease.ID) {
		t.Fatal("the layer closed before its deadline")
	}
	rt.tickInstanceWorlds(deadline)
	if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || c.World.InstanceReturn != nil {
		t.Fatal("the deadline did not return the resident")
	}
	if _, open := rt.Monsters.PopulationLease(testDivision, lease.ID); open {
		t.Fatal("the deadline left the population open")
	}
}
