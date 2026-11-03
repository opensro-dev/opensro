/*
===========================================================================

measurement_metrics.go - bounded transport snapshots and honest window deltas

The endpoint owns its process-lifetime counters and gauges. A probe retains
both complete snapshots, subtracts only named counters, and asks transport's
histogram owner for the tick window. Runtime percentiles have no served
buckets, so they remain labelled lifetime snapshots, never window estimates.

===========================================================================
*/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"opensro.online/server/internal/transport"
)

const (
	probeMetricsBodyLimit = 1 << 20
	// TickWindowBetween converts the served milliseconds back to a Duration.
	maxProbeTickMs = float64(math.MaxInt64 / int64(time.Millisecond))
)

/*
================
probeMetricsReport

Raw snapshots survive comparison errors. CounterDeltas and TickWindow are
published only after validating the entire comparison. Transport's open
last tick bucket uses the later lifetime maximum, not a measured window max.
================
*/
type probeMetricsReport struct {
	Before                 transport.Metrics    `json:"before"`
	After                  transport.Metrics    `json:"after"`
	CounterDeltas          map[string]uint64    `json:"counter_deltas,omitempty"`
	TickWindow             transport.TickWindow `json:"tick_window"`
	CounterResets          []string             `json:"counter_resets,omitempty"`
	RuntimePercentileScope string               `json:"runtime_percentile_scope"`
	TickPercentileScope    string               `json:"tick_percentile_scope"`
}

