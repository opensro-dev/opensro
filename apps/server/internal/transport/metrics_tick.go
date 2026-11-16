/*
===========================================================================

metrics_tick.go - the tick-only view of /transport/metrics

PathMetrics serves everything the hub counts: sessions, economy totals,
store lock timings, pet and placement counters. Contributors working on
server tick cost need only the tick's own numbers, and the operator
hands them a revocable login at the edge (opensro-ops hosts/vps1
TICK-METRICS.md) that reaches this path and nothing else. Keeping the
subset here, in the process, means a leaked login exposes timings and
counters only: Caddy cannot trim a JSON document, so the trim must happen
before the edge.

The fields keep their PathMetrics names and shapes, so a tool that reads
one document reads the other (sro-loadtest's metricsSnapshot, the bench
scripts, tick_acceptance.py). captured_at is added so two pulls can be
turned into a window without trusting the puller's clock.

Like PathMetrics, this path is loopback-only by the listener; the edge
must never proxy it without authentication.

===========================================================================
*/

package transport

import (
	"encoding/json"
	"net/http"
	"time"
)

// PathMetricsTick is the tick-only view; see the file banner.
const PathMetricsTick = "/transport/metrics/tick"

/*
================
TickMetrics

The PathMetricsTick document. Every field is a timing, a count or a
division / hook label; nothing names a player, an account or an address.
================
*/
type TickMetrics struct {
	CapturedAt string `json:"captured_at"`

	TickCount     uint64        `json:"tick_count"`
	TickLastMs    float64       `json:"tick_last_ms"`
	TickMeanMs    float64       `json:"tick_mean_ms"`
	TickMaxMs     float64       `json:"tick_max_ms"`
	TickOverruns  uint64        `json:"tick_overruns"`
	TickHistogram TickHistogram `json:"tick_ms_histogram"`

	TickPhaseMs       map[string]Histogram     `json:"tick_phase_ms"`
	SlowHooks         map[string]SlowHookStats `json:"slow_hooks"`
	SlowDivisions     map[string]SlowHookStats `json:"slow_divisions"`
	PublicationHolds  map[string]SlowHookStats `json:"publication_holds"`
	TickCommit        TickCommitStats          `json:"tick_commit"`
	MonsterNavigation map[string]uint64        `json:"monster_navigation,omitempty"`
	TickZones         map[string]TickZone      `json:"tick_zones,omitempty"`
	Runtime           RuntimeStats             `json:"runtime"`
}

/*
================
tickMetricsFrom

Copies the tick fields out of one full snapshot. The maps are the
snapshot's own copies (Hub.Metrics builds them per call), so sharing
them here aliases nothing the hub still writes.
================
*/
func tickMetricsFrom(m Metrics, now time.Time) TickMetrics {
	return TickMetrics{
		CapturedAt:        now.UTC().Format(time.RFC3339Nano),
		TickCount:         m.TickCount,
		TickLastMs:        m.TickLastMs,
		TickMeanMs:        m.TickMeanMs,
		TickMaxMs:         m.TickMaxMs,
		TickOverruns:      m.TickOverruns,
		TickHistogram:     m.TickHistogram,
		TickPhaseMs:       m.TickPhaseMs,
		SlowHooks:         m.SlowHooks,
		SlowDivisions:     m.SlowDivisions,
		PublicationHolds:  m.PublicationHolds,
		TickCommit:        m.TickCommit,
		MonsterNavigation: m.MonsterNavigation,
		TickZones:         m.TickZones,
		Runtime:           m.Runtime,
	}
}

/*
================
Server.handleMetricsTick
================
*/
func (s *Server) handleMetricsTick(w http.ResponseWriter, r *http.Request) {
	if !allowReadMethod(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(tickMetricsFrom(s.Hub.Metrics(), time.Now()))
}
