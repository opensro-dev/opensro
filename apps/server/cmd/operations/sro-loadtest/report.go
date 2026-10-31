/*
===========================================================================

report.go - the step's tick window, in the #2003 reader's arithmetic

Each step brackets its bots with two scrapes of the GameWorld's
/transport/metrics. This file turns the pair into the step's tick
window - mean, p50, p90, p99, p99.9, overruns, the per-phase split and
the allocation read - with exactly the math scripts/analysis/
tick_acceptance.py applies to the same snapshots, so a staircase
report and an acceptance re-measure of the same window agree bucket
for bucket. The Python reader's self-test fixtures are the Go tests'
oracle (report_test.go).

A window that cannot be read (the GameWorld restarted between scrapes,
the histogram recorded nothing) is reported as an error on the step,
never as zeros: a quiet failure here would print green numbers the
server never produced.

===========================================================================
*/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/releaseprotocol"
)

// metricsSnapshot is the subset of /transport/metrics the window reads.
// Field names are the server's served JSON (transport.Metrics).
type metricsSnapshot struct {
	TickCount    uint64  `json:"tick_count"`
	TickMeanMs   float64 `json:"tick_mean_ms"`
	TickOverruns uint64  `json:"tick_overruns"`
	LiveSessions int     `json:"live_sessions"`
	TickMsHist   struct {
		BoundsMs []float64 `json:"bounds_ms"`
		Counts   []uint64  `json:"counts"`
	} `json:"tick_ms_histogram"`
	TickPhaseMs   map[string]phaseHistogram `json:"tick_phase_ms"`
	SlowHooks     map[string]slowCount      `json:"slow_hooks"`
	SlowDivisions map[string]slowCount      `json:"slow_divisions"`
	TickSpikes    []spikeRow                `json:"tick_spikes"`
	Runtime       struct {
		AllocatedBytes uint64 `json:"allocated_bytes"`
		HeapLiveBytes  uint64 `json:"heap_live_bytes"`
		GCCycles       uint64 `json:"gc_cycles"`
		Goroutines     uint64 `json:"goroutines"`
	} `json:"runtime"`
}

// phaseHistogram is one tick_phase_ms entry (platform/latency.Histogram:
// "buckets", where the tick histogram serves "counts").
type phaseHistogram struct {
	BoundsMs []float64 `json:"bounds_ms"`
	Buckets  []uint64  `json:"buckets"`
	MaxMs    float64   `json:"max_ms"`
}

// slowCount is one slow_hooks / slow_divisions entry.
type slowCount struct {
	Count uint64  `json:"count"`
	MaxMs float64 `json:"max_ms"`
}

// spikeRow is one tick_spikes mirror entry: a closed burst.
type spikeRow struct {
	TickCountEnd uint64  `json:"tick_count_end"`
	MaxMs        float64 `json:"max_ms"`
}

// phaseWindow is one phase's window row in the step report.
type phaseWindow struct {
	Name           string  `json:"name"`
	Samples        uint64  `json:"samples"`
	P50Ms          float64 `json:"p50Ms"`
	P99Ms          float64 `json:"p99Ms"`
	MaxMsSinceBoot float64 `json:"maxMsSinceBoot"`
}

// slowWindow is one slow hook's or division's window row.
type slowWindow struct {
	Name           string  `json:"name"`
	Hits           uint64  `json:"hits"`
	MaxMsSinceBoot float64 `json:"maxMsSinceBoot"`
}

// tickWindow is the step's tick report. Error is set (and every number
// zero) when the two snapshots do not bound a readable window.
type tickWindow struct {
	Ticks              uint64        `json:"ticks"`
	Samples            uint64        `json:"samples"`
	MeanMs             float64       `json:"meanMs"`
	P50Ms              float64       `json:"p50Ms"`
	P90Ms              float64       `json:"p90Ms"`
	P99Ms              float64       `json:"p99Ms"`
	P999Ms             float64       `json:"p999Ms"`
	SpikeMaxMs         float64       `json:"spikeMaxMs"`
	Overruns           uint64        `json:"overruns"`
	OverrunsPer10k     float64       `json:"overrunsPer10k"`
	LiveSessionsBefore int           `json:"liveSessionsBefore"`
	LiveSessionsAfter  int           `json:"liveSessionsAfter"`
	Phases             []phaseWindow `json:"phases,omitempty"`
	SlowHooks          []slowWindow  `json:"slowHooks,omitempty"`
	SlowDivisions      []slowWindow  `json:"slowDivisions,omitempty"`
	AllocatedBytes     uint64        `json:"allocatedBytes"`
	HeapLiveBytes      uint64        `json:"heapLiveBytes"`
	GCCycles           uint64        `json:"gcCycles"`
	Goroutines         uint64        `json:"goroutines"`
	Error              string        `json:"error,omitempty"`
}

