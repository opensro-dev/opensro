/*
===========================================================================

measurement_metrics_test.go - measurement windows and bounded HTTP evidence

Synthetic transport snapshots distinguish counters from lifetime summaries.
HTTP fixtures exercise the supplied client's trust and request path without
starting a game transport, authenticating a player or measuring production.

===========================================================================
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

/*
================
probeMetricsFixture

Only the schema comes from the owner; counters are deterministic fixture data.
================
*/
func probeMetricsFixture() transport.Metrics {
	var hub transport.Hub
	return transport.Metrics{TickHistogram: hub.Metrics().TickHistogram}
}

/*
================
probeMetricsJSON
================
*/
func probeMetricsJSON(t *testing.T, snapshot transport.Metrics) []byte {
	t.Helper()
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

/*
================
TestProbeMetricsDeltasContainOnlyCounters
================
*/
func TestProbeMetricsDeltasContainOnlyCounters(t *testing.T) {
	before, after := probeMetricsFixture(), probeMetricsFixture()
	before.FramesIn, after.FramesIn = 100, 108
	before.BytesIn, after.BytesIn = 1000, 1120
	before.FramesOut, after.FramesOut = 200, 211
	before.BytesOut, after.BytesOut = 5000, 5500
	before.DatagramsOut, after.DatagramsOut = 20, 23
	before.DatagramBytesOut, after.DatagramBytesOut = 200, 230
	before.WriteErrors, after.WriteErrors = 1, 2
	before.EmptyFramesRefused, after.EmptyFramesRefused = 2, 4
	before.RateLimitedFrames, after.RateLimitedFrames = 3, 6
	before.SessionsClosedSlowConsumer, after.SessionsClosedSlowConsumer = 4, 8
	before.HandshakeCount, after.HandshakeCount = 10, 15
	before.HelloAdmissionAccepted, after.HelloAdmissionAccepted = 5, 9
	before.HelloAdmissionRefused, after.HelloAdmissionRefused = 1, 3
	before.WTOk, after.WTOk = 7, 9
	before.WSOK, after.WSOK = 8, 11
	before.Runtime.GCCycles, after.Runtime.GCCycles = 40, 42
	before.Runtime.AllocatedBytes, after.Runtime.AllocatedBytes = 30000, 30900
	report, err := compareProbeMetrics(before, after)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]uint64{
		"frames_in": 8, "bytes_in": 120, "frames_out": 11, "bytes_out": 500,
		"datagrams_out": 3, "datagram_bytes_out": 30, "write_errors": 1,
		"empty_frames_refused": 2, "rate_limited_frames": 3, "sessions_closed_slow_consumer": 4,
		"handshake_count": 5, "hello_admission_accepted": 4, "hello_admission_refused": 2,
		"wt_ok": 2, "ws_ok": 3, "runtime.gc_cycles": 2, "runtime.allocated_bytes": 900,
	} {
		if got, exists := report.CounterDeltas[key]; !exists || got != want {
			t.Errorf("counter %s = %d/%v, want %d", key, got, exists, want)
		}
	}
	if !reflect.DeepEqual(report.Before, before) || !reflect.DeepEqual(report.After, after) {
		t.Fatal("the report did not retain its complete input snapshots")
	}
}

