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

/*
================
RecoverVital

SR_GameServer 4A86A0 reduces the signed recovery amount with parameter 8F
or 90, truncates through CRT_ftol, and discards negative recovery. The
4A87D0 setter then saturates at the keeper maximum. The caller owns the
life-state check and commits the returned value under its character lock.
================
*/
func RecoverVital(current, maximum, amount int64, reduction float32) int64 {
	recovered := (1 - float64(reduction)/100) * float64(int32(amount))
	if math.IsNaN(recovered) || recovered >= float64(math.MaxInt32)+1 || recovered < float64(math.MinInt32) {
		return current
	}
	recovery := max(0, int64(int32(recovered)))
	return min(maximum, current+recovery)
}
