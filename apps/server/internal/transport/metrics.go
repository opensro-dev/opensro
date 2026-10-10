package transport

import (
	"sync"
	"sync/atomic"
	"time"
)

// hubMetrics owns every transport counter. Keeping observability state behind
// one component prevents the live-session registry from becoming a catch-all
// for unrelated synchronization.
type hubMetrics struct {
	wtOK      atomic.Uint64
	wsOK      atomic.Uint64
	rlDropped atomic.Uint64

	handshakeBusy             atomic.Uint64
	accountSessionEvictions   atomic.Uint64
	handshakeHelloTimeout     atomic.Uint64
	handshakeReadFailed       atomic.Uint64
	handshakeProtocolRejected atomic.Uint64
	resumeAttempted           atomic.Uint64
	resumeAccepted            atomic.Uint64
	resumeFresh               atomic.Uint64
	helloAdmissionAccepted    atomic.Uint64
	helloAdmissionRefused     atomic.Uint64
	detaches                  atomic.Uint64
	detachesIdle              atomic.Uint64
	detachesOther             atomic.Uint64

	sessOpened            atomic.Uint64
	sessClosed            atomic.Uint64
	closedClean           atomic.Uint64
	closedGrace           atomic.Uint64
	closedSlow            atomic.Uint64
	closedPanic           atomic.Uint64
	closedOther           atomic.Uint64
	framesIn              atomic.Uint64
	bytesIn               atomic.Uint64
	framesOut             atomic.Uint64
	bytesOut              atomic.Uint64
	datagramsOut          atomic.Uint64
	datagramBytesOut      atomic.Uint64
	writeErrors           atomic.Uint64
	enterWorldAuthRefused atomic.Uint64
	unhandledFrames       atomic.Uint64
	emptyFramesRefused    atomic.Uint64
	queueHighWater        atomic.Uint64
	queueByteHighWater    atomic.Uint64
	byeNormal             atomic.Uint64
	byeShutdown           atomic.Uint64
	byeReplaced           atomic.Uint64
	byeOther              atomic.Uint64

	handshakes durationStats
	ticks      tickStats
	timing     timingStats // timing.go
}

