/*
===========================================================================

winter_gauge_test.go - the 0x34F2 messages the winter missions send

The gauge rows only (30001): the full row at world entry, the count every
five minutes when it moved, the completion, the window's start and end.
Fixtures from winter_missions_test.go.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
gaugeFrames

The 0x34F2 frames a drain sent to the test division.
================
*/
func gaugeFrames(t *testing.T, out []simulation.DivisionFrames) []simulation.Frame {
	t.Helper()
	var frames []simulation.Frame
	for _, batch := range out {
		if batch.DivisionID != testDivision {
			t.Fatalf("frames for division %q", batch.DivisionID)
		}
		for _, f := range batch.Frames {
			if f.Opcode == wire.OpServerEvent {
				frames = append(frames, f)
			}
		}
	}
	return frames
}

/*
================
expectGauge
================
*/
func expectGauge(t *testing.T, name string, got []simulation.Frame, want ...wire.Frame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d frames, want %d", name, len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Fatalf("%s: frame %d % X, want % X", name, i, got[i].Payload, want[i].Payload)
		}
	}
}

/*
================
snowFlakeRow

The division's 30001 row as the messages carry it.
================
*/
func snowFlakeRow(t *testing.T, rt *Runtime) wire.ServerEventRow {
	t.Helper()
	for _, row := range rt.WinterMissionProgress(testDivision) {
		if row.ID == progression.WinterMissionSnowFlake {
			return wire.ServerEventRow{ID: row.ID, Count: uint32(row.Count), Goal: uint32(row.Goal)}
		}
	}
	t.Fatal("no Snow Flake Event row")
	return wire.ServerEventRow{}
}

/*
================
TestWinterGaugeEntryFullSync

A character entering while the event runs gets the Snow Flake Event row,
even at zero, and no row of the missions without a gauge.
================
*/
func TestWinterGaugeEntryFullSync(t *testing.T) {
	c := testCharacter()
	rt := winterMissionRuntime(t, c, &memoryWinterStore{})
	want := wire.ServerEventFullSyncFrame([]wire.ServerEventRow{snowFlakeRow(t, rt)})
	var got []simulation.Frame
	for _, p := range rt.ChristmasEntryFrames(testDivision, c) {
		if p.NativeOpcode == wire.OpServerEvent {
			got = append(got, simulation.Frame{Opcode: p.NativeOpcode, Payload: p.Payload})
		}
	}
	expectGauge(t, "entry", got, want)
}

/*
================
TestWinterGaugeEntryOff

The event off sends no gauge at world entry.
================
*/
func TestWinterGaugeEntryOff(t *testing.T) {
	c := testCharacter()
	rt := christmasLootRuntime(t, c, false)
	for _, p := range rt.ChristmasEntryFrames(testDivision, c) {
		if p.NativeOpcode == wire.OpServerEvent {
			t.Fatal("a gauge frame with the event off")
		}
	}
}

/*
================
TestWinterGaugeSyncEveryFiveMinutes

The first look sets the baseline; a moved count goes out at the five
minute mark and not before; an unmoved count sends nothing.
================
*/
func TestWinterGaugeSyncEveryFiveMinutes(t *testing.T) {
	c := testCharacter()
	rt := winterMissionRuntime(t, c, &memoryWinterStore{})
	t0 := rt.Now()
	rt.syncWinterGauge(testDivision, t0)
	rt.recordWinterMission(testDivision, progression.WinterTriggerExchange, 3, c.Name, t0)
	rt.syncWinterGauge(testDivision, t0.Add(winterGaugeSyncEvery-time.Second))
	expectGauge(t, "before five minutes", gaugeFrames(t, rt.drainWinterGauge(0)))
	rt.syncWinterGauge(testDivision, t0.Add(winterGaugeSyncEvery))
	expectGauge(t, "five minutes", gaugeFrames(t, rt.drainWinterGauge(0)),
		wire.ServerEventCountSyncFrame([]wire.ServerEventRow{snowFlakeRow(t, rt)}))
	rt.syncWinterGauge(testDivision, t0.Add(2*winterGaugeSyncEvery))
	expectGauge(t, "unmoved", gaugeFrames(t, rt.drainWinterGauge(0)))
}

/*
================
TestWinterGaugeCompletionFrame

The Snow Flake Event's reward tells every player the carried count, the
next goal and the hour of +20%.
================
*/
func TestWinterGaugeCompletionFrame(t *testing.T) {
	c := testCharacter()
	rt := winterMissionRuntime(t, c, &memoryWinterStore{})
	goal := snowFlakeRow(t, rt).Goal
	rt.recordWinterMission(testDivision, progression.WinterTriggerExchange, int(goal)+2, c.Name, rt.Now())
	at := rt.Now().Add(progression.WinterMissionRewardDelay)
	due := rt.dueWinterRewards(at)
	if len(due) != 1 {
		t.Fatalf("due rewards %+v", due)
	}
	rt.payWinterReward(due[0], at)
	row := snowFlakeRow(t, rt)
	if row.Count != 2 {
		t.Fatalf("carried count %d, want 2", row.Count)
	}
	expectGauge(t, "completion", gaugeFrames(t, rt.drainWinterGauge(0)), wire.ServerEventCompletionFrame(row,
		wire.ServerEventExpUp{RewardID: winterExpUpRewardID, ApplySeconds: 3600, Exp: 0.2, Sp: 0.2}))
}

/*
================
TestWinterGaugeStartEnd

The window closing sends the end; opening sends the start with the goal.
================
*/
func TestWinterGaugeStartEnd(t *testing.T) {
	c := testCharacter()
	rt := winterMissionRuntime(t, c, &memoryWinterStore{})
	nowMs := rt.Now().UnixMilli()
	rt.winterWindowChanged(testDivision, false, nowMs)
	expectGauge(t, "end", gaugeFrames(t, rt.drainWinterGauge(0)), wire.ServerEventEndFrame(progression.WinterMissionSnowFlake))
	rt.winterWindowChanged(testDivision, true, nowMs)
	expectGauge(t, "start", gaugeFrames(t, rt.drainWinterGauge(0)), wire.ServerEventStartFrame(snowFlakeRow(t, rt)))
}
