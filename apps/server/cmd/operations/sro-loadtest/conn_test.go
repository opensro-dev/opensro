/*
===========================================================================

conn_test.go - the bot's frame-buffer drop rule

When a bot stops draining, the reader must close rather than stall,
and the drop must be attributable to the bot: the first staircase's
500 step lost 30 bots to a 256-deep buffer during the crowded enter
burst while the server held every session. offerFrame is that rule.
===========================================================================
*/

package main

import (
	"testing"

	"opensro.online/server/internal/transport"
)

/*
================
TestOfferFrameOverflow
================
*/
func TestOfferFrameOverflow(t *testing.T) {
	conn := &gameConn{frames: make(chan transport.Frame, 1), done: make(chan struct{})}
	frame := transport.Frame{Opcode: transport.OpEnterWorldResult}
	if !conn.offerFrame(frame) {
		t.Fatal("first offer refused into an empty buffer")
	}
	if conn.overflow.Load() {
		t.Fatal("overflow set before the buffer filled")
	}
	if conn.offerFrame(frame) {
		t.Fatal("second offer accepted into a full buffer")
	}
	if !conn.overflow.Load() {
		t.Fatal("overflow not recorded when the buffer filled")
	}
}