/*
================
TestProbeMetricsPreservesGaugesAndLifetimePercentiles
================
*/
func TestProbeMetricsPreservesGaugesAndLifetimePercentiles(t *testing.T) {
	before, after := probeMetricsFixture(), probeMetricsFixture()
	before.LiveSessions, after.LiveSessions = 10, 2
	before.AttachedSessions, after.AttachedSessions = 8, 1
	before.DetachedSessions, after.DetachedSessions = 2, 1
	before.BoundSessions, after.BoundSessions = 7, 1
	before.OutboundQueueDepth, after.OutboundQueueDepth = 80, 3
	before.OutboundQueueBytes, after.OutboundQueueBytes = 800, 30
	before.OutboundQueueHighWater, after.OutboundQueueHighWater = 90, 100
	before.OutboundQueueByteHighWater, after.OutboundQueueByteHighWater = 900, 1000
	before.HandshakeMeanMs, after.HandshakeMeanMs = 50, 40
	before.HandshakeMaxMs, after.HandshakeMaxMs = 100, 120
	before.TickMeanMs, after.TickMeanMs = 20, 10
	before.TickMaxMs, after.TickMaxMs = 30, 40
	before.Runtime = transport.RuntimeStats{GCPauseP99Ms: 8, GCPauseMaxMs: 10, SchedLatencyP99Ms: 30,
		SchedLatencyMaxMs: 40, HeapLiveBytes: 200, HeapGoalBytes: 400, Goroutines: 20}
	after.Runtime = transport.RuntimeStats{GCPauseP99Ms: 4, GCPauseMaxMs: 12, SchedLatencyP99Ms: 15,
		SchedLatencyMaxMs: 50, HeapLiveBytes: 100, HeapGoalBytes: 300, Goroutines: 10}
	after.MonsterNavigation = map[string]uint64{"path_success": 100}
	report, err := compareProbeMetrics(before, after)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"live_sessions", "attached_sessions", "detached_sessions", "bound_sessions",
		"outbound_queue_depth", "outbound_queue_bytes", "outbound_queue_high_water", "outbound_queue_byte_high_water",
		"handshake_mean_ms", "handshake_max_ms", "tick_mean_ms", "tick_max_ms",
		"runtime.gc_pause_p99_ms", "runtime.sched_latency_p99_ms", "runtime.heap_live_bytes", "runtime.goroutines", "cpu",
	} {
		if _, exists := report.CounterDeltas[key]; exists {
			t.Errorf("non-counter %s was subtracted", key)
		}
	}
	if report.RuntimePercentileScope != "process_lifetime_snapshots" ||
		!reflect.DeepEqual(report.Before, before) || !reflect.DeepEqual(report.After, after) {
		t.Fatal("lifetime measurements lost their values or scope")
	}
}

/*
================
TestProbeMetricsCounterResetInvalidatesTheWindow
================
*/
func TestProbeMetricsCounterResetInvalidatesTheWindow(t *testing.T) {
	cases := []struct {
		name string
		set  func(*transport.Metrics, uint64)
	}{
		{"frames_in", func(m *transport.Metrics, n uint64) { m.FramesIn = n }},
		{"bytes_out", func(m *transport.Metrics, n uint64) { m.BytesOut = n }},
		{"write_errors", func(m *transport.Metrics, n uint64) { m.WriteErrors = n }},
		{"empty_frames_refused", func(m *transport.Metrics, n uint64) { m.EmptyFramesRefused = n }},
		{"rate_limited_frames", func(m *transport.Metrics, n uint64) { m.RateLimitedFrames = n }},
		{"handshake_count", func(m *transport.Metrics, n uint64) { m.HandshakeCount = n }},
		{"hello_admission_accepted", func(m *transport.Metrics, n uint64) { m.HelloAdmissionAccepted = n }},
		{"sessions_closed_slow_consumer", func(m *transport.Metrics, n uint64) { m.SessionsClosedSlowConsumer = n }},
		{"runtime.gc_cycles", func(m *transport.Metrics, n uint64) { m.Runtime.GCCycles = n }},
		{"runtime.allocated_bytes", func(m *transport.Metrics, n uint64) { m.Runtime.AllocatedBytes = n }},
		{"tick_count", func(m *transport.Metrics, n uint64) {
			m.TickCount, m.TickHistogram.Counts[0] = n, n
		}},
		{"tick_overruns", func(m *transport.Metrics, n uint64) {
			m.TickCount, m.TickHistogram.Counts[0], m.TickOverruns = 20, 20, n
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := probeMetricsFixture(), probeMetricsFixture()
			tc.set(&before, 9)
			tc.set(&after, 2)
			after.FramesOut = 100
			report, err := compareProbeMetrics(before, after)
			if err == nil || !slices.Contains(report.CounterResets, tc.name) || report.CounterDeltas != nil ||
				report.TickWindow != (transport.TickWindow{}) {
				t.Fatalf("reset became a successful or partial window: report=%+v err=%v", report, err)
			}
			if !reflect.DeepEqual(report.Before, before) || !reflect.DeepEqual(report.After, after) {
				t.Fatal("reset discarded the evidence")
			}
		})
	}
}

