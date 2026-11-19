/*
===========================================================================

petlifetime.go - the remaining-time field of the port-only pet frames

The shield, spirit and howl frames each carry "milliseconds left" and the
client refuses a value past the effect's lifetime. The pet tick reads its
clock once at the start of the tick, while an order handled during that
tick (a cast, a hit that zeroes publishAt) stamps its row from a later
rt.Now(). A row the tick then republishes is up to a tick's work younger
than the tick's clock: on r202, 151 shield frames said 5001-5020 ms and the
client dropped them as "Invalid shield state". The remaining time can never
exceed the lifetime the row was stamped with, so the writer bounds it here.

===========================================================================
*/
package action

/*
================
frameRemainingMs

Milliseconds from now to until, clamped to [0, lifetime]. Every port pet
frame that publishes a remaining time writes it through here.
================
*/
func frameRemainingMs(until, now, lifetime int64) uint32 {
	return uint32(min(lifetime, max(int64(0), until-now)))
}
