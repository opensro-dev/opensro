/*
===========================================================================

export_test.go - test-only hooks into the chat runtime

Lets the black-box wire tests pin the clock that stamps public history, so
the replayed send times are exact bytes.

===========================================================================
*/
package chat

import "time"

/*
================
SetClock
================
*/
func (rt *Runtime) SetClock(now func() time.Time) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.now = now
}