/*
================
readProbeMetrics

The caller supplies the HTTP trust roots and timeout policy. Reading metrics
never changes that client's TLS verification or substitutes a default client.
================
*/
func readProbeMetrics(ctx context.Context, client *http.Client, base string) (transport.Metrics, error) {
	if client == nil {
		return transport.Metrics{}, fmt.Errorf("metrics HTTP client is required")
	}
	target, err := url.Parse(base)
	if err != nil {
		return transport.Metrics{}, fmt.Errorf("metrics base URL: %w", err)
	}
	if (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.RawQuery != "" || target.Fragment != "" {
		return transport.Metrics{}, fmt.Errorf("metrics base must be an HTTP URL without a query or fragment")
	}
	target.Path = strings.TrimRight(target.Path, "/") + transport.PathMetrics
	target.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return transport.Metrics{}, fmt.Errorf("metrics request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return transport.Metrics{}, fmt.Errorf("metrics request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return transport.Metrics{}, fmt.Errorf("metrics status %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, probeMetricsBodyLimit+1))
	if err != nil {
		return transport.Metrics{}, fmt.Errorf("metrics body: %w", err)
	}
	if len(body) > probeMetricsBodyLimit {
		return transport.Metrics{}, fmt.Errorf("metrics body exceeds %d bytes", probeMetricsBodyLimit)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return transport.Metrics{}, fmt.Errorf("metrics JSON: %w", err)
	}
	var snapshot transport.Metrics
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return transport.Metrics{}, fmt.Errorf("metrics schema: %w", err)
	}
	if err := requireProbeMetricsFields(fields, snapshot); err != nil {
		return transport.Metrics{}, err
	}
	if err := validateProbeTickHistogram(snapshot); err != nil {
		return transport.Metrics{}, err
	}
	return snapshot, nil
}

/*
================
requireProbeMetricsFields

Absent or null fields must not masquerade as zero counters. Unknown fields
remain allowed so a newer endpoint can add metrics without breaking a probe.
================
*/
func requireProbeMetricsFields(fields map[string]json.RawMessage, snapshot transport.Metrics) error {
	for key := range probeMetricCounters(snapshot) {
		if strings.HasPrefix(key, "runtime.") {
			continue
		}
		if err := requireProbeMetricField(fields, key); err != nil {
			return err
		}
	}
	for _, key := range []string{
		"live_sessions", "attached_sessions", "detached_sessions", "bound_sessions",
		"outbound_queue_depth", "outbound_queue_high_water", "outbound_queue_bytes", "outbound_queue_byte_high_water",
		"handshake_last_ms", "handshake_mean_ms", "handshake_max_ms", "tick_last_ms", "tick_mean_ms", "tick_max_ms",
		"runtime", "tick_ms_histogram",
	} {
		if err := requireProbeMetricField(fields, key); err != nil {
			return err
		}
	}
	for section, keys := range map[string][]string{
		"runtime": {
			"gc_pause_p99_ms", "gc_pause_max_ms", "sched_latency_p99_ms", "sched_latency_max_ms",
			"heap_live_bytes", "heap_goal_bytes", "allocated_bytes", "goroutines", "gc_cycles",
		},
		"tick_ms_histogram": {"bounds_ms", "counts", "p50_ms", "p90_ms", "p99_ms", "p999_ms"},
	} {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(fields[section], &nested); err != nil {
			return fmt.Errorf("metrics %s: %w", section, err)
		}
		for _, key := range keys {
			if err := requireProbeMetricField(nested, key); err != nil {
				return fmt.Errorf("metrics %s: %w", section, err)
			}
		}
	}
	return nil
}

/*
================
requireProbeMetricField
================
*/
func requireProbeMetricField(fields map[string]json.RawMessage, key string) error {
	raw, exists := fields[key]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("metrics field %s is missing or null", key)
	}
	return nil
}

/*
================
validateProbeTickHistogram

TickWindowBetween uses the transport owner's fixed bounds. Read that schema
from a zero Hub's exported snapshot, as transport's own histogram tests do,
instead of copying its private bucket table into this operation.
================
*/
func validateProbeTickHistogram(snapshot transport.Metrics) error {
	var hub transport.Hub
	expected := hub.Metrics().TickHistogram.BoundsMs
	histogram := snapshot.TickHistogram
	if len(histogram.BoundsMs) != len(expected) || len(histogram.Counts) != len(expected)+1 {
		return fmt.Errorf("metrics tick histogram has incompatible bucket lengths")
	}
	for i, bound := range expected {
		if histogram.BoundsMs[i] != bound {
			return fmt.Errorf("metrics tick histogram bound %d differs from transport", i)
		}
	}
	var total uint64
	for _, count := range histogram.Counts {
		if count > ^uint64(0)-total {
			return fmt.Errorf("metrics tick histogram count overflows")
		}
		total += count
	}
	if total != snapshot.TickCount || snapshot.TickOverruns > snapshot.TickCount {
		return fmt.Errorf("metrics tick histogram does not match tick counters")
	}
	if math.IsNaN(snapshot.TickMaxMs) || math.IsInf(snapshot.TickMaxMs, 0) || snapshot.TickMaxMs < 0 || snapshot.TickMaxMs > maxProbeTickMs {
		return fmt.Errorf("metrics tick maximum is invalid")
	}
	return nil
}

/*
================
probeMetricCounters

Only monotonic counts belong here. Queue high-water marks, current heap and
session gauges, timings and percentile snapshots stay in Before and After.
Optional world-owned maps and timing summaries are retained there as well.
================
*/
func probeMetricCounters(snapshot transport.Metrics) map[string]uint64 {
	return map[string]uint64{
		"wt_ok": snapshot.WTOk, "ws_ok": snapshot.WSOK,
		"rate_limited_frames":     snapshot.RateLimitedFrames,
		"handshake_rejected_busy": snapshot.HandshakeRejectedBusy, "account_session_evictions": snapshot.AccountSessionEvictions,
		"handshake_hello_timeout": snapshot.HandshakeHelloTimeout, "handshake_read_failed": snapshot.HandshakeReadFailed,
		"handshake_protocol_rejected": snapshot.HandshakeProtocolRejected, "handshake_count": snapshot.HandshakeCount,
		"resume_attempted": snapshot.ResumeAttempted, "resume_accepted": snapshot.ResumeAccepted,
		"resume_fresh": snapshot.ResumeFresh, "resume_no_position": snapshot.ResumeNoPosition, "resume_gap": snapshot.ResumeGap,
		"hello_admission_accepted": snapshot.HelloAdmissionAccepted, "hello_admission_refused": snapshot.HelloAdmissionRefused,
		"detaches": snapshot.Detaches, "detaches_idle": snapshot.DetachesIdle, "detaches_ring_park": snapshot.DetachesRingPark, "detaches_other": snapshot.DetachesOther,
		"sessions_opened": snapshot.SessionsOpened, "sessions_closed": snapshot.SessionsClosed,
		"sessions_closed_clean": snapshot.SessionsClosedClean, "sessions_closed_grace_expired": snapshot.SessionsClosedGraceExpired,
		"sessions_closed_slow_consumer": snapshot.SessionsClosedSlowConsumer, "sessions_closed_handler_panic": snapshot.SessionsClosedHandlerPanic,
		"sessions_closed_other": snapshot.SessionsClosedOther,
		"frames_in":             snapshot.FramesIn, "bytes_in": snapshot.BytesIn, "frames_out": snapshot.FramesOut, "bytes_out": snapshot.BytesOut,
		"datagrams_out": snapshot.DatagramsOut, "datagram_bytes_out": snapshot.DatagramBytesOut, "write_errors": snapshot.WriteErrors,
		"enter_world_auth_refused": snapshot.EnterWorldAuthRefused, "unhandled_opcode_frames": snapshot.UnhandledOpcodeFrames,
		"empty_frames_refused": snapshot.EmptyFramesRefused,
		"bye_normal":           snapshot.ByeNormal, "bye_shutdown": snapshot.ByeShutdown,
		"bye_replaced": snapshot.ByeReplaced, "bye_other": snapshot.ByeOther,
		"tick_count": snapshot.TickCount, "tick_overruns": snapshot.TickOverruns,
		"tick_spike_dropped": snapshot.TickSpikeDropped, "tick_spike_write_errors": snapshot.TickSpikeWriteErrors,
		"runtime.gc_cycles": snapshot.Runtime.GCCycles, "runtime.allocated_bytes": snapshot.Runtime.AllocatedBytes,
	}
}

/*
================
compareProbeMetrics

A reset invalidates the whole window, including counters that happened to
increase again after restart. Preserve the evidence and report the failure;
never turn a negative delta into a successful zero or an unsigned wrap.
================
*/
func compareProbeMetrics(before, after transport.Metrics) (probeMetricsReport, error) {
	report := probeMetricsReport{
		Before: before, After: after,
		RuntimePercentileScope: "process_lifetime_snapshots",
		TickPercentileScope:    "window_bucket_upper_bounds; open_bucket_uses_after_lifetime_tick_max",
	}
	if err := validateProbeTickHistogram(before); err != nil {
		return report, fmt.Errorf("before snapshot: %w", err)
	}
	if err := validateProbeTickHistogram(after); err != nil {
		return report, fmt.Errorf("after snapshot: %w", err)
	}
	first, last := probeMetricCounters(before), probeMetricCounters(after)
	deltas := make(map[string]uint64, len(first))
	for key, value := range first {
		if last[key] < value {
			report.CounterResets = append(report.CounterResets, key)
			continue
		}
		deltas[key] = last[key] - value
	}
	for i, count := range before.TickHistogram.Counts {
		if after.TickHistogram.Counts[i] < count {
			report.CounterResets = append(report.CounterResets, fmt.Sprintf("tick_ms_histogram.counts[%d]", i))
		}
	}
	if len(report.CounterResets) != 0 {
		sort.Strings(report.CounterResets)
		return report, fmt.Errorf("metrics counters reset: %s", strings.Join(report.CounterResets, ", "))
	}
	report.CounterDeltas = deltas
	report.TickWindow = transport.TickWindowBetween(before, after)
	return report, nil
}
