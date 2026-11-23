/*
===========================================================================

tombgate_test.go - the Sarin gate admits only in its window, and says so

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// tombGateTestHour is an hour the tests open and close the gate in.
var tombGateTestHour = time.Date(2026, 11, 23, 14, 0, 0, 0, time.UTC)

/*
================
TestTombGateAdmitsOnlyInsideItsWindow

The gate catalogue's first city gate, renamed the Sarin gate: at
14:20 (window closed) a trip through it is refused and costs nothing;
at 14:05 (open) the same trip enters. Another gate is never refused
by the schedule.
================
*/
func TestTombGateAdmitsOnlyInsideItsWindow(t *testing.T) {
	clientdata.RequireGameData(t)
	rt, c, clock, _ := returnFixture(t, 30000)
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 252094, RefObjID: 2094, Codename: "STORE_CH", Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25, FortressID: 1}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	rt.ConfigureTombGate(true)
	rt.tombGate.loc = time.UTC
	source := rt.portals.destinations[1]
	source.code = tombGateSource
	rt.portals.destinations[1] = source
	travel := func() OpResult {
		rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(252094).Payload())
		return rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(252094).U8(2).U32(2).Payload())
	}
	entered := func(out OpResult) bool {
		return len(out.Frames) > 0 && out.Frames[0].Opcode == enterworld.OpcodeResetClient
	}
	clock.now = tombGateTestHour.Add(20 * time.Minute)
	gold := *c.Gold
	if out := travel(); entered(out) || *c.Gold != gold {
		t.Fatalf("the shut gate let a traveller through: %+v", out)
	}
	clock.now = tombGateTestHour.Add(5 * time.Minute)
	if out := travel(); !entered(out) {
		t.Fatalf("the open gate refused: %+v", out)
	}
	rt.ConfigureTombGate(false)
	if rt.tombGateShut(tombGateSource) {
		t.Fatal("with the tomb off the schedule still shuts the gate")
	}
}

/*
================
TestTombGateAnnouncesEachEdgeOnce

Every division hears 0x3058 kind 0x10 when the window opens and 0x11
when it closes, once per edge. A boot outside the window says nothing;
a boot inside it announces the opening, as the native executor starts
a current job.
================
*/
func TestTombGateAnnouncesEachEdgeOnce(t *testing.T) {
	c := instanceWorldTestCharacter(41, "Delver")
	rt, clock := newInstanceWorldRuntime(t, c)
	admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)
	rt.ConfigureTombGate(true)
	rt.tombGate.loc = time.UTC
	tick := func(at time.Duration) []byte {
		clock.now = tombGateTestHour.Add(at)
		var kinds []byte
		for _, out := range rt.advanceTombGate(clock.now.UnixMilli()) {
			for _, frame := range out.Frames {
				if frame.Opcode == opEventNotice {
					kinds = append(kinds, frame.Payload[0])
				}
			}
		}
		return kinds
	}
	steps := []struct {
		at   time.Duration
		want []byte
	}{
		{-30 * time.Second, nil},                                     // boots shut: silence
		{time.Second, []byte{tombGateKindOpened}},                    // 14:00:01 opens
		{5 * time.Minute, nil},                                       // still open
		{10*time.Minute + 2*time.Second, []byte{tombGateKindClosed}}, // 14:10:01 closed
		{30 * time.Minute, nil},                                      // still shut
		{time.Hour + time.Second, []byte{tombGateKindOpened}},        // 15:00:01
	}
	for _, step := range steps {
		if got := tick(step.at); string(got) != string(step.want) {
			t.Fatalf("at +%v: kinds %x, want %x", step.at, got, step.want)
		}
	}
	rt.ConfigureTombGate(true)
	rt.tombGate.loc = time.UTC
	if got := tick(time.Hour + 3*time.Minute); string(got) != string([]byte{tombGateKindOpened}) {
		t.Fatalf("a boot inside the window announced %x", got)
	}
}