/*
================
fetchMetrics

One metrics scrape: the parsed snapshot for the window math and the
raw body for the report directory (metrics-before/after.json, the
files the #2003 reader consumes).
================
*/
func fetchMetrics(url string, timeout time.Duration) (metricsSnapshot, []byte, error) {
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Get(url)
	if err != nil {
		return metricsSnapshot{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return metricsSnapshot{}, nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return metricsSnapshot{}, nil, err
	}
	var snap metricsSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return metricsSnapshot{}, nil, fmt.Errorf("metrics %s: undecodable reply: %w", url, err)
	}
	return snap, data, nil
}

/*
================
computeTickWindow

The window between two snapshots, in the #2003 reader's arithmetic.
================
*/
func computeTickWindow(before, after metricsSnapshot) tickWindow {
	w := tickWindow{
		LiveSessionsBefore: before.LiveSessions,
		LiveSessionsAfter:  after.LiveSessions,
		AllocatedBytes:     after.Runtime.AllocatedBytes - before.Runtime.AllocatedBytes,
		HeapLiveBytes:      after.Runtime.HeapLiveBytes,
		GCCycles:           after.Runtime.GCCycles - before.Runtime.GCCycles,
		Goroutines:         after.Runtime.Goroutines,
	}
	if after.TickCount <= before.TickCount {
		w.Error = fmt.Sprintf("tick_count did not advance (%d -> %d): the GameWorld restarted between snapshots or they are swapped",
			before.TickCount, after.TickCount)
		return w
	}
	w.Ticks = after.TickCount - before.TickCount
	deltas, samples := bucketDeltas(before.TickMsHist.Counts, after.TickMsHist.Counts)
	if samples == 0 {
		w.Error = "the tick histogram recorded no samples in the window"
		return w
	}
	w.Samples = samples
	bounds := after.TickMsHist.BoundsMs
	if len(bounds) == 0 {
		bounds = before.TickMsHist.BoundsMs
	}
	w.SpikeMaxMs = windowSpikeMax(before, after)
	w.P50Ms = percentileFromBuckets(bounds, deltas, 0.50, w.SpikeMaxMs)
	w.P90Ms = percentileFromBuckets(bounds, deltas, 0.90, w.SpikeMaxMs)
	w.P99Ms = percentileFromBuckets(bounds, deltas, 0.99, w.SpikeMaxMs)
	w.P999Ms = percentileFromBuckets(bounds, deltas, 0.999, w.SpikeMaxMs)
	// The mean is exact: both snapshots carry count and mean, so the
	// window's sum is the difference of the two since-boot sums.
	w.MeanMs = (after.TickMeanMs*float64(after.TickCount) - before.TickMeanMs*float64(before.TickCount)) / float64(w.Ticks)
	w.Overruns = after.TickOverruns - before.TickOverruns
	w.OverrunsPer10k = float64(w.Overruns) * 10000 / float64(w.Ticks)
	w.Phases = phaseWindows(before.TickPhaseMs, after.TickPhaseMs)
	w.SlowHooks = slowWindows(before.SlowHooks, after.SlowHooks)
	w.SlowDivisions = slowWindows(before.SlowDivisions, after.SlowDivisions)
	return w
}

/*
================
bucketDeltas

Per-bucket window counts: after minus before, clamped at zero (a
counter that went backwards - a same-boot histogram rebuild - must
not manufacture negative samples). Missing before buckets read zero.
================
*/
func bucketDeltas(before, after []uint64) (deltas []uint64, total uint64) {
	deltas = make([]uint64, len(after))
	for i, a := range after {
		var b uint64
		if i < len(before) {
			b = before[i]
		}
		if a > b {
			deltas[i] = a - b
		}
		total += deltas[i]
	}
	return deltas, total
}

