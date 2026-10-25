/*
===========================================================================

measurement_wire_test.go - controlled bad peers and incomplete measurements

A WebSocket peer injects duplicate, corrupted and reordered echo responses.
Its metrics are a zero-Hub schema fixture, not a running GameWorld. A
keepalive barrier proves that the reader observed the final injected frame
before the second metrics scrape can finish.

===========================================================================
*/

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/transport"
)

/*
================
wireProbeMode
================
*/
type wireProbeMode uint8

const (
	wireProbeDuplicate wireProbeMode = iota
	wireProbeReorder
	wireProbeCorrupt
	wireProbeMetricsFailure
	wireProbeSilent
	wireProbeUnmatched
	wireProbeUnrelatedMetrics
	wireProbeSessionID    = 123
	wireProbeDrain        = 50 * time.Millisecond
	wireProbeLastSequence = 2
	probeCorruptBit       = 1
)

/*
================
wireProbeFixture
================
*/
type wireProbeFixture struct {
	server    *httptest.Server
	firstPing chan struct{}
}

/*
================
startWireProbeFixture

Only the fixture owns its socket writer. The real client still uses its
production frame codec and concurrent measurement reader/sender.
================
*/
func startWireProbeFixture(t *testing.T, mode wireProbeMode) wireProbeFixture {
	t.Helper()
	firstPing, lastObserved := make(chan struct{}), make(chan struct{})
	var firstOnce, observedOnce sync.Once
	var scrapes atomic.Int32
	var metrics transport.Hub
	var metricsMu sync.Mutex
	snapshot := metrics.Metrics()
	mux := http.NewServeMux()
	mux.HandleFunc(transport.PathMetrics, func(w http.ResponseWriter, request *http.Request) {
		if scrapes.Add(1) > 1 {
			select {
			case <-lastObserved:
			case <-request.Context().Done():
				return
			}
			if mode == wireProbeMetricsFailure {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		metricsMu.Lock()
		observed := snapshot
		metricsMu.Unlock()
		if err := json.NewEncoder(w).Encode(observed); err != nil {
			t.Errorf("fixture metrics write: %v", err)
		}
	})
	mux.HandleFunc(transport.PathWS, func(w http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{}
		peer, err := upgrader.Upgrade(w, request, nil)
		if err != nil {
			t.Errorf("fixture upgrade: %v", err)
			return
		}
		defer peer.Close()
		_, body, err := peer.ReadMessage()
		if err != nil {
			t.Errorf("fixture HELLO: %v", err)
			return
		}
		frame, err := transport.DecodeFrame(body)
		if err != nil || frame.Opcode != transport.OpHello {
			t.Errorf("fixture did not receive HELLO: %v", err)
			return
		}
		welcome := transport.Frame{Opcode: transport.OpWelcome, Payload: transport.EncodeWelcome(transport.Welcome{SessionID: wireProbeSessionID})}
		if err := peer.WriteMessage(websocket.BinaryMessage, welcome.Encode()); err != nil {
			t.Errorf("fixture WELCOME: %v", err)
			return
		}
		if mode != wireProbeUnrelatedMetrics {
			metricsMu.Lock()
			snapshot.WSOK++
			snapshot.HandshakeCount++
			snapshot.HelloAdmissionAccepted++
			metricsMu.Unlock()
		}
		var held []byte
		for {
			_, body, err := peer.ReadMessage()
			if err != nil {
				return
			}
			frame, err := transport.DecodeFrame(body)
			if err != nil {
				t.Errorf("fixture request codec: %v", err)
				return
			}
			if frame.Opcode == transport.OpPong {
				if len(frame.Payload) == 1 && frame.Payload[0] == wireProbeLastSequence {
					observedOnce.Do(func() { close(lastObserved) })
				}
				continue
			}
			if frame.Opcode != transport.OpPing || len(frame.Payload) != probePayloadBytes {
				t.Errorf("fixture unexpected opcode/size: %d/%d", frame.Opcode, len(frame.Payload))
				return
			}
			firstOnce.Do(func() { close(firstPing) })
			sequence := binary.LittleEndian.Uint64(frame.Payload[probeSequenceOffset:probeStampOffset])
			responses := [][]byte{frame.Payload}
			switch mode {
			case wireProbeDuplicate:
				responses = append(responses, frame.Payload)
			case wireProbeReorder:
				if held == nil {
					held = append([]byte(nil), frame.Payload...)
					continue
				}
				responses = append(responses, held)
			case wireProbeCorrupt:
				frame.Payload[len(frame.Payload)-1] ^= probeCorruptBit
			case wireProbeSilent:
				continue
			case wireProbeUnmatched:
				responses = append(responses, []byte("unmatched echo"))
			}
			for _, payload := range responses {
				response := transport.Frame{Opcode: transport.OpPong, Payload: payload}
				if err := peer.WriteMessage(websocket.BinaryMessage, response.Encode()); err != nil {
					return
				}
			}
			barrier := transport.Frame{Opcode: transport.OpPing, Payload: []byte{byte(sequence)}}
			if err := peer.WriteMessage(websocket.BinaryMessage, barrier.Encode()); err != nil {
				return
			}
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return wireProbeFixture{server: server, firstPing: firstPing}
}

/*
================
wireProbeConfig
================
*/
func wireProbeConfig(fixture wireProbeFixture) measurementConfig {
	return measurementConfig{
		Probe:       probeConfig{Base: fixture.server.URL, Transport: "ws", Ticket: probeTestTicket, Timeout: wireProbeDrain},
		PingSamples: 1, PingInterval: measurementTestInterval,
		BurstSamples: 1, BurstInterval: measurementTestInterval,
	}
}

/*
================
TestMeasurementRejectsBadWireResponses
================
*/
func TestMeasurementRejectsBadWireResponses(t *testing.T) {
	for _, mode := range []wireProbeMode{wireProbeDuplicate, wireProbeReorder, wireProbeCorrupt, wireProbeUnmatched} {
		t.Run(map[wireProbeMode]string{wireProbeDuplicate: "duplicate", wireProbeReorder: "reordered_phases", wireProbeCorrupt: "corrupt_stamp", wireProbeUnmatched: "unmatched_echo"}[mode], func(t *testing.T) {
			fixture := startWireProbeFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), measurementTestTimeout)
			defer cancel()
			report, err := measureProbe(ctx, wireProbeConfig(fixture))
			if err == nil || report.Valid {
				t.Fatalf("bad peer appeared valid: mode=%d report=%+v", mode, report)
			}
			if mode == wireProbeDuplicate && report.PingTrain.DuplicateReplies+report.OrderedBurst.DuplicateReplies == 0 {
				t.Fatal("duplicate echo was lost from the evidence")
			}
			if mode == wireProbeReorder && (report.ReorderedReplies != 1 || report.PingTrain.OutOfOrder != 0 || report.OrderedBurst.OutOfOrder != 0) {
				t.Fatal("cross-phase reordering was lost or confused with per-phase reordering")
			}
			if mode == wireProbeUnmatched && (report.IgnoredPongs == 0 || report.PingTrain.Missing+report.OrderedBurst.Missing != 0) {
				t.Fatal("an unmatched extra echo was silently admitted")
			}
		})
	}
}

/*
================
TestMeasurementPreservesIncompleteMetrics
================
*/
func TestMeasurementPreservesIncompleteMetrics(t *testing.T) {
	fixture := startWireProbeFixture(t, wireProbeMetricsFailure)
	ctx, cancel := context.WithTimeout(context.Background(), measurementTestTimeout)
	defer cancel()
	report, err := measureProbe(ctx, wireProbeConfig(fixture))
	if err == nil || report.Valid || report.Metrics != nil || report.IncompleteBefore == nil || report.HandshakeSamples != 1 ||
		report.PingTrain.Received != 1 || report.OrderedBurst.Received != 1 {
		t.Fatalf("failed scrape turned into zero-filled evidence: %v; %+v", err, report)
	}
}

/*
================
TestMeasurementRejectsUnrelatedMetrics

A successful echo against one endpoint cannot validate another endpoint's
unchanged counters. Keep the raw window and refuse the measurement arm.
================
*/
func TestMeasurementRejectsUnrelatedMetrics(t *testing.T) {
	fixture := startWireProbeFixture(t, wireProbeUnrelatedMetrics)
	ctx, cancel := context.WithTimeout(context.Background(), measurementTestTimeout)
	defer cancel()
	report, err := measureProbe(ctx, wireProbeConfig(fixture))
	if err == nil || report.Valid || report.Metrics == nil || report.PingTrain.Received != 1 || report.OrderedBurst.Received != 1 ||
		report.Metrics.CounterDeltas["ws_ok"] != 0 {
		t.Fatalf("unrelated endpoint counters were accepted: %v; %+v", err, report)
	}
}

/*
================
TestMeasurementCancellationJoinsBlockedReader
================
*/
func TestMeasurementCancellationJoinsBlockedReader(t *testing.T) {
	fixture := startWireProbeFixture(t, wireProbeSilent)
	ctx, cancel := context.WithTimeout(context.Background(), measurementTestTimeout)
	defer cancel()
	go func() {
		select {
		case <-fixture.firstPing:
			cancel()
		case <-ctx.Done():
		}
	}()
	report, err := measureProbe(ctx, wireProbeConfig(fixture))
	if !errors.Is(err, context.Canceled) || report.Valid || report.PingTrain.Missing != 1 || report.OrderedBurst.NotSent != 1 {
		t.Fatalf("cancellation lost partial data or reader failure: %v; %+v", err, report)
	}
}
