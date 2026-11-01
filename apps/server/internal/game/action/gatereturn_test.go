/*
===========================================================================

gatereturn_test.go - the Bandit Den gate's return-point row and 0x7495 type 1

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
gateTownReturnFixture

The return fixture's character beside a selected return-point gate (gid
2001) that holds only service 9, as the Bandit Den gate does.
================
*/
func gateTownReturnFixture(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	rt, c, _, _ := returnFixture(t, 30000)
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 2001, RefObjID: 2197, Codename: "STORE_TD_GATE", TalkFlags: simulation.NpcTalkFlagResurrectPoint,
		Services:      simulation.NpcServices(0).With(simulation.NpcServiceResurrectPoint),
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
	rt.NpcSpawn.Enabled = true
	rt.portals = &portalCatalog{sources: map[uint32]uint32{2197: 13}, destinations: map[uint32]portalDestination{13: {returnPoint: true}}}
	rt.Selected.Set(testDivision, c.Name, 2001)
	return rt, c
}

/*
================
sendGateTownReturn
================
*/
func sendGateTownReturn(rt *Runtime, c *enterworld.Character) OpResult {
	return rt.HandlePortal(testDivision, c, wire.NewWriter(5).U32(2001).U8(gateTownReturn).Payload())
}

/*
================
TestDenGateGrantsOnlyTheReturnPointRow

Native data: GATE_TD sets CanGotoResurrectPos and has no link out, so its
building holds service 9 alone (service 8 comes with a link) and the client draws only
"Teleport to return point". A linked town gate keeps its destination row.
================
*/
func TestDenGateGrantsOnlyTheReturnPointRow(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	roster, err := simulation.AppendTeleportGates(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	rt, _, _, _ := returnFixture(t, 30000)
	rt.NpcRoster = roster
	if err := rt.ConfigurePortals(dir); err != nil {
		t.Fatal(err)
	}
	const teleportBits = simulation.NpcTalkFlagRecallPoint | simulation.NpcTalkFlagTeleport | simulation.NpcTalkFlagResurrectPoint
	found := map[string]simulation.NpcDef{}
	byRef := map[uint32]simulation.NpcDef{}
	returnPoints := 0
	for _, npc := range rt.NpcRoster {
		found[npc.Codename] = npc
		byRef[npc.RefObjID] = npc
		if npc.TalkFlags&simulation.NpcTalkFlagResurrectPoint != 0 {
			returnPoints++
		}
	}
	den, ok := found["STORE_TD_GATE"]
	if !ok {
		t.Fatal("the Bandit Den gate is not in the roster")
	}
	if got := den.TalkFlags & teleportBits; got != simulation.NpcTalkFlagResurrectPoint {
		t.Errorf("den gate teleport bits %#x, want 0x100", got)
	}
	if !den.Services.Has(simulation.NpcServiceResurrectPoint) || den.Services.Has(simulation.NpcServiceTeleport) {
		t.Errorf("den gate services %v, want service 9 without 8", den.Services)
	}
	if returnPoints != 1 {
		t.Errorf("%d gates offer the return point row, want the den gate alone", returnPoints)
	}
	// Ref 2094 is the Jangan town gate of TestPortalStructureGrantAndDestinationTransaction.
	town, ok := byRef[2094]
	if !ok || town.TalkFlags&teleportBits != simulation.NpcTalkFlagRecallPoint|simulation.NpcTalkFlagTeleport {
		t.Errorf("linked town gate bits %#x, want 0xc0", town.TalkFlags&teleportBits)
	}
	for _, npc := range rt.NpcRoster {
		id, ok := rt.portals.sources[npc.RefObjID]
		if !ok {
			continue
		}
		linked := false
		for key := range rt.portals.links {
			linked = linked || key[0] == id
		}
		if linked != (npc.TalkFlags&simulation.NpcTalkFlagTeleport != 0) {
			t.Errorf("%s: linked %v but destination row %v", npc.Codename, linked, !linked)
		}
	}
}

/*
================
TestGateTownReturnMovesToTheAppointedPoint

0x7495 type 1 (the native case 1) lands the player at the appointed return
point, charging nothing.
================
*/
func TestGateTownReturnMovesToTheAppointedPoint(t *testing.T) {
	rt, c := gateTownReturnFixture(t)
	want := rt.appointedRebirth(testDivision, c)
	gold := *c.Gold
	out := sendGateTownReturn(rt, c)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("type 1 did not re-enter: %+v", out)
	}
	if got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}); got.RegionID != want.spawn.RegionID || got.X != want.spawn.X || got.Z != want.spawn.Z {
		t.Fatalf("landed at %+v, want %+v", got, want.spawn)
	}
	if *c.Gold != gold {
		t.Fatal("the return point row charged a fee")
	}
}