/*
================
TestProbeMetricsTickWindowExcludesTheSlowPast
================
*/
func TestProbeMetricsTickWindowExcludesTheSlowPast(t *testing.T) {
	var hub transport.Hub
	for range 100 {
		hub.RecordTickDuration(150*time.Millisecond, 100*time.Millisecond)
	}
	before := hub.Metrics()
	for range 99 {
		hub.RecordTickDuration(8*time.Millisecond, 100*time.Millisecond)
	}
	hub.RecordTickDuration(130*time.Millisecond, 100*time.Millisecond)
	after := hub.Metrics()
	report, err := compareProbeMetrics(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := transport.TickWindow{Count: 100, Overruns: 1, P50Ms: 10, P99Ms: 10}
	if report.TickWindow != want || report.CounterDeltas["tick_count"] != 100 || report.CounterDeltas["tick_overruns"] != 1 {
		t.Fatalf("tick window %+v, want %+v", report.TickWindow, want)
	}
	if report.Before.TickHistogram.P50Ms == report.TickWindow.P50Ms {
		t.Fatal("fixture did not distinguish lifetime and window percentiles")
	}
}

/*
================
TestProbeMetricsTickBucketResetCannotHideBehindAnIncreasingCount
================
*/
func TestProbeMetricsTickBucketResetCannotHideBehindAnIncreasingCount(t *testing.T) {
	before, after := probeMetricsFixture(), probeMetricsFixture()
	before.TickCount, before.TickHistogram.Counts[0] = 10, 10
	after.TickCount, after.TickHistogram.Counts[1] = 20, 20
	report, err := compareProbeMetrics(before, after)
	if err == nil || !slices.Contains(report.CounterResets, "tick_ms_histogram.counts[0]") || report.CounterDeltas != nil {
		t.Fatalf("bucket reset was hidden: report=%+v err=%v", report, err)
	}
}

/*
================
TestProbeMetricsEmptyTickWindowIsAValidObservation
================
*/
func TestProbeMetricsEmptyTickWindowIsAValidObservation(t *testing.T) {
	before, after := probeMetricsFixture(), probeMetricsFixture()
	after.FramesIn = 1
	report, err := compareProbeMetrics(before, after)
	if err != nil || report.TickWindow != (transport.TickWindow{}) || report.CounterDeltas["frames_in"] != 1 {
		t.Fatalf("no-tick observation: report=%+v err=%v", report, err)
	}
}

/*
================
TestProbeMetricsOpenTickBucketKeepsItsLifetimeMaximumLabel
================
*/
func TestProbeMetricsOpenTickBucketKeepsItsLifetimeMaximumLabel(t *testing.T) {
	before, after := probeMetricsFixture(), probeMetricsFixture()
	before.TickCount, after.TickCount = 1, 2
	before.TickHistogram.Counts[0] = 1
	after.TickHistogram.Counts[0] = 1
	after.TickHistogram.Counts[len(after.TickHistogram.Counts)-1] = 1
	before.TickMaxMs, after.TickMaxMs = 9000, 9000
	report, err := compareProbeMetrics(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if report.TickWindow.P99Ms != 9000 ||
		report.TickPercentileScope != "window_bucket_upper_bounds; open_bucket_uses_after_lifetime_tick_max" {
		t.Fatal("open bucket was presented as an independently measured window maximum")
	}
}

/*
================
TestProbeMetricsHTTPUsesTheSuppliedTLSClientAndMetricsPath
================
*/
func TestProbeMetricsHTTPUsesTheSuppliedTLSClientAndMetricsPath(t *testing.T) {
	want := probeMetricsFixture()
	want.FramesIn = 42
	body := probeMetricsJSON(t, want)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/prefix"+transport.PathMetrics || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected metrics request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	got, err := readProbeMetrics(t.Context(), server.Client(), server.URL+"/prefix/")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("metrics=%+v err=%v, want %+v", got, err, want)
	}
}

/*
================
TestProbeMetricsHTTPRejectsInvalidResponses
================
*/
func TestProbeMetricsHTTPRejectsInvalidResponses(t *testing.T) {
	valid := probeMetricsJSON(t, probeMetricsFixture())
	incompatible := probeMetricsFixture()
	incompatible.TickHistogram.BoundsMs[0]++
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(valid, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "frames_in")
	missingCounter, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"wrong-status", http.StatusServiceUnavailable, string(valid)},
		{"malformed-json", http.StatusOK, "{"},
		{"wrong-root-schema", http.StatusOK, "[]"},
		{"empty-root", http.StatusOK, "{}"},
		{"null-root", http.StatusOK, "null"},
		{"missing-counter", http.StatusOK, string(missingCounter)},
		{"incompatible-histogram", http.StatusOK, string(probeMetricsJSON(t, incompatible))},
		{"wrong-counter-type", http.StatusOK, strings.Replace(string(valid), `"frames_in":0`, `"frames_in":"zero"`, 1)},
		{"null-counter", http.StatusOK, strings.Replace(string(valid), `"frames_in":0`, `"frames_in":null`, 1)},
		{"null-runtime", http.StatusOK, strings.Replace(string(valid), `"gc_cycles":0`, `"gc_cycles":null`, 1)},
		{"trailing-object", http.StatusOK, string(valid) + " {}"},
		{"trailing-garbage", http.StatusOK, string(valid) + "garbage"},
		{"oversized-body", http.StatusOK, string(valid) + strings.Repeat(" ", probeMetricsBodyLimit)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			if _, err := readProbeMetrics(t.Context(), server.Client(), server.URL); err == nil {
				t.Fatal("invalid response was accepted")
			}
		})
	}
}

