/*
===========================================================================

winter_gauge.go - the winter missions' 0x34F2 gauge messages

The v1.188 game server tells its clients how the shard's gauge missions
stand (wire.OpServerEvent): the rows at world entry, the moved counts
every five minutes, each completion with its reward, and the event's
start and end. Only missions whose
_RefServerEvent row sets the gauge flag are sent (30001, the Snow Flake
Event): the later client's CNIFServerEvent holds one mission and every
row it is given replaces the last (client 0x0080F4E0), so a row without a
gauge would take the window over. The v1.150 client has no reader; the
port's opt-in gauge reads them.

The frames are queued here by the tick's advanceWinterMissions and the
reward payout, and leave through drainWinterGauge, a frames step.

===========================================================================
*/

package action

import (
	"fmt"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

// winterGaugeSyncEvery is the native count broadcast's period: the
// reference server sends the gauge events whose data moved every 300 s.
const winterGaugeSyncEvery = 300 * time.Second

// winterExpUpRewardID is the Snow Flake Event's reward row
// (_RefServerEventReward_ExpUPForPlayers 0x1ED: 3600 s, 20% / 20%).
const winterExpUpRewardID uint32 = 0x1ED

/*
================
winterGaugeRows

The rows the gauge shows: the missions with the gauge flag.
================
*/
func winterGaugeRows(rows []progression.WinterMissionState) []wire.ServerEventRow {
	var out []wire.ServerEventRow
	for _, row := range rows {
		if row.Gauge {
			out = append(out, wire.ServerEventRow{ID: row.ID, Count: uint32(row.Count), Goal: uint32(row.Goal)})
		}
	}
	return out
}

/*
================
Runtime.queueWinterGauge

Queues frames for every player of the division.
================
*/
func (rt *Runtime) queueWinterGauge(division string, frames ...wire.Frame) {
	if len(frames) == 0 {
		return
	}
	batch := simulation.DivisionFrames{DivisionID: division, Frames: make([]simulation.Frame, 0, len(frames))}
	for _, f := range frames {
		batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
	}
	w := &rt.winterMissions
	w.mu.Lock()
	w.gaugeOut = append(w.gaugeOut, batch)
	w.mu.Unlock()
}

/*
================
Runtime.drainWinterGauge

The tick step that sends the queued gauge frames.
================
*/
func (rt *Runtime) drainWinterGauge(int64) []simulation.DivisionFrames {
	w := &rt.winterMissions
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.gaugeOut
	w.gaugeOut = nil
	return out
}

/*
================
Runtime.winterGaugeEntryFrame

The full rows a character entering the world gets; ok is false when the
division has no gauge mission.
================
*/
func (rt *Runtime) winterGaugeEntryFrame(division string) (wire.Frame, bool) {
	rows := winterGaugeRows(rt.WinterMissionProgress(division))
	if len(rows) == 0 {
		return wire.Frame{}, false
	}
	return wire.ServerEventFullSyncFrame(rows), true
}

/*
================
Runtime.syncWinterGauge

Every winterGaugeSyncEvery, the gauge rows whose count moved since the
last broadcast go to the division. The first look only sets the
baseline: a character present then got the rows at its world entry.
================
*/
func (rt *Runtime) syncWinterGauge(division string, now time.Time) {
	rows := winterGaugeRows(rt.WinterMissionProgress(division))
	w := &rt.winterMissions
	w.mu.Lock()
	state := w.divisions[division]
	if state == nil {
		w.mu.Unlock()
		return
	}
	first := state.gaugeSyncedAt.IsZero()
	if !first && now.Sub(state.gaugeSyncedAt) < winterGaugeSyncEvery {
		w.mu.Unlock()
		return
	}
	state.gaugeSyncedAt = now
	if state.gaugeSynced == nil {
		state.gaugeSynced = map[uint32]uint32{}
	}
	var moved []wire.ServerEventRow
	for _, row := range rows {
		if !first && state.gaugeSynced[row.ID] != row.Count {
			moved = append(moved, row)
		}
		state.gaugeSynced[row.ID] = row.Count
	}
	w.mu.Unlock()
	if len(moved) > 0 {
		rt.queueWinterGauge(division, wire.ServerEventCountSyncFrame(moved))
	}
}

/*
================
Runtime.queueWinterGaugeCompletion

A gauge mission's reward was paid: every player learns the carried
count, the next goal and the bonus. The sent count is the next sync's
baseline.
================
*/
func (rt *Runtime) queueWinterGaugeCompletion(division string, id uint32) {
	var frames []wire.Frame
	for _, row := range winterGaugeRows(rt.WinterMissionProgress(division)) {
		if row.ID != id {
			continue
		}
		frames = append(frames, wire.ServerEventCompletionFrame(row, wire.ServerEventExpUp{
			RewardID:     winterExpUpRewardID,
			ApplySeconds: uint32(progression.WinterExpUpDuration / time.Second),
			Exp:          float32(progression.WinterExpUpPercent) / 100,
			Sp:           float32(progression.WinterExpUpPercent) / 100,
		}))
		rt.markWinterGaugeCount(division, row)
	}
	rt.queueWinterGauge(division, frames...)
}

/*
================
Runtime.winterWindowChanged

The event's window opened or closed: the native start and end lines for
every mission, and the gauge's start (the window opens at zero towards
the goal) or end (the window closes).
================
*/
func (rt *Runtime) winterWindowChanged(division string, active bool, nowMs int64) {
	line := winterNoticeEnd
	if active {
		line = winterNoticeStart
	}
	rows := rt.WinterMissionProgress(division)
	for _, row := range rows {
		rt.announce(division, announceWinterMission, fmt.Sprintf(line, progression.WinterMissionName(row.ID)), wire.NoticeLink{}, 0, nowMs)
	}
	var frames, held []wire.Frame
	for _, row := range winterGaugeRows(rows) {
		if !active {
			frames = append(frames, wire.ServerEventEndFrame(row.ID))
			continue
		}
		frames = append(frames, wire.ServerEventStartFrame(row))
		// The start sets every window to zero (client 0x00CFA790 type 5);
		// a count the division already holds (a restart, an earlier
		// window) follows at once and becomes the next sync's baseline.
		if row.Count != 0 {
			held = append(held, wire.ServerEventCountSyncFrame([]wire.ServerEventRow{row}))
		}
		rt.markWinterGaugeSynced(division, row, time.UnixMilli(nowMs))
	}
	rt.queueWinterGauge(division, append(frames, held...)...)
}

/*
================
Runtime.markWinterGaugeCount

Records a count the clients now hold without moving the sync's clock.
================
*/
func (rt *Runtime) markWinterGaugeCount(division string, row wire.ServerEventRow) {
	w := &rt.winterMissions
	w.mu.Lock()
	defer w.mu.Unlock()
	if state := w.divisions[division]; state != nil && state.gaugeSynced != nil {
		state.gaugeSynced[row.ID] = row.Count
	}
}

/*
================
Runtime.markWinterGaugeSynced

Records a row the clients now hold, so the next sync compares with it.
================
*/
func (rt *Runtime) markWinterGaugeSynced(division string, row wire.ServerEventRow, now time.Time) {
	w := &rt.winterMissions
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.divisions[division]
	if state == nil {
		return
	}
	if state.gaugeSynced == nil {
		state.gaugeSynced = map[uint32]uint32{}
	}
	state.gaugeSynced[row.ID] = row.Count
	state.gaugeSyncedAt = now
}