/*
================
TestGateTownReturnAppliesTownHead

The linkless gate's return-point row is a town teleport (case 1 runs
the shared town routine): a grey traveller's hostility settles in both
directions and its abnormal state clears on the row, and the player
still lands at the appointed point.
================
*/
func TestGateTownReturnAppliesTownHead(t *testing.T) {
	rt, c := gateTownReturnFixture(t)
	peer := rebirthTestCharacter(20, 100)
	peer.Name = "GateReturnPeer"
	peer.ID = c.ID + 1000
	deps := rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], peer)
	gidC := enterworld.ObjectIDForCharacter(c)
	gidPeer := enterworld.ObjectIDForCharacter(peer)
	c.Aggressions = map[uint32]uint32{gidPeer: playerAggressionTicks}
	peer.Aggressions = map[uint32]uint32{gidC: playerAggressionTicks}
	if c.PVPState() != 1 || peer.PVPState() != 1 {
		t.Fatal("fixture is not mutually grey")
	}
	record := abnormal.Record{Status: abnormal.Panic, Grade: 1, DurationMs: 60000, SourceGID: gidC, Param34: 50}
	rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, rt.Now().UnixMilli())
	if block := rt.playerAbnormal(testDivision, c.Name); block == nil || block.Mask == 0 {
		t.Fatal("abnormal fixture did not take")
	}
	var peerFrames []wire.Frame
	rt.PushCharacterFrames = func(_, name string, f []wire.Frame) {
		if name == peer.Name {
			peerFrames = append(peerFrames, f...)
		}
	}
	rt.PushDivisionPeerFrames = func(_, _ string, f []wire.Frame) {}
	want := rt.appointedRebirth(testDivision, c)
	out := sendGateTownReturn(rt, c)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("type 1 did not re-enter: %+v", out)
	}
	if got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{}); got.RegionID != want.spawn.RegionID || got.X != want.spawn.X {
		t.Fatalf("landed at %+v, want %+v", got, want.spawn)
	}
	if len(c.Aggressions) != 0 || c.PVPState() != 0 {
		t.Fatalf("hostility survives the gate town return: %v state %d", c.Aggressions, c.PVPState())
	}
	if len(peer.Aggressions) != 0 || peer.PVPState() != 0 {
		t.Fatalf("reciprocal record survives the gate town return: %v state %d", peer.Aggressions, peer.PVPState())
	}
	if len(peerFrames) == 0 || peerFrames[0].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("listed opponent was not told its state cleared: %+v", peerFrames)
	}
	if block := rt.playerAbnormal(testDivision, c.Name); block != nil && block.Mask != 0 {
		t.Fatalf("abnormal mask %#x survives the gate town return", block.Mask)
	}
}

/*
================
TestGateTownReturnRollbackKeepsTheHead

The head runs before the move: a use refused inside the move commit
(the character went delete-pending) still settles hostility in both
directions and clears the abnormal state, the head's frames ride the
refusal answer, and the quest event never fires.
================
*/
func TestGateTownReturnRollbackKeepsTheHead(t *testing.T) {
	rt, c := gateTownReturnFixture(t)
	peer := rebirthTestCharacter(20, 100)
	peer.Name = "GateReturnPeer"
	peer.ID = c.ID + 1000
	deps := rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], peer)
	gidC := enterworld.ObjectIDForCharacter(c)
	gidPeer := enterworld.ObjectIDForCharacter(peer)
	c.Aggressions = map[uint32]uint32{gidPeer: playerAggressionTicks}
	peer.Aggressions = map[uint32]uint32{gidC: playerAggressionTicks}
	if c.PVPState() != 1 || peer.PVPState() != 1 {
		t.Fatal("fixture is not mutually grey")
	}
	record := abnormal.Record{Status: abnormal.Panic, Grade: 1, DurationMs: 60000, SourceGID: gidC, Param34: 50}
	rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, rt.Now().UnixMilli())
	if block := rt.playerAbnormal(testDivision, c.Name); block == nil || block.Mask == 0 {
		t.Fatal("abnormal fixture did not take")
	}
	rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, f []wire.Frame) {}
	calls := recordTeleportEvents(rt)
	c.DeletePending = true
	out := sendGateTownReturn(rt, c)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != 0xb495 || !bytes.Equal(out.Frames[0].Payload, []byte{2, 2}) {
		t.Fatalf("rolled-back use answered %+v, want the refusal first", out.Frames)
	}
	settled := false
	for _, frame := range out.Frames {
		settled = settled || frame.Opcode == wire.OpObjectStateRefresh
	}
	if !settled {
		t.Fatalf("the head's frames are missing from the refusal answer: %+v", out.Frames)
	}
	if len(c.Aggressions) != 0 || c.PVPState() != 0 {
		t.Fatalf("hostility survives the rolled-back move: %v state %d", c.Aggressions, c.PVPState())
	}
	if len(peer.Aggressions) != 0 || peer.PVPState() != 0 {
		t.Fatalf("reciprocal record survives the rolled-back move: %v state %d", peer.Aggressions, peer.PVPState())
	}
	if block := rt.playerAbnormal(testDivision, c.Name); block != nil && block.Mask != 0 {
		t.Fatalf("abnormal mask %#x survives the rolled-back move", block.Mask)
	}
	if len(*calls) != 0 {
		t.Fatalf("a rolled-back use raised the event: %v", *calls)
	}
}

