/*
===========================================================================

measurement.go - scheduled, sequenced transport observations

One admitted connection runs a ping train and an ordered burst while a
separate reader receives replies and answers keepalives. Timestamps are
local monotonic values; echoed bytes identify a sample, never supply its
clock. The JSON describes one protocol-level arm, not browser performance.

===========================================================================
*/

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"opensro.online/server/internal/transport"
)

const (
	defaultPingSamples    = 1200
	defaultPingInterval   = 100 * time.Millisecond
	defaultBurstSamples   = 200
	defaultBurstInterval  = 50 * time.Millisecond
	maxMeasurementSamples = 100_000
	maxMeasurementBudget  = time.Hour
	measurementStages     = 5
	measurementSchema     = 1
	probePayloadMarker    = "SROPRB01"
	probeSequenceOffset   = 8
	probeStampOffset      = 16
	probePayloadBytes     = 24
	probeTrainPhase       = 0
	probeBurstPhase       = 1
	probePhaseCount       = 2
	maxProbeResponses     = ^uint64(0)
)

/*
================
measurementConfig

The paced burst fits the default admission budget (25 frames/s, burst75).
An operator may explicitly request an unpaced burst on an isolated arm;
rate-limited frames then invalidate that arm rather than become network loss.
================
*/
type measurementConfig struct {
	Probe         probeConfig
	PingSamples   int
	PingInterval  time.Duration
	BurstSamples  int
	BurstInterval time.Duration
}

/*
================
budget

Validate before multiplying durations. The extra stage budgets cover
metrics before/after, admission and both final response-drain windows.
================
*/
func (cfg measurementConfig) budget() (time.Duration, error) {
	if cfg.PingSamples < 1 || cfg.PingSamples > maxMeasurementSamples ||
		cfg.BurstSamples < 1 || cfg.BurstSamples > maxMeasurementSamples {
		return 0, fmt.Errorf("each schedule needs 1..%d samples", maxMeasurementSamples)
	}
	if cfg.PingInterval <= 0 || cfg.PingInterval > maxMeasurementBudget ||
		cfg.BurstInterval < 0 || cfg.BurstInterval > maxMeasurementBudget ||
		cfg.Probe.Timeout <= 0 || cfg.Probe.Timeout > maxMeasurementBudget {
		return 0, fmt.Errorf("invalid schedule interval or response timeout")
	}
	duration := time.Duration(cfg.PingSamples-1)*cfg.PingInterval +
		time.Duration(cfg.BurstSamples-1)*cfg.BurstInterval + measurementStages*cfg.Probe.Timeout
	if duration > maxMeasurementBudget {
		return 0, fmt.Errorf("measurement budget exceeds %s", maxMeasurementBudget)
	}
	return duration, nil
}

/*
================
measurementSettings

Only public configuration is serialized. Admission tickets and their
environment variable never become measurement artifacts.
================
*/
type measurementSettings struct {
	Target        string        `json:"target"`
	WTAddress     string        `json:"wt_address,omitempty"`
	Transport     string        `json:"transport"`
	PingSamples   int           `json:"ping_samples"`
	PingInterval  time.Duration `json:"ping_interval_ns"`
	BurstSamples  int           `json:"burst_samples"`
	BurstInterval time.Duration `json:"burst_interval_ns"`
	DrainTimeout  time.Duration `json:"response_timeout_ns"`
}

/*
================
measurementReport

Counter snapshots are shared-server observations. Their scope and the
sample count prevent one handshake or lifetime percentile being presented
as a measured run distribution.
================
*/
type measurementReport struct {
	Schema           int                 `json:"schema"`
	StartedAt        time.Time           `json:"started_at"`
	Settings         measurementSettings `json:"settings"`
	Valid            bool                `json:"valid"`
	Error            string              `json:"error,omitempty"`
	HandshakeSamples int                 `json:"handshake_samples"`
	HelloToWelcome   time.Duration       `json:"hello_to_welcome_ns"`
	PingTrain        sampleSummary       `json:"ping_train"`
	OrderedBurst     sampleSummary       `json:"ordered_burst"`
	KeepaliveReplies uint64              `json:"keepalive_replies"`
	IgnoredPongs     uint64              `json:"ignored_pongs"`
	ReorderedReplies uint64              `json:"reordered_replies"`
	Metrics          *probeMetricsReport `json:"metrics,omitempty"`
	IncompleteBefore *transport.Metrics  `json:"metrics_before_if_incomplete,omitempty"`
	PercentileRule   string              `json:"percentile_rule"`
	JitterRule       string              `json:"jitter_rule"`
	StallRule        string              `json:"stall_rule"`
}