/*
================
TestProbeMetricsHTTPAllowsWhitespaceAndAdditionalMetrics
================
*/
func TestProbeMetricsHTTPAllowsWhitespaceAndAdditionalMetrics(t *testing.T) {
	want := probeMetricsFixture()
	body := string(probeMetricsJSON(t, want))
	body = strings.TrimSuffix(body, "}") + `,"future_metric":17}` + "\n \t"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	got, err := readProbeMetrics(t.Context(), server.Client(), server.URL)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("forward-compatible snapshot rejected: %+v %v", got, err)
	}
}

/*
================
TestProbeMetricsRejectsAnIncompatibleHistogram
================
*/
func TestProbeMetricsRejectsAnIncompatibleHistogram(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*transport.Metrics)
	}{
		{"missing-bounds", func(m *transport.Metrics) { m.TickHistogram.BoundsMs = nil }},
		{"wrong-bound", func(m *transport.Metrics) { m.TickHistogram.BoundsMs[0]++ }},
		{"missing-counts", func(m *transport.Metrics) { m.TickHistogram.Counts = nil }},
		{"count-mismatch", func(m *transport.Metrics) { m.TickCount = 1 }},
		{"count-overflow", func(m *transport.Metrics) { m.TickHistogram.Counts[0], m.TickHistogram.Counts[1] = ^uint64(0), 1 }},
		{"impossible-overruns", func(m *transport.Metrics) { m.TickOverruns = 1 }},
		{"negative-maximum", func(m *transport.Metrics) { m.TickMaxMs = -1 }},
		{"overflowing-maximum", func(m *transport.Metrics) { m.TickMaxMs = math.MaxFloat64 }},
		{"infinite-maximum", func(m *transport.Metrics) { m.TickMaxMs = math.Inf(1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := probeMetricsFixture(), probeMetricsFixture()
			tc.mutate(&after)
			report, err := compareProbeMetrics(before, after)
			if err == nil || report.CounterDeltas != nil || !reflect.DeepEqual(report.After, after) {
				t.Fatalf("incompatible histogram accepted or lost: report=%+v err=%v", report, err)
			}
		})
	}
}

/*
================
TestProbeMetricsHTTPHonorsCancellationAndRequiresAClient
================
*/
func TestProbeMetricsHTTPHonorsCancellationAndRequiresAClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("a cancelled request reached the server")
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readProbeMetrics(ctx, server.Client(), server.URL); err == nil {
		t.Fatal("request ignored the caller's cancelled context")
	}
	if _, err := readProbeMetrics(t.Context(), nil, server.URL); err == nil {
		t.Fatal("nil client silently selected a different trust policy")
	}
	for _, base := range []string{"file:///metrics", server.URL + "?secret=hidden", server.URL + "#fragment"} {
		if _, err := readProbeMetrics(t.Context(), server.Client(), base); err == nil {
			t.Fatalf("invalid base URL %q was accepted", base)
		}
	}
}