/*
================
percentileFromBuckets

The #2003 rule: a percentile reads as its bucket's upper bound, and
the open last bucket reads as the window's spike max when one is
known. A bounded bucket never takes the spike max - the journal is
thresholded and the histogram is not, so a quiet journal must not pull
a percentile below its own bucket.
================
*/
func percentileFromBuckets(bounds []float64, counts []uint64, q float64, openValue float64) float64 {
	var total uint64
	for _, n := range counts {
		total += n
	}
	if total == 0 || len(bounds) == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	var seen uint64
	for i, n := range counts {
		seen += n
		if seen >= rank {
			if i < len(bounds) {
				return bounds[i]
			}
			if openValue != 0 {
				return openValue
			}
			return bounds[len(bounds)-1]
		}
	}
	if openValue != 0 {
		return openValue
	}
	return bounds[len(bounds)-1]
}

/*
================
windowSpikeMax

The worst tick the spike journal closed inside the window, bound by
tick_count like the reader binds it (the journal outlives the process
while tick_count restarts at boot; the mirror rides the after
snapshot, and the tick bound is what separates this boot's bursts).
================
*/
func windowSpikeMax(before, after metricsSnapshot) float64 {
	var max float64
	for _, spike := range after.TickSpikes {
		if spike.TickCountEnd > before.TickCount && spike.TickCountEnd <= after.TickCount && spike.MaxMs > max {
			max = spike.MaxMs
		}
	}
	return max
}

/*
================
phaseWindows

Each phase's window percentiles from its bucket deltas. Phases with
no window samples carry no signal and are omitted. Sorted by p99,
worst first, so the report's head is the phase that owns the tail.
================
*/
func phaseWindows(before, after map[string]phaseHistogram) []phaseWindow {
	var rows []phaseWindow
	for name, a := range after {
		b := before[name]
		deltas, samples := bucketDeltas(b.Buckets, a.Buckets)
		if samples == 0 {
			continue
		}
		bounds := a.BoundsMs
		if len(bounds) == 0 {
			bounds = b.BoundsMs
		}
		rows = append(rows, phaseWindow{
			Name:           name,
			Samples:        samples,
			P50Ms:          percentileFromBuckets(bounds, deltas, 0.50, 0),
			P99Ms:          percentileFromBuckets(bounds, deltas, 0.99, 0),
			MaxMsSinceBoot: a.MaxMs,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].P99Ms != rows[j].P99Ms {
			return rows[i].P99Ms > rows[j].P99Ms
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

/*
================
slowWindows

Slow-hook and slow-division window hits (the reader's slow_totals): a
name absent before counts its full after count; only hits print.
max_ms cannot be windowed from two snapshots, so it rides under its
since-boot name, as the reader reports it.
================
*/
func slowWindows(before, after map[string]slowCount) []slowWindow {
	var rows []slowWindow
	for name, a := range after {
		b, seenBefore := before[name]
		var hits uint64
		if a.Count > b.Count {
			hits = a.Count - b.Count
		} else if !seenBefore {
			hits = a.Count
		}
		if hits == 0 {
			continue
		}
		rows = append(rows, slowWindow{Name: name, Hits: hits, MaxMsSinceBoot: a.MaxMs})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Hits != rows[j].Hits {
			return rows[i].Hits > rows[j].Hits
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

/*
================
fetchServerBuild

The Agent's /title/build: the server build the staircase ran against,
for the report header. Best-effort - a stack that will not answer names
itself by refusing the bots a moment later, so a failure here records
an empty build rather than stopping the run.
================
*/
func fetchServerBuild(agentURL string, timeout time.Duration) (revision, subject string) {
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(agentURL, "/")+"/title/build", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set(releaseprotocol.Header, strconv.Itoa(releaseprotocol.Current))
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	var reply struct {
		Build struct {
			Revision string `json:"revision"`
			Subject  string `json:"subject"`
		} `json:"build"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply); err != nil {
		return "", ""
	}
	return reply.Build.Revision, reply.Build.Subject
}
