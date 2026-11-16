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

The fields keep their PathMetrics names and shapes, and the view carries
every field sro-loadtest's metricsSnapshot and tick_acceptance.py decode
(the tick_* family, live_sessions, tick_spikes, runtime), so those tools
read either document; a field they read and the view lacked would decode
as zero and report a quiet wrong window. captured_at is added so two
pulls can be turned into a window without trusting the puller's clock.

The path sits outside /transport/ on purpose. Both public edges proxy
/transport/* to this listener (the shard route on opensro.online and the
play site), and each closes only the exact path /transport/metrics.
Checked from outside on 2026-11-16: an unknown /transport/ path reaches the
GameWorld's own 404 there, any other path stops at the website or Caddy.
A view under /transport/metrics/ would have been public from the deploy
until the edge closed it; out here no edge reaches it, whatever the order
of the server release and the edge change. The authenticated edge route is
the only way in.

===========================================================================
*/

package transport

import (
	"encoding/json"
	"net/http"
	"time"
)

// PathMetricsTick is the tick-only view; see the file banner for why it is
// not under /transport/.
const PathMetricsTick = "/ops/tick-metrics"

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
	// TickSpikes is the spike journal's mirror: closed bursts with their
	// slowest step, phase and hook (code labels) and the session count.
	TickSpikes []TickSpike `json:"tick_spikes,omitempty"`
	// LiveSessions is the load the window ran under (a count only).
	LiveSessions int `json:"live_sessions"`

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
		TickSpikes:        m.TickSpikes,
		LiveSessions:      m.LiveSessions,
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
