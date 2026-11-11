/*
===========================================================================

metrics.go - /transport/metrics snapshots and exact window diffs

One snapshot is the JSON the transport hub serves on /transport/metrics.
Two snapshots of a live server give an exact window: the tick counters
are cumulative since boot, tick_mean_ms is the cumulative mean tick
wall, and tick_zones (the #2824 ledger) carries exact cumulative
milliseconds per zone - so a diff is exact milliseconds per tick with
no sampling and no histogram bucket midpoints anywhere.

===========================================================================
*/

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

/*
================
zoneTotal

One tick_zones row: how many times the zone ran and its summed wall
time, both cumulative since boot.
================
*/
type zoneTotal struct {
	Count   uint64  `json:"count"`
	TotalMs float64 `json:"total_ms"`
}

/*
================
metricsSnapshot

The subset of /transport/metrics a budget window reads.
================
*/
type metricsSnapshot struct {
	TickCount    uint64  `json:"tick_count"`
	TickMeanMs   float64 `json:"tick_mean_ms"`
	LiveSessions int     `json:"live_sessions"`
	Runtime      struct {
		AllocatedBytes uint64 `json:"allocated_bytes"`
		HeapLiveBytes  uint64 `json:"heap_live_bytes"`
	} `json:"runtime"`
	TickZones map[string]zoneTotal `json:"tick_zones"`
	// MonsterLeg is the monster_leg block (#3119): the leg census,
	// cumulative since boot like the tick counters. Absent in
	// captures from before the block existed.
	MonsterLeg map[string]uint64 `json:"monster_leg"`
}

/*
================
window

One measured window between two snapshots: exact ticks, exact tick
wall, and every zone's exact milliseconds per tick over the window.
================
*/
type window struct {
	Ticks          int64
	WallMs         float64
	MeanMsPerTick  float64
	AllocBytesTick float64
	SessionsFrom   int
	SessionsTo     int
	ZoneMsPerTick  map[string]float64
	ZoneCounts     map[string]uint64
	// LegDeltas is the window's monster_leg counter deltas (#3119):
	// count/actors/steps/rechecked/cadence_arms/suspended, rest.<guard>,
	// mode.<bucket>. Empty for pre-#3119 captures.
	LegDeltas map[string]uint64
}

/*
================
loadSnapshot
================
*/
func loadSnapshot(path string) (metricsSnapshot, error) {
	var snap metricsSnapshot
	raw, err := os.ReadFile(path)
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snap, fmt.Errorf("%s: %w", path, err)
	}
	return snap, nil
}

/*
================
snapshotPaths

The snapshot files of a capture directory, in capture order. The
capture half writes metrics-1.json .. metrics-5.json; the coordinator's
hand captures name them m0.json / m1.json. Any root *.json that decodes
with a tick_count is accepted, so a hand-assembled directory works too.
================
*/
func snapshotPaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || name == "budget.json" {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)
	return paths, nil
}

/*
================
loadSnapshots

Every decodable metrics snapshot in the directory, in capture order:
the file order snapshotPaths returns (metrics-1.json .. metrics-5.json,
or a hand capture's m0.json / m1.json). The order is load-bearing -
pickWindows pairs the profile and the trace by their bracketing
snapshots - so snapshots are never re-sorted by tick counter and
never deduplicated: two back-to-back pulls inside one tick share a
counter (metrics-2 and metrics-3 of a capture sit either side of
nothing), and dropping one for the tie would shift every pair after
it onto the whole-capture window (#3103).
================
*/
func loadSnapshots(dir string) ([]metricsSnapshot, error) {
	paths, err := snapshotPaths(dir)
	if err != nil {
		return nil, err
	}
	var snaps []metricsSnapshot
	for _, path := range paths {
		snap, err := loadSnapshot(path)
		if err != nil || snap.TickCount == 0 {
			continue // not a metrics snapshot (budget.json, notes, ...)
		}
		snaps = append(snaps, snap)
	}
	if len(snaps) < 2 {
		return nil, fmt.Errorf("%s: need at least two metrics snapshots, found %d", dir, len(snaps))
	}
	return snaps, nil
}

/*
================
diffWindow

The exact window between two cumulative snapshots. The window's tick
wall is the difference of the cumulative sums (mean x count), never a
histogram midpoint; per-zone time is the difference of the ledger's
exact totals. A zone that appears or disappears mid-window diffs
against zero.
================
*/
func diffWindow(from, to metricsSnapshot) (window, error) {
	var w window
	w.Ticks = int64(to.TickCount) - int64(from.TickCount)
	if w.Ticks <= 0 {
		return w, fmt.Errorf("snapshot tick_count did not advance (%d -> %d)", from.TickCount, to.TickCount)
	}
	w.WallMs = to.TickMeanMs*float64(to.TickCount) - from.TickMeanMs*float64(from.TickCount)
	w.MeanMsPerTick = w.WallMs / float64(w.Ticks)
	w.AllocBytesTick = float64(to.Runtime.AllocatedBytes-from.Runtime.AllocatedBytes) / float64(w.Ticks)
	w.SessionsFrom = from.LiveSessions
	w.SessionsTo = to.LiveSessions
	w.ZoneMsPerTick = make(map[string]float64)
	w.ZoneCounts = make(map[string]uint64)
	keys := make(map[string]bool)
	for key := range from.TickZones {
		keys[key] = true
	}
	for key := range to.TickZones {
		keys[key] = true
	}
	for key := range keys {
		deltaMs := to.TickZones[key].TotalMs - from.TickZones[key].TotalMs
		w.ZoneMsPerTick[key] = deltaMs / float64(w.Ticks)
		w.ZoneCounts[key] = to.TickZones[key].Count - from.TickZones[key].Count
	}
	w.LegDeltas = make(map[string]uint64)
	for key, toValue := range to.MonsterLeg {
		// Cumulative counters only grow within one boot; a key that
		// somehow went backwards contributes nothing rather than a
		// wrapped delta.
		if delta := toValue - from.MonsterLeg[key]; toValue >= from.MonsterLeg[key] && delta != 0 {
			w.LegDeltas[key] = delta
		}
	}
	return w, nil
}

/*
================
pickWindows

Which snapshot pairs bracket the CPU profile and the trace, in
capture (file) order: a capture directory holds five snapshots - the
profile between the first pair (metrics-1, metrics-2), the trace
between the second (metrics-3, metrics-4) - so each source scales by
its own bracketing pair, never by the whole capture (#3103). A hand
capture holds one pair for everything; the zone table always spans
the full first-to-last window.
================
*/
func pickWindows(snaps []metricsSnapshot) (overall, cpuWin, traceWin window, err error) {
	overall, err = diffWindow(snaps[0], snaps[len(snaps)-1])
	if err != nil {
		return
	}
	cpuWin, traceWin = overall, overall
	if len(snaps) >= 5 {
		if cpuWin, err = diffWindow(snaps[0], snaps[1]); err != nil {
			return
		}
		if traceWin, err = diffWindow(snaps[2], snaps[3]); err != nil {
			return
		}
	}
	return overall, cpuWin, traceWin, nil
}
