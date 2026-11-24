/*
===========================================================================

sanctum.go - the six sanctums' gates and the schedules that open them

Port-only, not native to v1.150 (SRO_PHARAOH_TEMPLE, #3787). The later
server opens each pair of sanctums on a schedule and lets players in
through gates it places itself; the client's teleportdata has none of
them, so without this table no sanctum can be entered at all.

Evidence, all from the pinned ISRO-R shard backup the temple population
already comes from (SILKROAD_R_SHARD202507231313.bak, sha256 89c6835f…)
and the v1.188 server binaries (research/investigations/server/
unique-encounters/haroeris-seth-chain.md has the trail):

	gates      _RefGameWorldGroup_Config, one GROUP_TEMPLE_* group per
	           sanctum: ENTER_GATE_* (the gate's place), ENTER_GATE_
	           TARGAET_* (where it leads), LEAVE_GATE_* and LEAVE_GATE_
	           TARGAET_*. The table's backup page is read by bytes (the
	           SQL reader refuses its unowned columns); each value was
	           checked against the group's _2 copy.
	schedule   _Schedule_copy1, defines 12-14 (SCHEDULE_DEF_EGYPT_
	           TEMPLE_*): two daily rows each, 7200 s long. The live
	           _Schedule rows (1 s and 61 s) are the operator's tuning,
	           not the game's.
	timeline   SR_ShardManager's CShardSJ_TempleOfHaroerisGate start
	           (0x661d30): with an empty Param it notifies at the start,
	           +1200 s and +1500 s (30, 10 and 5 minutes left) and opens
	           at +1800 s; the window's end closes it. 5400 s of play is
	           INS_TEMPLE_HARO's WorldDurationTime (5400000 ms).
	chain      GameServer CGameSJ_TempleOfHaroerisGate opens Haroeris's
	           sanctum with a one-monster wave whose trigger asks the
	           shard manager to open Seth's (0x7ce6 type 1): Seth's gate
	           opens when Haroeris dies.
	pairs      the other two jobs open both worlds of their pair on one
	           step: CShardSJ_TempleOfSelkisAndNeithGate's open (call
	           0x660241 to 0x6605a0) looks up INS_TEMPLE_SELK (0x6605e6)
	           and INS_TEMPLE_NEIT (0x660652) and opens each (failure
	           logs 0x66093e, 0x660a28) before "now Open" (0x66025f);
	           CShardSJ_TempleOfAnubisAndIsisGate does the same with
	           INS_TEMPLE_ANUB and _ISIS (0x661276, 0x6612e5, 0x66186e).
	           Neither pair has a kill trigger: only Seth waits on one.

This leaf owns the table. It imports only the schedule leaf.

===========================================================================
*/
package pharaohtemple

import (
	"time"

	"opensro.online/server/internal/game/world/schedule"
)

// The schedule defines (_RefScheduleDefine ScheduleDefineIdx) of the three
// sanctum pairs.
const (
	ScheduleSelkisNeith    = 12 // SCHEDULE_DEF_EGYPT_TEMPLE_SELKIS_AND_NEITH
	ScheduleAnubisIsis     = 13 // SCHEDULE_DEF_EGYPT_TEMPLE_ANUBIS_AND_ISIS
	ScheduleHaroerisSeth   = 14 // SCHEDULE_DEF_EGYPT_TEMPLE_HAROERIS_AND_SETH
	scheduleWindowDuration = 7200 * time.Second
)

// The timeline inside a window: the shard manager's start with an empty
// Param. Each notice carries the minutes left until the open.
const (
	// OpenAfter is the time from the window's start to the gates opening.
	OpenAfter = 1800 * time.Second
)

// noticeAfter is when each notice goes out, from the window's start.
var noticeAfter = []time.Duration{0, 1200 * time.Second, 1500 * time.Second}

/*
================
Point

One place in the temple: a dungeon region and a position in it.
================
*/
type Point struct {
	Region  uint16
	X, Y, Z float32
}

/*
================
Sanctum

One sanctum's group: the region it is, its keeper, the schedule define
that opens it, its two gates, and the kind whose death opens it (empty
when the schedule's open does).
================
*/
type Sanctum struct {
	Group       string
	Region      uint16
	SourceWorld int
	Keeper      string
	Schedule    int
	Enter       Point
	EnterTarget Point
	Leave       Point
	LeaveTarget Point
	OpenedBy    string
}

// region16 is a config row's signed region as the wire's u16.
func region16(signed int16) uint16 {
	return uint16(signed)
}

// The Temple's region as the config rows spell it.
const templeRegionSigned int16 = -32752

