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

Every decodable metrics snapshot in the directory, sorted by the
cumulative tick counter so capture order survives any file naming.
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
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].TickCount < snaps[j].TickCount })
	// Two pulls inside one tick share a counter; keep the later file.
	deduped := snaps[:0]
	for i, snap := range snaps {
		if i+1 < len(snaps) && snaps[i+1].TickCount == snap.TickCount {
			continue
		}
		deduped = append(deduped, snap)
	}
	if len(deduped) < 2 {
		return nil, fmt.Errorf("%s: need at least two metrics snapshots with distinct tick_count, found %d", dir, len(deduped))
	}
	return deduped, nil
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
	return w, nil
}

/*
================
pickWindows

Which snapshot pairs bracket the CPU profile and the trace. A capture
directory holds five snapshots - profile between the first pair, trace
between the second - while a hand capture holds one pair for
everything; the zone table always spans the full first-to-last window.
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
