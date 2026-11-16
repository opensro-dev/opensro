/*
===========================================================================

tick_output.go - the tick's two output windows: writes and the commit

A tick opens two windows before its first phase and closes them after its
last hook: the authority's batched commit (tick_commit.go) and, when the
Pusher has one, its write window (TickWindow). The write window lets the
transport carry a session's whole tick output in one write instead of one
write per frame; it closes first, so the tick's frames leave as soon as
its last push is queued, as they did before the commit flush, which then
runs as before. Both close even when the tick is cancelled or panics: the
close is deferred inside the tick's recover boundary.

===========================================================================
*/

package simulation

/*
================
TickWindow

The optional Pusher face for the tick's write window. BeginTick runs
before the tick's first push and EndTick after its last, once per tick,
on the coordinator, while no shard worker runs. Between them a pusher
may hold the writes its pushes cause; it must not reorder, drop or delay
past EndTick anything they enqueued, and pushes outside the window go
out as before.
================
*/
type TickWindow interface {
	BeginTick()
	EndTick()
}

/*
================
Ticker.beginTickOutput

Opens both windows and returns the function that closes them, for the
tick's defer.
================
*/
func (t *Ticker) beginTickOutput() func() {
	t.beginCommit()
	window, _ := t.Push.(TickWindow)
	if window != nil {
		window.BeginTick()
	}
	return func() {
		if window != nil {
			window.EndTick()
		}
		t.flushCommit()
	}
}
