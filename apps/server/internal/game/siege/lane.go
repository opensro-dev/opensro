/*
===========================================================================

lane.go - the fortress-war lane: schedule edges, periods and the login list

The lane is the ShardManager-and-GameServer pair of the fortress war folded
into one owner. Each tick it evaluates the shard's schedules; on an edge it
does what CSiegeFortressMgr_OnShardMessage (62EE90) does for that subtype
and relays the subtype to every player:

	AllowSiegeRequest  start 0x33 / end 0x34   request period
	AllowSiegeTaxJob   start 0x31 / end 0x32   tax period
	SiegeProgressing   start 2    / end 6      war mode
	AlertSiegeStartAfter30Min            1
	AlertSiegeFinishAfter30/20/10/1Min   3, 4, 5, 9

Every player entering the world receives the fortress list (subtype 0) and
the war's registered guilds (subtype 0x10). The fortress state itself is
the fortress authority's; the lane only drives its periods.

===========================================================================
*/
package siege

import (
	"fmt"
	"sync"
	"time"

	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
scheduleEdge

What one schedule does on its edges. A zero subtype sends nothing.
================
*/
type scheduleEdge struct {
	name       string
	period     fortress.Period
	start, end uint8
}

// edgeOrder is the order edges are applied when several land in one tick:
// the periods before the alerts, so an alert reads the period it follows.
var edgeOrder = []scheduleEdge{
	{name: "AllowSiegeRequest", period: fortress.PeriodRequest, start: SubtypeRequestPeriodOpen, end: SubtypeRequestPeriodEnd},
	{name: "AllowSiegeTaxJob", period: fortress.PeriodTax, start: SubtypeTaxPeriodBegin, end: SubtypeTaxPeriodEnd},
	{name: "SiegeProgressing", period: fortress.PeriodWar, start: SubtypeWarBegin, end: SubtypeWarEnd},
	{name: "AlertSiegeStartAfter30Min", start: SubtypeWarSoon},
	{name: "AlertSiegeFinishAfter30Min", start: SubtypeWarEnds30},
	{name: "AlertSiegeFinishAfter20Min", start: SubtypeWarEnds20},
	{name: "AlertSiegeFinishAfter10Min", start: SubtypeWarEnds10},
	{name: "AlertSiegeFinishAfter1Min", start: SubtypeWarEnds1},
}

/*
================
LaneConfig
================
*/
type LaneConfig struct {
	Division   string
	Fortresses *fortress.Authority
	Schedules  map[string]Schedule
	Location   *time.Location
	// GuildName names an occupying guild for the list; empty when unknown.
	GuildName func(guildID int64) string
	// WarChanged runs after the war period turns on or off, for the
	// fortress worlds' own war phases.
	WarChanged func(nowMs int64, active bool) []simulation.DivisionFrames
}

/*
================
Lane
================
*/
type Lane struct {
	config LaneConfig
	mu     sync.Mutex
	active map[string]bool
}

/*
================
NewLane

The shipped schedule unless the config names one. Every edge schedule must
exist: a schedule.cfg missing one would silently drop a war phase.
================
*/
func NewLane(config LaneConfig) (*Lane, error) {
	if config.Fortresses == nil || config.Division == "" {
		return nil, fmt.Errorf("siege: lane needs a division and a fortress authority")
	}
	if config.Location == nil {
		config.Location = time.Local
	}
	if config.Schedules == nil {
		parsed, err := ParseSchedules(shippedSchedule, config.Location)
		if err != nil {
			return nil, fmt.Errorf("siege: shipped schedule: %w", err)
		}
		config.Schedules = parsed
	}
	for _, edge := range edgeOrder {
		if _, ok := config.Schedules[edge.name]; !ok {
			return nil, fmt.Errorf("siege: schedule %s is missing", edge.name)
		}
	}
	return &Lane{config: config, active: map[string]bool{}}, nil
}

/*
================
Tick

Applies every schedule edge between the last tick and now. The first tick
starts whatever is already running, as a shard booting mid-period does.
================
*/
func (l *Lane) Tick(nowMs int64) []simulation.DivisionFrames {
	if l == nil {
		return nil
	}
	now := time.UnixMilli(nowMs).In(l.config.Location)
	var frames []simulation.Frame
	var warChanged, warActive bool
	l.mu.Lock()
	for _, edge := range edgeOrder {
		running := l.config.Schedules[edge.name].Active(now)
		if running == l.active[edge.name] {
			continue
		}
		l.active[edge.name] = running
		if edge.period != 0 && l.config.Fortresses.SetPeriod(l.config.Division, edge.period, running) &&
			edge.period == fortress.PeriodWar {
			warChanged, warActive = true, running
		}
		subtype := edge.end
		if running {
			subtype = edge.start
		}
		if subtype != 0 {
			frames = append(frames, simulation.Frame{Opcode: OpFortressWarState, Payload: EncodeEdge3887(subtype)})
		}
	}
	l.mu.Unlock()
	var out []simulation.DivisionFrames
	if len(frames) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: l.config.Division, Frames: frames})
	}
	if warChanged && l.config.WarChanged != nil {
		out = append(out, l.config.WarChanged(nowMs, warActive)...)
	}
	return out
}

/*
================
FortressList

The subtype-0 list for one player: every fortress with its occupying guild,
the period flags, and the fortress the player's guild holds or applied to.
================
*/
func (l *Lane) FortressList(guildID int64) []byte {
	records := l.config.Fortresses.Records(l.config.Division)
	rows := make([]FortressRow, 0, len(records))
	var guildFortress uint32
	for _, record := range records {
		row := FortressRow{FortressID: record.ID}
		if record.GuildID != 0 {
			if l.config.GuildName != nil {
				row.OwnerName = l.config.GuildName(record.GuildID)
			}
			row.Discarded[0] = uint32(record.GuildID)
		}
		rows = append(rows, row)
		_, applied := record.Applicants[guildID]
		if guildID != 0 && (record.GuildID == guildID || applied) {
			guildFortress = record.ID
		}
	}
	return EncodeFortressList3887(rows, uint8(l.config.Fortresses.Periods(l.config.Division)), guildFortress)
}

/*
================
WarGuilds

The subtype-0x10 registry for a fortress: its occupying guild and its
applicants.
================
*/
func (l *Lane) WarGuilds(record fortress.Record) []byte {
	var ids []uint32
	if record.GuildID != 0 {
		ids = append(ids, uint32(record.GuildID))
	}
	for guild := range record.Applicants {
		if guild != record.GuildID {
			ids = append(ids, uint32(guild))
		}
	}
	return EncodeWarGuildRegistry3887(record.ID, ids)
}

/*
================
WorldBound

The fortress state an entering player starts from.
================
*/
func (l *Lane) WorldBound(s *transport.Session, guildID int64) {
	if l == nil || s == nil {
		return
	}
	_ = s.Send(OpFortressWarState, l.FortressList(guildID))
	for _, record := range l.config.Fortresses.Records(l.config.Division) {
		_ = s.Send(OpFortressWarState, l.WarGuilds(record))
	}
}

/*
================
WarStart

The start of the war running now or next (MainProcess +0x42438, set from
the shard's war edges), in shard time.
================
*/
func (l *Lane) WarStart(nowMs int64) time.Time {
	start, _, _ := l.config.Schedules["SiegeProgressing"].Window(time.UnixMilli(nowMs).In(l.config.Location))
	return start
}