/*
================
probeSampleRef
================
*/
type probeSampleRef struct {
	phase int
	index int
	stamp uint64
}

/*
================
probeRecorder

The sender owns sequence allocation; the receiver owns first receipt.
One mutex publishes complete samples and coalesced wakeups to the sender.
No arrival can see an unregistered sample, including loopback replies
which arrive before WriteFrame returns.
================
*/
type probeRecorder struct {
	mu               sync.Mutex
	samples          [probePhaseCount][]probeSample
	starts           [probePhaseCount]time.Time
	refs             map[uint64]probeSampleRef
	next             uint64
	keepaliveReplies uint64
	ignoredPongs     uint64
	highestReceived  uint64
	reorderedReplies uint64
	readError        error
	stopping         bool
	changed          chan struct{}
	readDone         chan struct{}
}

/*
================
newProbeRecorder
================
*/
func newProbeRecorder() *probeRecorder {
	return &probeRecorder{
		refs: make(map[uint64]probeSampleRef), changed: make(chan struct{}, 1), readDone: make(chan struct{}),
	}
}

/*
================
notify
================
*/
func (recorder *probeRecorder) notify() {
	select {
	case recorder.changed <- struct{}{}:
	default:
	}
}

/*
================
send

Register before writing: a successful local write is not a receive barrier.
The marker, sequence and echoed send offset all have to match our ledger.
================
*/
func (recorder *probeRecorder) send(client *probeClient, phase int) error {
	recorder.mu.Lock()
	recorder.next++
	sequence := recorder.next
	at := time.Now()
	stamp := uint64(at.Sub(recorder.starts[phase]))
	index := len(recorder.samples[phase])
	recorder.samples[phase] = append(recorder.samples[phase], probeSample{Sequence: sequence, SentAt: at})
	recorder.refs[sequence] = probeSampleRef{phase: phase, index: index, stamp: stamp}
	recorder.mu.Unlock()

	payload := make([]byte, probePayloadBytes)
	copy(payload, probePayloadMarker)
	binary.LittleEndian.PutUint64(payload[probeSequenceOffset:probeStampOffset], sequence)
	binary.LittleEndian.PutUint64(payload[probeStampOffset:], stamp)
	err := client.writeFrame(transport.Frame{Opcode: transport.OpPing, Payload: payload})
	recorder.mu.Lock()
	recorder.samples[phase][index].Written = err == nil
	recorder.mu.Unlock()
	return err
}

/*
================
receivePong
================
*/
func (recorder *probeRecorder) receivePong(payload []byte, at time.Time) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(payload) < len(probePayloadMarker) || string(payload[:len(probePayloadMarker)]) != probePayloadMarker {
		recorder.ignoredPongs++
		return nil
	}
	if len(payload) != probePayloadBytes {
		return fmt.Errorf("marked probe PONG has %d bytes, want %d", len(payload), probePayloadBytes)
	}
	sequence := binary.LittleEndian.Uint64(payload[probeSequenceOffset:probeStampOffset])
	stamp := binary.LittleEndian.Uint64(payload[probeStampOffset:])
	ref, exists := recorder.refs[sequence]
	if !exists || ref.stamp != stamp {
		return fmt.Errorf("probe PONG does not match a sent sequence and timestamp")
	}
	sample := &recorder.samples[ref.phase][ref.index]
	if sample.Responses == maxProbeResponses {
		return fmt.Errorf("probe response count overflow")
	}
	if sample.Responses == 0 {
		sample.ReceivedAt = at
		if sequence < recorder.highestReceived {
			recorder.reorderedReplies++
		}
		recorder.highestReceived = max(recorder.highestReceived, sequence)
	}
	sample.Responses++
	recorder.notify()
	return nil
}