var sanctums = []Sanctum{
	{
		Group: "GROUP_TEMPLE_SELK", Region: region16(-32758), SourceWorld: 51, Keeper: "MOB_SD_SELKIS",
		Schedule:    ScheduleSelkisNeith,
		Enter:       Point{region16(templeRegionSigned), -8049, 41, 1861},
		EnterTarget: Point{region16(-32758), -8052, 32, 2344},
		Leave:       Point{region16(-32758), -8064, 58, 2150},
		LeaveTarget: Point{region16(templeRegionSigned), -8062, 41, 1803},
	},
	{
		Group: "GROUP_TEMPLE_NEIT", Region: region16(-32757), SourceWorld: 52, Keeper: "MOB_SD_NEITH",
		Schedule:    ScheduleSelkisNeith,
		Enter:       Point{region16(templeRegionSigned), -3085, 67, 3489},
		EnterTarget: Point{region16(-32757), -3630, 58, 3482},
		Leave:       Point{region16(-32757), -3363, 58, 3486},
		LeaveTarget: Point{region16(templeRegionSigned), -3047, 67, 3481},
	},
	{
		Group: "GROUP_TEMPLE_ANUB", Region: region16(-32756), SourceWorld: 53, Keeper: "MOB_SD_ANUBIS",
		Schedule:    ScheduleAnubisIsis,
		Enter:       Point{region16(templeRegionSigned), 292, 41, -2018},
		EnterTarget: Point{region16(-32756), 292, 31, -2438},
		Leave:       Point{region16(-32756), 292, 58, -2249},
		LeaveTarget: Point{region16(templeRegionSigned), 289, 41, -1949},
	},
	{
		Group: "GROUP_TEMPLE_ISIS", Region: region16(-32755), SourceWorld: 54, Keeper: "MOB_SD_ISIS",
		Schedule:    ScheduleAnubisIsis,
		Enter:       Point{region16(templeRegionSigned), 1472, 40, -955},
		EnterTarget: Point{region16(-32755), 1926, 31, -952},
		Leave:       Point{region16(-32755), 1720, 57, -963},
		LeaveTarget: Point{region16(templeRegionSigned), 1384, 41, -950},
	},
	{
		Group: "GROUP_TEMPLE_HARO", Region: region16(-32754), SourceWorld: 55, Keeper: "MOB_SD_HAROERIS",
		Schedule:    ScheduleHaroerisSeth,
		Enter:       Point{region16(templeRegionSigned), 7770, 112, 26},
		EnterTarget: Point{region16(-32754), 8240, 102, 37},
		Leave:       Point{region16(-32754), 7986, 128, 25},
		LeaveTarget: Point{region16(templeRegionSigned), 7696, 112, 26},
	},
	{
		// Seth's enter gate stands in Haroeris's sanctum and opens on his
		// death (the wave trigger Temple_Of_Seth_Create_Msg).
		Group: "GROUP_TEMPLE_SETH", Region: region16(-32753), SourceWorld: 56, Keeper: "MOB_SD_SETH",
		Schedule:    ScheduleHaroerisSeth,
		Enter:       Point{region16(-32754), 9384, 102, 23},
		EnterTarget: Point{region16(-32753), 4, -143, -815},
		Leave:       Point{region16(-32753), 8, -143, -948},
		LeaveTarget: Point{region16(templeRegionSigned), 7696, 112, 26},
		OpenedBy:    "MOB_SD_HAROERIS",
	},
}

// The _Schedule_copy1 rows: each define's two daily starts in order,
// seconds from midnight (shard local time, as the siege and Roc
// schedules).
var scheduleStarts = map[int][]int{
	ScheduleSelkisNeith:  {0, 9*3600 + 30*60},         // rows 152 and 151: 00:00, 09:30
	ScheduleAnubisIsis:   {1*3600 + 30*60, 11 * 3600}, // rows 154 and 153: 01:30, 11:00
	ScheduleHaroerisSeth: {3 * 3600, 12*3600 + 30*60}, // rows 155 and 156: 03:00, 12:30
}

/*
================
Sanctums

The six sanctums, in source world order (51-56).
================
*/
func Sanctums() []Sanctum {
	out := make([]Sanctum, len(sanctums))
	copy(out, sanctums)
	return out
}

/*
================
Windows

A schedule define's daily windows, in start order: nil for a define no
sanctum uses.
================
*/
func Windows(define int) []schedule.Window {
	starts := scheduleStarts[define]
	if len(starts) == 0 {
		return nil
	}
	out := make([]schedule.Window, 0, len(starts))
	for _, start := range starts {
		out = append(out, schedule.Window{Every: schedule.Daily, StartSecond: start, Duration: scheduleWindowDuration})
	}
	return out
}

/*
================
NoticeOffsets

When a window's notices go out, from its start: 30, 10 and 5 minutes
before the open.
================
*/
func NoticeOffsets() []time.Duration {
	out := make([]time.Duration, len(noticeAfter))
	copy(out, noticeAfter)
	return out
}
