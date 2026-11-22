/*
===========================================================================

recovery.go - native recovery reduction and gauge saturation.

Combat projects one gauge without owning a live character. Skill healing and
level transitions share this calculation so Panic and Combustion cannot be
bypassed by choosing a different recovery source.

===========================================================================
*/

package combat

import "math"

const (
	HPRecoveryReductionParameter uint16 = 0x8f
	MPRecoveryReductionParameter uint16 = 0x90
)

/*
================
RecoverVital

The server's vital recovery routine reduces the signed recovery amount with parameter 8F
or 90, truncates through CRT_ftol, and discards negative recovery. The
vital setter then saturates at the keeper maximum. The caller owns the
life-state check and commits the returned value under its character lock.
================
*/
func RecoverVital(current, maximum, amount int64, reduction float32) int64 {
	// A living object always has a positive maximum natively; a zero
	// one here means the caller could not resolve it (a companion whose
	// catalogue row is missing or carries MaxHP 0). Saturating to it
	// would turn a heal into a kill (#4081: a pet's 4 s pulse wrote
	// CurrentHP 0 and froze it, summoned, for good), so recovery keeps
	// the vital it was given. Inferred: neither binary has an unknown
	// maximum to show the rule; recovery never lowers a vital otherwise.
	if maximum <= 0 {
		return current
	}
	recovered := (1 - float64(reduction)/100) * float64(int32(amount))
	if math.IsNaN(recovered) || recovered >= float64(math.MaxInt32)+1 || recovered < float64(math.MinInt32) {
		return current
	}
	recovery := max(0, int64(int32(recovered)))
	return min(maximum, current+recovery)
}