/*
================
read

Reading never waits for the next scheduled send. Server keepalives share
the client's serialized writer and are not RTT observations.
================
*/
func (recorder *probeRecorder) read(client *probeClient) {
	var readError error
	defer func() {
		recorder.completeRead(readError)
	}()
	for {
		frame, err := client.readFrame()
		at := time.Now()
		if err != nil {
			readError = err
			return
		}
		switch frame.Opcode {
		case transport.OpPing:
			if err := client.replyPing(frame); err != nil {
				readError = err
				return
			}
			recorder.mu.Lock()
			recorder.keepaliveReplies++
			recorder.mu.Unlock()
		case transport.OpPong:
			if len(frame.Payload) > transport.MaxPingPayloadBytes {
				readError = fmt.Errorf("PONG exceeds the transport payload bound")
				return
			}
			if err := recorder.receivePong(frame.Payload, at); err != nil {
				readError = err
				return
			}
		default:
			readError = fmt.Errorf("unexpected opcode 0x%04X during measurement", frame.Opcode)
			return
		}
	}
}

/*
================
completeRead

Only I/O errors produced by our explicit stop are expected. Protocol
failures remain failures even if their publication races with shutdown.
Gorilla exposes a timeout as net.Error without unwrapping its deadline;
that observable timeout contract must also be recognized after our stop.
================
*/
func (recorder *probeRecorder) completeRead(err error) {
	recorder.mu.Lock()
	var timeout net.Error
	deadline := errors.Is(err, os.ErrDeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
	if recorder.stopping && (errors.Is(err, net.ErrClosed) || deadline) {
		err = nil
	}
	recorder.readError = err
	recorder.mu.Unlock()
	close(recorder.readDone)
}

/*
================
finish

Join the reader before deciding validity. Checking a done channel without
joining could miss a decoded protocol error about to be published.
================
*/
func (recorder *probeRecorder) finish(interrupt func() error) error {
	recorder.mu.Lock()
	recorder.stopping = true
	recorder.mu.Unlock()
	err := interrupt()
	<-recorder.readDone
	return errors.Join(err, recorder.readerError())
}

/*
================
readerError
================
*/
func (recorder *probeRecorder) readerError() error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.readError
}

/*
================
pending
================
*/
func (recorder *probeRecorder) pending(phase int) bool {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, sample := range recorder.samples[phase] {
		if sample.Written && sample.Responses == 0 {
			return true
		}
	}
	return false
}

/*
================
probeSchedule
================
*/
type probeSchedule struct {
	phase    int
	count    int
	interval time.Duration
}

/*
================
runSchedule

The clock is independent of receive latency. Absolute deadlines preserve
the offered schedule; raw send offsets expose local scheduling delays.
================
*/
func (recorder *probeRecorder) runSchedule(ctx context.Context, client *probeClient, schedule probeSchedule) error {
	started := time.Now()
	recorder.mu.Lock()
	recorder.starts[schedule.phase] = started
	recorder.mu.Unlock()
	for index := 0; index < schedule.count; index++ {
		timer := time.NewTimer(time.Until(started.Add(time.Duration(index) * schedule.interval)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-recorder.readDone:
			timer.Stop()
			return recorder.readerError()
		case <-timer.C:
		}
		if err := recorder.send(client, schedule.phase); err != nil {
			return err
		}
	}
	return nil
}

/*
================
drain

No reply is invented for a timed-out request. Both schedules still run
when a peer drops some pings; their final missing counts invalidate the arm.
================
*/
func (recorder *probeRecorder) drain(ctx context.Context, phase int, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for recorder.pending(phase) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-recorder.readDone:
			return recorder.readerError()
		case <-timer.C:
			return nil
		case <-recorder.changed:
		}
	}
	return nil
}