// Metrics is a point-in-time snapshot of the hub's transport counters.
// The first four keys predate the rest and their names and meanings are
// frozen: dashboards and out-of-process tooling read them.
type Metrics struct {
	// WTOk counts completed handshakes (fresh or resumed) on WebTransport.
	WTOk uint64 `json:"wt_ok"`
	// WSOK counts completed handshakes on the WebSocket transport.
	WSOK uint64 `json:"ws_ok"`
	// LiveSessions is the current session count.
	LiveSessions int `json:"live_sessions"`
	// RateLimitedFrames counts inbound frames dropped by the per-session
	// rate limiter since boot.
	RateLimitedFrames      uint64 `json:"rate_limited_frames"`
	AttachedSessions       int    `json:"attached_sessions"`
	DetachedSessions       int    `json:"detached_sessions"`
	BoundSessions          int    `json:"bound_sessions"`
	OutboundQueueDepth     int    `json:"outbound_queue_depth"`
	OutboundQueueHighWater uint64 `json:"outbound_queue_high_water"`

	HandshakeRejectedBusy     uint64  `json:"handshake_rejected_busy"`
	AccountSessionEvictions   uint64  `json:"account_session_evictions"`
	HandshakeHelloTimeout     uint64  `json:"handshake_hello_timeout"`
	HandshakeReadFailed       uint64  `json:"handshake_read_failed"`
	HandshakeProtocolRejected uint64  `json:"handshake_protocol_rejected"`
	HandshakeCount            uint64  `json:"handshake_count"`
	HandshakeLastMs           float64 `json:"handshake_last_ms"`
	HandshakeMeanMs           float64 `json:"handshake_mean_ms"`
	HandshakeMaxMs            float64 `json:"handshake_max_ms"`
	ResumeAttempted           uint64  `json:"resume_attempted"`
	ResumeAccepted            uint64  `json:"resume_accepted"`
	ResumeFresh               uint64  `json:"resume_fresh"`
	HelloAdmissionAccepted    uint64  `json:"hello_admission_accepted"`
	HelloAdmissionRefused     uint64  `json:"hello_admission_refused"`
	Detaches                  uint64  `json:"detaches"`
	DetachesIdle              uint64  `json:"detaches_idle"`
	DetachesOther             uint64  `json:"detaches_other"`

	// Session lifecycle since boot. The closed breakdown is deliberately
	// coarse: "other" includes transport errors, protocol violations, and
	// shutdown-deadline kills.
	SessionsOpened             uint64 `json:"sessions_opened"`
	SessionsClosed             uint64 `json:"sessions_closed"`
	SessionsClosedClean        uint64 `json:"sessions_closed_clean"`
	SessionsClosedGraceExpired uint64 `json:"sessions_closed_grace_expired"`
	SessionsClosedSlowConsumer uint64 `json:"sessions_closed_slow_consumer"`
	SessionsClosedHandlerPanic uint64 `json:"sessions_closed_handler_panic"`
	SessionsClosedOther        uint64 `json:"sessions_closed_other"`

	// FramesIn counts every inbound frame accepted by a session read loop,
	// including frames later dropped by rate limiting.
	FramesIn              uint64 `json:"frames_in"`
	BytesIn               uint64 `json:"bytes_in"`
	FramesOut             uint64 `json:"frames_out"`
	BytesOut              uint64 `json:"bytes_out"`
	DatagramsOut          uint64 `json:"datagrams_out"`
	DatagramBytesOut      uint64 `json:"datagram_bytes_out"`
	WriteErrors           uint64 `json:"write_errors"`
	EnterWorldAuthRefused uint64 `json:"enter_world_auth_refused"`
	UnhandledOpcodeFrames uint64 `json:"unhandled_opcode_frames"`
	// EmptyFramesRefused counts outbound opcode-0 frames the queue refused
	// (emptyframe.go); any non-zero value is a producer bug to fix.
	EmptyFramesRefused         uint64 `json:"empty_frames_refused"`
	OutboundQueueBytes         int    `json:"outbound_queue_bytes"`
	OutboundQueueByteHighWater uint64 `json:"outbound_queue_byte_high_water"`
	ByeNormal                  uint64 `json:"bye_normal"`
	ByeShutdown                uint64 `json:"bye_shutdown"`
	ByeReplaced                uint64 `json:"bye_replaced"`
	ByeOther                   uint64 `json:"bye_other"`

	TickCount    uint64  `json:"tick_count"`
	TickLastMs   float64 `json:"tick_last_ms"`
	TickMeanMs   float64 `json:"tick_mean_ms"`
	TickMaxMs    float64 `json:"tick_max_ms"`
	TickOverruns uint64  `json:"tick_overruns"`
	// Request handler and tick phase histograms (timing.go).
	HandlerMs   map[string]Histogram     `json:"handler_ms"`
	TickPhaseMs map[string]Histogram     `json:"tick_phase_ms"`
	SlowHooks   map[string]SlowHookStats `json:"slow_hooks"`
	// Divisions whose own work in a tick ran >= 100 ms, by division ID.
	SlowDivisions map[string]SlowHookStats `json:"slow_divisions"`
}

