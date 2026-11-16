/*
===========================================================================

clock.go - the Devil's Spirit's awakening clock

Port-only, not native to v1.150 (#3456 step 2); the rule is the v1.188
server's. A Spirit's item data word (InventoryRow.Data, item record
+0x38) holds its awakening expiry in Unix seconds. A new Spirit holds 0:
it is dormant, and a dormant Spirit reads as expired, so it cannot be
worn until it is used. Using it wakes it for its own Param1 seconds (28
days on the mall Spirits, 7 on the event pair); using an awake or lapsed
Spirit again changes nothing. A worn Spirit whose clock runs out stays
worn but stops contributing. The extension gear lengthens the clock of a
Spirit that has been woken, from its expiry while it is still awake and
from now once it has lapsed.

The functions here are the rule alone, with the time injected; the item
use, the wear gate and the stats read them.

===========================================================================
*/
package nasrun

// maxClockSeconds is the last instant a u32 data word can hold; a clock
// that would pass it stops there instead of wrapping to a time long gone.
const maxClockSeconds uint64 = 1<<32 - 1

/*
================
Awake

Whether a Spirit with this data word is awake at nowUnix: it was woken
and its expiry is still ahead. A dormant Spirit (0) is not awake: the
reference server compares the stored time with now, and the zero time is
always behind it. The expiry instant itself is lapsed (inferred: the
server's comparison is a strict "later than now").
================
*/
func Awake(data uint32, nowUnix int64) bool {
	return data != 0 && int64(data) > nowUnix
}

/*
================
Wake

The data word a use leaves on a Spirit: a dormant one wakes for
periodSeconds from now; any other keeps its word, and woke reports
false. A period of zero or less wakes nothing (a row without Param1).
================
*/
func Wake(data uint32, periodSeconds int64, nowUnix int64) (next uint32, woke bool) {
	if data != 0 || periodSeconds <= 0 || nowUnix < 0 {
		return data, false
	}
	return clockAt(uint64(nowUnix) + uint64(periodSeconds)), true
}

/*
================
Extend

The data word the extension gear leaves: spanSeconds past the expiry
while the Spirit is awake, past now once it has lapsed. A dormant
Spirit refuses (the gear needs a stored time), as does a span of zero
or less.
================
*/
func Extend(data uint32, spanSeconds int64, nowUnix int64) (next uint32, extended bool) {
	if data == 0 || spanSeconds <= 0 || nowUnix < 0 {
		return data, false
	}
	base := uint64(nowUnix)
	if Awake(data, nowUnix) {
		base = uint64(data)
	}
	return clockAt(base + uint64(spanSeconds)), true
}

/*
================
clockAt
================
*/
func clockAt(seconds uint64) uint32 {
	if seconds > maxClockSeconds {
		return uint32(maxClockSeconds)
	}
	return uint32(seconds)
}