/*
================
measureProbe

Always return a report, including partial observations on failure. Only
completed before/after snapshots receive a delta; a failed second scrape
retains the first without presenting zero-filled data as an observation.
================
*/
func measureProbe(ctx context.Context, cfg measurementConfig) (report measurementReport, result error) {
	report = measurementReport{
		Schema: measurementSchema, StartedAt: time.Now().UTC(),
		Settings: measurementSettings{
			Transport:   cfg.Probe.Transport,
			PingSamples: cfg.PingSamples, PingInterval: cfg.PingInterval,
			BurstSamples: cfg.BurstSamples, BurstInterval: cfg.BurstInterval, DrainTimeout: cfg.Probe.Timeout,
		},
		PercentileRule: "nearest_rank_ceil; durations_ns",
		JitterRule:     "mean(abs(receive_gap_ns-send_gap_ns)/2); unique_replies_in_arrival_order",
		StallRule:      "unique_receipt_gap_ns > 250000000; first_gap_from_phase_start",
	}
	if report.Settings.Transport == "" {
		report.Settings.Transport = "wt"
	}
	recorder := newProbeRecorder()
	var client *probeClient
	readerStarted := false
	defer func() {
		if readerStarted {
			result = errors.Join(result, recorder.finish(client.interrupt))
		}
		if client != nil {
			client.close()
		}
		if ctx.Err() != nil {
			result = errors.Join(result, ctx.Err())
		}
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		if recorder.starts[probeTrainPhase].IsZero() {
			recorder.starts[probeTrainPhase] = report.StartedAt
		}
		if recorder.starts[probeBurstPhase].IsZero() {
			recorder.starts[probeBurstPhase] = report.StartedAt
		}
		var err error
		report.PingTrain, err = summarizeSamples(recorder.starts[probeTrainPhase], cfg.PingSamples, recorder.samples[probeTrainPhase])
		result = errors.Join(result, err)
		report.OrderedBurst, err = summarizeSamples(recorder.starts[probeBurstPhase], cfg.BurstSamples, recorder.samples[probeBurstPhase])
		result = errors.Join(result, err)
		report.KeepaliveReplies, report.IgnoredPongs = recorder.keepaliveReplies, recorder.ignoredPongs
		if report.IgnoredPongs != 0 {
			result = errors.Join(result, fmt.Errorf("peer returned PONGs outside the measurement ledger"))
		}
		report.ReorderedReplies = recorder.reorderedReplies
		if report.ReorderedReplies != 0 {
			result = errors.Join(result, fmt.Errorf("replies crossed the global send order"))
		}
		for _, phase := range []sampleSummary{report.PingTrain, report.OrderedBurst} {
			if phase.Missing != 0 || phase.NotSent != 0 || phase.DuplicateReplies != 0 || phase.OutOfOrder != 0 {
				result = errors.Join(result, fmt.Errorf("schedule has missing, unsent, duplicate or reordered replies"))
			}
		}
		if result != nil {
			report.Error = result.Error()
		}
		report.Valid = result == nil
	}()
	if _, err := cfg.budget(); err != nil {
		return report, &stageError{"config", err}
	}
	var err error
	client, err = newProbeClient(cfg.Probe)
	if err != nil {
		return report, err
	}
	report.Settings.Target, report.Settings.WTAddress = client.cfg.Base, client.cfg.WTAddr
	before, err := readProbeMetrics(ctx, client.http, client.cfg.Base)
	if err != nil {
		return report, &stageError{"metrics-before", err}
	}
	report.IncompleteBefore = &before
	if err := client.open(ctx); err != nil {
		return report, err
	}
	report.HandshakeSamples, report.HelloToWelcome = 1, client.handshake
	readerStarted = true
	go recorder.read(client)
	for _, schedule := range []probeSchedule{
		{probeTrainPhase, cfg.PingSamples, cfg.PingInterval},
		{probeBurstPhase, cfg.BurstSamples, cfg.BurstInterval},
	} {
		if err := recorder.runSchedule(ctx, client, schedule); err != nil {
			return report, &stageError{"schedule", err}
		}
		if err := recorder.drain(ctx, schedule.phase, cfg.Probe.Timeout); err != nil {
			return report, &stageError{"receive", err}
		}
	}
	after, err := readProbeMetrics(ctx, client.http, client.cfg.Base)
	if err != nil {
		return report, &stageError{"metrics-after", err}
	}
	metrics, err := compareProbeMetrics(before, after)
	report.Metrics, report.IncompleteBefore = &metrics, nil
	if err != nil {
		return report, &stageError{"metrics-window", err}
	}
	handshakeCounter := "wt_ok"
	if client.cfg.Transport == "ws" {
		handshakeCounter = "ws_ok"
	}
	if metrics.CounterDeltas[handshakeCounter] == 0 {
		return report, &stageError{"metrics-window", fmt.Errorf("metrics do not include the arm's %s handshake", client.cfg.Transport)}
	}
	for _, counter := range []string{"rate_limited_frames", "write_errors", "empty_frames_refused", "sessions_closed_slow_consumer"} {
		if metrics.CounterDeltas[counter] != 0 {
			return report, &stageError{"metrics-window", fmt.Errorf("server %s increased during the arm", counter)}
		}
	}
	return report, nil
}
