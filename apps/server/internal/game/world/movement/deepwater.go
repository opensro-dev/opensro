/*
===========================================================================

deepwater.go - the SRO_DEEP_WATER_REFUSAL switch

The deep-water destination gate (WaterValidator.ValidateMovement) is
port-only, not native: the original client sends seabed clicks and the
original server's move path has no water rule; the depth refusal came from
a reference port. The gate ships off; the shard setting turns it back on.

===========================================================================
*/
package movement

import (
	"os"
	"strings"
)

// EnvDeepWaterRefusal turns the deep-water destination refusal on ("on",
// "1", "true"). Port-only, not native: off accepts submerged destinations.
const EnvDeepWaterRefusal = "SRO_DEEP_WATER_REFUSAL"

/*
================
DeepWaterRefusalFromEnv
================
*/
func DeepWaterRefusalFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvDeepWaterRefusal))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
WaterValidator.SetDeepWaterRefusal

Turns the deep-water destination refusal on or off (EnvDeepWaterRefusal).
Set once at boot, before the first move.
================
*/
func (v *WaterValidator) SetDeepWaterRefusal(on bool) {
	v.deepWaterRefusal = on
}
