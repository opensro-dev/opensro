/*
===========================================================================

measurement_test.go - probe schedules through production reliable transports

These are correctness fixtures, not performance comparisons. Ephemeral
servers exercise real admission, codecs, echo budgets and keepalives; short
schedules make no claim about remote browsers or QUIC under packet loss.

===========================================================================
*/

package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

const (
	measurementTestSamples       = 5
	measurementTestInterval      = time.Millisecond
	measurementTestTimeout       = 2 * time.Second
	measurementTestKeepalive     = 5 * time.Millisecond
	measurementTestQuietInterval = 20 * time.Millisecond
	measurementTestRate          = 1000
	measurementTestBurst         = 256
	measurementLimitedRate       = 1
	measurementLimitedBurst      = 2
	measurementLimitedDrain      = 50 * time.Millisecond
)

/*
================
measurementTestConfig
================
*/
func measurementTestConfig(t *testing.T, server *transport.Server, kind string) measurementConfig {
	t.Helper()
	return measurementConfig{
		Probe: probeConfig{
			Base: "http://" + server.WSAddr(), WTAddr: server.WTAddr(),
			Ticket: probeTestTicket, Timeout: measurementTestTimeout, CACert: caCertFile(t, server), Transport: kind,
		},
		PingSamples: measurementTestSamples, PingInterval: measurementTestInterval,
		BurstSamples: measurementTestSamples, BurstInterval: measurementTestInterval,
	}
}

/*
================
TestMeasurementProductionTransports

Neither arm bypasses admission or enters a character into a world. The JSON
contains one handshake observation and preserves both raw sample schedules.
================
*/
func TestMeasurementProductionTransports(t *testing.T) {
	for _, kind := range []string{"ws", "wt"} {
		t.Run(kind, func(t *testing.T) {
			server := startProbeServer(t)
			cfg := measurementTestConfig(t, server, kind)
			ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
			defer cancel()
			report, err := measureProbe(ctx, cfg)
			if err != nil || !report.Valid {
				t.Fatalf("measurement failed: %v; report=%+v", err, report)
			}
			if report.HandshakeSamples != 1 || report.HelloToWelcome < 0 || report.Settings.Transport != kind {
				t.Fatalf("incorrect connection evidence: %+v", report)
			}
			for _, phase := range []sampleSummary{report.PingTrain, report.OrderedBurst} {
				if phase.Sent != measurementTestSamples || phase.Received != measurementTestSamples || phase.NotSent != 0 ||
					phase.Missing != 0 || phase.DuplicateReplies != 0 || phase.OutOfOrder != 0 || len(phase.Samples) != measurementTestSamples {
					t.Fatalf("incomplete or miscounted phase: %+v", phase)
				}
			}
			if report.Metrics == nil || report.IncompleteBefore != nil || report.Metrics.CounterDeltas["rate_limited_frames"] != 0 {
				t.Fatal("measurement lost its counter window")
			}
			body, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), probeTestTicket) || !strings.Contains(string(body), "rtt_p99_ns") {
				t.Fatal("measurement JSON leaked its ticket or omitted its latency evidence")
			}
		})
	}
}

/*
================
TestMeasurementDefaultBurstFitsAdmission

The production rate bucket must admit the actual default 200-frame paced
burst. This proves schedule correctness, not comparative transport speed.
================
*/
func TestMeasurementDefaultBurstFitsAdmission(t *testing.T) {
	server := startProbeServer(t)
	cfg := measurementTestConfig(t, server, "ws")
	cfg.PingSamples = 1
	cfg.BurstSamples, cfg.BurstInterval = defaultBurstSamples, defaultBurstInterval
	ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
	defer cancel()
	report, err := measureProbe(ctx, cfg)
	if err != nil || !report.Valid || report.OrderedBurst.Received != defaultBurstSamples ||
		report.Metrics == nil || report.Metrics.CounterDeltas["rate_limited_frames"] != 0 {
		t.Fatalf("default paced burst exceeded admission: %v; %+v", err, report)
	}
}