// Metrics snapshots the counters and the current live-session count.
func (h *Hub) Metrics() Metrics {
	h.mu.RLock()
	live := len(h.sessions)
	bound := len(h.bindings)
	sessions := make([]*Session, 0, live)
	for _, session := range h.sessions {
		sessions = append(sessions, session)
	}
	h.mu.RUnlock()
	attached := 0
	detached := 0
	queueDepth := 0
	queueBytes := 0
	for _, session := range sessions {
		isAttached, depth, bytes := session.metricsSnapshot()
		if isAttached {
			attached++
		} else {
			detached++
		}
		queueDepth += depth
		queueBytes += bytes
	}
	metrics := Metrics{
		WTOk:                   h.metrics.wtOK.Load(),
		WSOK:                   h.metrics.wsOK.Load(),
		LiveSessions:           live,
		RateLimitedFrames:      h.metrics.rlDropped.Load(),
		AttachedSessions:       attached,
		DetachedSessions:       detached,
		BoundSessions:          bound,
		OutboundQueueDepth:     queueDepth,
		OutboundQueueHighWater: h.metrics.queueHighWater.Load(),

		HandshakeRejectedBusy:     h.metrics.handshakeBusy.Load(),
		AccountSessionEvictions:   h.metrics.accountSessionEvictions.Load(),
		HandshakeHelloTimeout:     h.metrics.handshakeHelloTimeout.Load(),
		HandshakeReadFailed:       h.metrics.handshakeReadFailed.Load(),
		HandshakeProtocolRejected: h.metrics.handshakeProtocolRejected.Load(),
		ResumeAttempted:           h.metrics.resumeAttempted.Load(),
		ResumeAccepted:            h.metrics.resumeAccepted.Load(),
		ResumeFresh:               h.metrics.resumeFresh.Load(),
		HelloAdmissionAccepted:    h.metrics.helloAdmissionAccepted.Load(),
		HelloAdmissionRefused:     h.metrics.helloAdmissionRefused.Load(),
		Detaches:                  h.metrics.detaches.Load(),
		DetachesIdle:              h.metrics.detachesIdle.Load(),
		DetachesOther:             h.metrics.detachesOther.Load(),

		SessionsOpened:             h.metrics.sessOpened.Load(),
		SessionsClosed:             h.metrics.sessClosed.Load(),
		SessionsClosedClean:        h.metrics.closedClean.Load(),
		SessionsClosedGraceExpired: h.metrics.closedGrace.Load(),
		SessionsClosedSlowConsumer: h.metrics.closedSlow.Load(),
		SessionsClosedHandlerPanic: h.metrics.closedPanic.Load(),
		SessionsClosedOther:        h.metrics.closedOther.Load(),

		FramesIn:                   h.metrics.framesIn.Load(),
		BytesIn:                    h.metrics.bytesIn.Load(),
		FramesOut:                  h.metrics.framesOut.Load(),
		BytesOut:                   h.metrics.bytesOut.Load(),
		DatagramsOut:               h.metrics.datagramsOut.Load(),
		DatagramBytesOut:           h.metrics.datagramBytesOut.Load(),
		WriteErrors:                h.metrics.writeErrors.Load(),
		EnterWorldAuthRefused:      h.metrics.enterWorldAuthRefused.Load(),
		UnhandledOpcodeFrames:      h.metrics.unhandledFrames.Load(),
		EmptyFramesRefused:         h.metrics.emptyFramesRefused.Load(),
		OutboundQueueBytes:         queueBytes,
		OutboundQueueByteHighWater: h.metrics.queueByteHighWater.Load(),
		ByeNormal:                  h.metrics.byeNormal.Load(),
		ByeShutdown:                h.metrics.byeShutdown.Load(),
		ByeReplaced:                h.metrics.byeReplaced.Load(),
		ByeOther:                   h.metrics.byeOther.Load(),
	}
	h.metrics.handshakes.snapshotHandshakeInto(&metrics)
	h.metrics.ticks.snapshotInto(&metrics)
	h.metrics.timing.snapshotInto(&metrics)
	return metrics
}

// observeQueueDepth raises the process-lifetime high-water mark without
// taking a lock on the transport hot path.
func (h *Hub) observeQueueDepth(depth, bytes int) {
	if depth <= 0 {
		depth = 0
	}
	if depth > 0 {
		wanted := uint64(depth)
		for {
			current := h.metrics.queueHighWater.Load()
			if current >= wanted || h.metrics.queueHighWater.CompareAndSwap(current, wanted) {
				break
			}
		}
	}
	if bytes <= 0 {
		return
	}
	wanted := uint64(bytes)
	for {
		current := h.metrics.queueByteHighWater.Load()
		if current >= wanted || h.metrics.queueByteHighWater.CompareAndSwap(current, wanted) {
			return
		}
	}
}

