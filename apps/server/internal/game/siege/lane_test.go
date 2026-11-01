package siege

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
laneFixture
================
*/
func laneFixture(t *testing.T) (*Lane, *fortress.Authority, *time.Location) {
	t.Helper()
	loc := time.FixedZone("shard", 8*3600)
	authority := fortress.New([]fortress.Catalog{{ID: 1, CodeName: "FORTRESS_JANGAN", MaxEntrance: 300}})
	lane, err := NewLane(LaneConfig{Division: "d", Fortresses: authority, Location: loc,
		GuildName: func(int64) string { return "Owners" }})
	if err != nil {
		t.Fatal(err)
	}
	return lane, authority, loc
}

/*
================
subtypes
================
*/
func subtypes(out []simulation.DivisionFrames) []uint8 {
	var got []uint8
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode == OpFortressWarState {
				got = append(got, frame.Payload[0])
			}
		}
	}
	return got
}

/*
================
TestLaneRunsAWarWednesday

From Wednesday morning (request and tax periods already running) through
the war: the 30-minute warning, the begin, the four end warnings and the
end, each once, with the war period following the schedule.
================
*/
func TestLaneRunsAWarWednesday(t *testing.T) {
	lane, authority, loc := laneFixture(t)
	at := func(hour, minute, second int) int64 {
		return time.Date(2026, 10, 7, hour, minute, second, 0, loc).UnixMilli()
	}
	if got := subtypes(lane.Tick(at(9, 0, 0))); !bytes.Equal(got, []byte{0x33, 0x31}) {
		t.Fatalf("boot inside the request and tax periods sent %x", got)
	}
	var warChanges []bool
	lane.config.WarChanged = func(_ int64, active bool) []simulation.DivisionFrames {
		warChanges = append(warChanges, active)
		return nil
	}
	var sent []uint8
	for second := at(19, 0, 0); second <= at(21, 31, 0); second += 1000 {
		sent = append(sent, subtypes(lane.Tick(second))...)
		if second == at(20, 30, 0) && !authority.WarActive("d") {
			t.Fatal("war period not set during the war")
		}
	}
	if want := []byte{1, 2, 3, 4, 5, 9, 6}; !bytes.Equal(sent, want) {
		t.Fatalf("war evening sent %x, want %x", sent, want)
	}
	if authority.WarActive("d") || len(warChanges) != 2 || !warChanges[0] || warChanges[1] {
		t.Fatalf("war period after the war: active=%v changes=%v", authority.WarActive("d"), warChanges)
	}
	if got := subtypes(lane.Tick(at(23, 59, 59))); !bytes.Equal(got, []byte{0x34, 0x32}) {
		t.Fatalf("day end sent %x", got)
	}
}

/*
================
TestFortressListMatchesTheClientReadOrder

Client 76C870 case 0: u8 count; per fortress u32 id, sized name, four u32, u8
[+u32], u8 [+u32]; u8 period flags; u32 guild fortress.
================
*/
func TestFortressListMatchesTheClientReadOrder(t *testing.T) {
	lane, authority, _ := laneFixture(t)
	authority.SetPeriod("d", fortress.PeriodRequest, true)
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	want := append([]byte{0, 1}, u32(1)...)
	want = append(want, 0, 0)
	for range 4 {
		want = append(want, u32(0)...)
	}
	want = append(want, 0, 0, byte(fortress.PeriodRequest))
	want = append(want, u32(0)...)
	if got := lane.FortressList(9); !bytes.Equal(got, want) {
		t.Fatalf("list\n got %x\nwant %x", got, want)
	}
	if got := lane.WarGuilds(fortress.Record{ID: 1}); !bytes.Equal(got, append(append([]byte{0x10}, u32(1)...), 0)) {
		t.Fatalf("empty war guild registry %x", got)
	}
}

/*
================
TestWindowsFollowTheWarSchedule

The calendar's windows come from the schedules that drive the war: on war
Wednesday morning registration is running and the war is next, starting
where WarStart says; during the war it is running.
================
*/
func TestWindowsFollowTheWarSchedule(t *testing.T) {
	lane, _, loc := laneFixture(t)
	morning := time.Date(2026, 10, 7, 9, 0, 0, 0, loc).UnixMilli()
	byName := func(nowMs int64) map[string]Window {
		out := map[string]Window{}
		for _, window := range lane.Windows(nowMs) {
			out[window.Name] = window
		}
		return out
	}
	windows := byName(morning)
	request, war := windows["AllowSiegeRequest"], windows["SiegeProgressing"]
	if !request.Running || war.Running || !war.Start.Equal(lane.WarStart(morning)) || !war.End.After(war.Start) {
		t.Fatalf("morning windows %+v", windows)
	}
	during := war.Start.Add(30 * time.Minute).UnixMilli()
	if !byName(during)["SiegeProgressing"].Running {
		t.Fatalf("the war is not running at %v: %+v", time.UnixMilli(during).In(loc), byName(during))
	}
	if (*Lane)(nil).Windows(morning) != nil {
		t.Fatal("a nil lane listed windows")
	}
}
