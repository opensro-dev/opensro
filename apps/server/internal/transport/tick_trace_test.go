/*
===========================================================================

tick_trace_test.go - the slow-tick trace writer's contract (#2301)

A trace the tick offers must land in a rotating slot without the
tick ever blocking, a full queue must drop and count rather than
wait, and the capture stays off until the variable arms it.

===========================================================================
*/

package transport

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestTickTraceWriterRotatesSlots
================
*/
func TestTickTraceWriterRotatesSlots(t *testing.T) {
	dir := t.TempDir()
	writer := newTickTraceWriter(dir)
	if err := writer.start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < tickTraceSlots+1; i++ {
		writer.offer([]byte("trace"))
	}
	writer.close()
	// One more trace than slots: every slot exists, slot 0 rewritten.
	for slot := uint64(0); slot < tickTraceSlots; slot++ {
		if _, err := os.Stat(filepath.Join(dir, tickTraceFileName(slot))); err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
	}
	written, dropped, writeErrors := writer.snapshot()
	if written != tickTraceSlots+1 || dropped != 0 || writeErrors != 0 {
		t.Fatalf("snapshot = %d/%d/%d, want %d written, no drops or errors",
			written, dropped, writeErrors, tickTraceSlots+1)
	}
}

/*
================
TestTickTraceWriterDropsWhenFull
================
*/
func TestTickTraceWriterDropsWhenFull(t *testing.T) {
	// Never started: nothing drains the queue, so the queue cap is
	// the whole story and the next offer drops.
	writer := newTickTraceWriter(t.TempDir())
	for i := 0; i < tickTraceQueueCap; i++ {
		writer.offer([]byte("trace"))
	}
	writer.offer([]byte("trace"))
	_, dropped, _ := writer.snapshot()
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1 once the queue is full", dropped)
	}
}

/*
================
TestTickTraceEnabledIsOptIn
================
*/
func TestTickTraceEnabledIsOptIn(t *testing.T) {
	t.Setenv(EnvTickTrace, "")
	if TickTraceEnabled() {
		t.Fatal("unset must leave the capture off")
	}
	t.Setenv(EnvTickTrace, "off")
	if TickTraceEnabled() {
		t.Fatal("off must leave the capture off")
	}
	t.Setenv(EnvTickTrace, "on")
	if !TickTraceEnabled() {
		t.Fatal("on must arm the capture")
	}
}
