/*
===========================================================================

tick_trace.go - slow-tick Perfetto traces in the GameWorld state root

The spike journal (tick_spikes.go) keeps that a tick burst
happened; these files keep what ran inside one slow tick: the zone
timeline the simulation records while SRO_TICK_TRACE arms it
(simulation ticktrace.go, #2301). Each slow tick becomes one
Perfetto (chrome trace) JSON file that opens in an existing viewer
- no viewer is built. The tick never blocks on disk: traces go
through a bounded queue to one writer goroutine, a full queue
drops the trace and counts the drop, and the files rotate over a
few slots so the directory stays bounded. The counters ride the
metrics snapshot beside the journal's, so capture can be watched
live. Diagnostics only: no wire, store or protocol surface.

Port-only tooling, not native behaviour, and off by default:
SRO_TICK_TRACE=on arms the capture. The rolling in-memory
recorder that dumps on an over-budget tick is #2302.

===========================================================================
*/

package transport

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// EnvTickTrace arms the slow-tick zone capture: "on" (or "1")
// enables it; unset or anything else leaves the capture off and
// the tick pays one nil check per zone point.
const EnvTickTrace = "SRO_TICK_TRACE"

const (
	// Traces buffered for the writer before drops are counted.
	// Slow ticks are rare; eight is deep enough for a burst.
	tickTraceQueueCap = 8
	// Rotating file slots bound the trace directory: four slow
	// ticks' timelines, newest overwriting oldest in turn.
	tickTraceSlots = 4
)

/*
================
TickTraceEnabled

The opt-in: anything but an explicit on is off, so a shard that
never sets the variable runs the native-cost path.
================
*/
func TickTraceEnabled() bool {
	return os.Getenv(EnvTickTrace) == "on" || os.Getenv(EnvTickTrace) == "1"
}

/*
================
tickTraceFileName

One rotating slot's file inside the GameWorld state root.
================
*/
func tickTraceFileName(slot uint64) string {
	return fmt.Sprintf("tick-trace-%d.json", slot%tickTraceSlots)
}

/*
================
tickTraceWriter

The bounded queue and its writer goroutine. The writer owns the
slot rotation; the counters are atomic because the tick lane and
the metrics snapshot read them without the queue.
================
*/
type tickTraceWriter struct {
	dir      string
	queue    chan []byte
	wait     sync.WaitGroup
	slot     uint64 // writer goroutine only
	written  atomic.Uint64
	dropped  atomic.Uint64
	writeErr atomic.Uint64
}

/*
================
newTickTraceWriter
================
*/
func newTickTraceWriter(dir string) *tickTraceWriter {
	return &tickTraceWriter{dir: dir, queue: make(chan []byte, tickTraceQueueCap)}
}

/*
================
tickTraceWriter.start

Creates the state directory and launches the writer goroutine.
================
*/
func (w *tickTraceWriter) start() error {
	if w.dir != "" && w.dir != "." {
		if err := os.MkdirAll(w.dir, 0o755); err != nil {
			return err
		}
	}
	w.wait.Add(1)
	go w.run()
	return nil
}

/*
================
tickTraceWriter.run

One write per trace into the next rotating slot: a crash loses at
most the trace in flight and no handle outlives its file.
================
*/
func (w *tickTraceWriter) run() {
	defer w.wait.Done()
	for data := range w.queue {
		path := filepath.Join(w.dir, tickTraceFileName(w.slot))
		w.slot++
		if err := os.WriteFile(path, data, 0o644); err != nil {
			w.writeErr.Add(1)
			continue
		}
		w.written.Add(1)
	}
}

/*
================
tickTraceWriter.offer

Hands one rendered trace to the writer; never blocks. A full
queue drops the trace and counts it: capture must not slow the
tick it describes.
================
*/
func (w *tickTraceWriter) offer(data []byte) {
	select {
	case w.queue <- data:
	default:
		w.dropped.Add(1)
	}
}

/*
================
tickTraceWriter.snapshot

The metrics mirror: traces written, dropped and failed.
================
*/
func (w *tickTraceWriter) snapshot() (written, dropped, writeErrors uint64) {
	return w.written.Load(), w.dropped.Load(), w.writeErr.Load()
}

/*
================
tickTraceWriter.close

Drains the queue and stops the writer. Production never closes
(the process exits); tests use it to read finished files.
================
*/
func (w *tickTraceWriter) close() {
	close(w.queue)
	w.wait.Wait()
}

//============================================================================

/*
================
Hub.EnableTickTrace

Starts the slow-tick trace writer in dir. Diagnostics only: the
caller logs a failure and runs without capture - recording must
never stop the server booting.
================
*/
func (h *Hub) EnableTickTrace(dir string) error {
	writer := newTickTraceWriter(dir)
	if err := writer.start(); err != nil {
		return err
	}
	h.metrics.traces = writer
	return nil
}

/*
================
Hub.RecordTickTrace

Offers one slow tick's rendered Perfetto JSON to the writer.
Until EnableTickTrace installs one the trace is dropped
uncounted: the boot window is not a capture failure.
================
*/
func (h *Hub) RecordTickTrace(data []byte) {
	if writer := h.metrics.traces; writer != nil {
		writer.offer(data)
	}
}