/*
================
TestMeasurementRejectsAdmissionLimitedArm

An intentionally tiny server bucket drops this explicitly unpaced arm.
Missing echoes and the server counter must invalidate it, not become a
successful network-latency measurement.
================
*/
func TestMeasurementRejectsAdmissionLimitedArm(t *testing.T) {
	settings := probeTestServerConfig(t)
	settings.RateLimitPerSec, settings.RateLimitBurst = measurementLimitedRate, measurementLimitedBurst
	server := startProbeServerWithConfig(t, settings)
	cfg := measurementTestConfig(t, server, "ws")
	cfg.BurstInterval, cfg.Probe.Timeout = 0, measurementLimitedDrain
	ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
	defer cancel()
	report, err := measureProbe(ctx, cfg)
	if err == nil || report.Valid || report.Metrics == nil {
		t.Fatalf("limited arm appeared successful: err=%v report=%+v", err, report)
	}
	if report.Metrics.CounterDeltas["rate_limited_frames"] == 0 || report.PingTrain.Missing+report.OrderedBurst.Missing == 0 {
		t.Fatal("the dropped frames disappeared from the measurement evidence")
	}
}

/*
================
TestMeasurementAnswersKeepalive

Fast keepalives use a declared large fixture budget. They exercise the
shared writer while the receiver and independent schedule are both active.
================
*/
func TestMeasurementAnswersKeepalive(t *testing.T) {
	for _, kind := range []string{"ws", "wt"} {
		t.Run(kind, func(t *testing.T) {
			settings := probeTestServerConfig(t)
			settings.RateLimitPerSec, settings.RateLimitBurst = measurementTestRate, measurementTestBurst
			settings.KeepaliveInterval = measurementTestKeepalive
			server := startProbeServerWithConfig(t, settings)
			cfg := measurementTestConfig(t, server, kind)
			cfg.PingInterval, cfg.BurstInterval = measurementTestQuietInterval, measurementTestQuietInterval
			ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
			defer cancel()
			report, err := measureProbe(ctx, cfg)
			if err != nil || !report.Valid || report.KeepaliveReplies == 0 {
				t.Fatalf("keepalive was ignored or corrupted the run: %v; %+v", err, report)
			}
		})
	}
}

/*
================
TestProbeHTTPSRootsCoverDiscoveryMetricsAndWSS

An HTTPS edge may serve a different certificate from the UDP listener.
Both must verify against the supplied roots; WT still pins its own leaf.
================
*/
func TestProbeHTTPSRootsCoverDiscoveryMetricsAndWSS(t *testing.T) {
	server := startProbeServer(t)
	target, err := url.Parse("http://" + server.WSAddr())
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(target))
	defer edge.Close()
	certificates := []*x509.Certificate{edge.Certificate(), server.Cert.Leaf}
	var bundle []byte
	for _, certificate := range certificates {
		bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})...)
	}
	ca := filepath.Join(t.TempDir(), "edge-and-udp.pem")
	if err := os.WriteFile(ca, bundle, probeTestCAFileMode); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"ws", "wt"} {
		t.Run(kind, func(t *testing.T) {
			cfg := measurementTestConfig(t, server, kind)
			cfg.Probe.Base, cfg.Probe.CACert = edge.URL, ca
			ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
			defer cancel()
			report, err := measureProbe(ctx, cfg)
			if err != nil || !report.Valid {
				t.Fatalf("trusted edge/UDP arm failed: %v; %+v", err, report)
			}
		})
	}
}

/*
================
TestMeasurementBudgetRejectsInvalidAndUnboundedSchedules
================
*/
func TestMeasurementBudgetRejectsInvalidAndUnboundedSchedules(t *testing.T) {
	valid := measurementConfig{
		Probe: probeConfig{Timeout: defaultTimeout}, PingSamples: defaultPingSamples,
		PingInterval: defaultPingInterval, BurstSamples: defaultBurstSamples, BurstInterval: defaultBurstInterval,
	}
	if _, err := valid.budget(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*measurementConfig){
		func(cfg *measurementConfig) { cfg.PingSamples = 0 },
		func(cfg *measurementConfig) { cfg.BurstSamples = maxMeasurementSamples + 1 },
		func(cfg *measurementConfig) { cfg.PingInterval = -time.Second },
		func(cfg *measurementConfig) { cfg.BurstInterval = -time.Second },
		func(cfg *measurementConfig) { cfg.Probe.Timeout = 0 },
		func(cfg *measurementConfig) { cfg.PingInterval = maxMeasurementBudget },
	} {
		cfg := valid
		change(&cfg)
		if _, err := cfg.budget(); err == nil {
			t.Fatal("invalid measurement schedule was accepted")
		}
	}
}