/*
================
TestGateTownReturnRefusals

The shared admissions in their native order, then case 1's transport test.
================
*/
func TestGateTownReturnRefusals(t *testing.T) {
	refused := func(t *testing.T, out OpResult, code byte) {
		t.Helper()
		if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, code}) {
			t.Fatalf("got %+v, want refusal %#x", out.Frames, code)
		}
	}
	t.Run("unselected", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		rt.Selected.Clear(testDivision, c.Name)
		refused(t, sendGateTownReturn(rt, c), 2)
	})
	t.Run("trailing bytes", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		refused(t, rt.HandlePortal(testDivision, c, wire.NewWriter(6).U32(2001).U8(gateTownReturn).U8(0).Payload()), 2)
	})
	t.Run("teleporting", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		c.NativeTeleportMode = 1
		refused(t, sendGateTownReturn(rt, c), errCodeTeleportState)
	})
	t.Run("out of range", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		rt.NpcRoster[0].Spawn.X += npcHitRange + 50
		refused(t, sendGateTownReturn(rt, c), hitRangeTooFar)
	})
	// Both failing: the hit range is tested before the service, so a
	// player out of range of a serviceless NPC gets 4, not 3.
	t.Run("out of range and no teleport service", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		rt.NpcRoster[0].Spawn.X += npcHitRange + 50
		rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceShop)
		refused(t, sendGateTownReturn(rt, c), hitRangeTooFar)
	})
	t.Run("no teleport service", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceShop)
		refused(t, sendGateTownReturn(rt, c), errCodeTeleportService)
	})
	t.Run("transport out", func(t *testing.T) {
		rt, c := gateTownReturnFixture(t)
		c.ActiveCOS = &enterworld.CharacterCOS{GID: 0x700001, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true}
		refused(t, sendGateTownReturn(rt, c), errCodeGuideTransport)
	})
}

/*
================
TestDenGateOffersTheReverseReturnToAScrollHolder

The gate inference of reversereturn.go covers a gate whose only row is
the return point.
================
*/
func TestDenGateOffersTheReverseReturnToAScrollHolder(t *testing.T) {
	rt, c, _ := reverseReturnFixture(t)
	den := simulation.NpcDef{TalkFlags: simulation.NpcTalkFlagResurrectPoint}
	if rt.reverseReturnCapability(den, c) != simulation.NpcTalkFlagReverseReturn {
		t.Fatal("the den gate did not offer the reverse return to a scroll holder")
	}
}

/*
================
TestDenEntryRefusesTradersAndHunters

The reference server's case 2 checks the link's TARGET: travelling into
the Bandit Den refuses the quest bit (0x1C17) and a registered trader or
hunter (0x1C18). A thief or a jobless player enters; nothing applies to a
link that leaves elsewhere.
================
*/
func TestDenEntryRefusesTradersAndHunters(t *testing.T) {
	source := portalDestination{code: "GATE_NPC_CH_KISAENG6"}
	link := portalLink{source: 12, target: 13}
	cases := []struct {
		name      string
		job       uint8
		traveller portalTraveller
		want      byte
	}{
		{"jobless into the den", 0, portalTraveller{intoDen: true}, 0},
		{"thief into the den", domain.JobThief, portalTraveller{intoDen: true}, 0},
		{"trader into the den", domain.JobTrader, portalTraveller{intoDen: true}, errCodeDenJob},
		{"hunter into the den", domain.JobHunter, portalTraveller{intoDen: true}, errCodeDenJob},
		{"quest bit into the den", 0, portalTraveller{intoDen: true, questMask: 0x40000}, errCodeGateQuestBlock},
		{"quest bit before the job", domain.JobTrader, portalTraveller{intoDen: true, questMask: 0x40000}, errCodeGateQuestBlock},
		{"trader elsewhere", domain.JobTrader, portalTraveller{}, 0},
		{"quest bit at an NPC gate elsewhere", 0, portalTraveller{questMask: 0x40000}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c, _, _ := returnFixture(t, 30000)
			c.Job.Type = tc.job
			if got := portalAdmission(c, source, link, tc.traveller); got != tc.want {
				t.Fatalf("admission %#x, want %#x", got, tc.want)
			}
		})
	}
}

/*
================
TestDenLinksTargetTheDenGate

Shipped data: both links into the den (from GATE_NPC_CH_KISAENG6 and
GATE_NPC_EU_SOLDIER_PO2) target the teleport the den rule names.
================
*/
func TestDenLinksTargetTheDenGate(t *testing.T) {
	clientdata.RequireGameData(t)
	catalog, err := loadPortalCatalog(gamedatatest.TextdataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	into := 0
	for key := range catalog.links {
		if catalog.destinations[key[1]].code == denGateCode {
			into++
		}
	}
	if into != 2 {
		t.Fatalf("%d links into %s, want the two NPC links", into, denGateCode)
	}
}