// recordFrameOut accounts only frames the active connection accepted. A
// queued frame is intentionally not counted until its write succeeds.
func (h *Hub) recordFrameOut(frame Frame) {
	h.metrics.framesOut.Add(1)
	h.metrics.bytesOut.Add(uint64(frame.EncodedLen()))
	if frame.Opcode != OpBye || len(frame.Payload) == 0 {
		return
	}
	switch frame.Payload[0] {
	case ByeReasonNormal:
		h.metrics.byeNormal.Add(1)
	case ByeReasonShutdown:
		h.metrics.byeShutdown.Add(1)
	case ByeReasonReplaced:
		h.metrics.byeReplaced.Add(1)
	default:
		h.metrics.byeOther.Add(1)
	}
}

// countAttach records one completed handshake by transport kind.
func (h *Hub) countAttach(kind string) {
	switch kind {
	case "webtransport":
		h.metrics.wtOK.Add(1)
	case "websocket":
		h.metrics.wsOK.Add(1)
	}
}

// The simulation-tick duration seam. The 100ms tick itself lives in
// internal/game/world/simulation and must stay transport-free, so the RECORDING side lives
// here: whoever runs the tick calls Hub.RecordTickDuration once per tick
// and the numbers surface on /transport/metrics. Unwired, everything here
// is a no-op and the tick keys just read zero.
//
// worldsession.NewTicker wires it automatically (a TickHook appended
// after the caller's hooks measures the tick and calls this seam), so the
// production composition in main needs nothing extra. A tick owner that
// composes simulation.NewTicker directly instead should time RunTick itself:
//
//	start := time.Now()
//	ticker.RunTick(nowMs)
//	hub.RecordTickDuration(time.Since(start), ticker.Interval)

// tickStats aggregates tick durations under one small mutex — recorded at
// 10Hz and snapshotted on metrics scrapes, so contention is irrelevant and
// a mutex beats juggling torn atomic float pairs.
type tickStats struct {
	mu       sync.Mutex
	count    uint64
	last     time.Duration
	sum      time.Duration
	max      time.Duration
	overruns uint64
}

// durationStats is shared by low-frequency lifecycle duration aggregates.
// A mutex keeps each count/sum/max snapshot coherent.
type durationStats struct {
	mu    sync.Mutex
	count uint64
	last  time.Duration
	sum   time.Duration
	max   time.Duration
}

func (stats *durationStats) record(elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	stats.mu.Lock()
	stats.count++
	stats.last = elapsed
	stats.sum += elapsed
	if elapsed > stats.max {
		stats.max = elapsed
	}
	stats.mu.Unlock()
}

func (stats *durationStats) snapshotHandshakeInto(metrics *Metrics) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	metrics.HandshakeCount = stats.count
	metrics.HandshakeLastMs = float64(stats.last) / float64(time.Millisecond)
	metrics.HandshakeMaxMs = float64(stats.max) / float64(time.Millisecond)
	if stats.count > 0 {
		metrics.HandshakeMeanMs = float64(stats.sum) / float64(time.Millisecond) / float64(stats.count)
	}
}

// RecordTickDuration records one simulation tick that took elapsed against a
// budget of interval (the tick cadence); elapsed > interval counts as an
// overrun, interval <= 0 disables overrun accounting for that sample.
// Safe for concurrent use; cheap enough for any per-tick cadence.
func (h *Hub) RecordTickDuration(elapsed, interval time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	t := &h.metrics.ticks
	t.mu.Lock()
	t.count++
	t.last = elapsed
	t.sum += elapsed
	if elapsed > t.max {
		t.max = elapsed
	}
	if interval > 0 && elapsed > interval {
		t.overruns++
	}
	t.mu.Unlock()
}

// snapshotInto copies the tick aggregates into a Metrics snapshot.
func (t *tickStats) snapshotInto(m *Metrics) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m.TickCount = t.count
	m.TickLastMs = float64(t.last) / float64(time.Millisecond)
	m.TickMaxMs = float64(t.max) / float64(time.Millisecond)
	m.TickOverruns = t.overruns
	if t.count > 0 {
		m.TickMeanMs = float64(t.sum) / float64(time.Millisecond) / float64(t.count)
	}
}
