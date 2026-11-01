/*
===========================================================================

speed.go - shared grab and growth pet pacing for movement and spawn packets

===========================================================================
*/
package companion

import (
	"os"
	"strings"

	"opensro.online/server/internal/domain"
)

// EnvPetPacing is port-only, not native; unset retains authored movement.
const EnvPetPacing = "SRO_PET_PACING"

/*
================
Policies

The port-only pet rules, read once at startup and handed to the action
runtime. The zero value is native: authored speeds.
================
*/
type Policies struct {
	Pacing bool
}

/*
================
PoliciesFromEnv
================
*/
func PoliciesFromEnv() Policies {
	return Policies{Pacing: policyEnabled(EnvPetPacing)}
}

/*
================
policyEnabled
================
*/
func policyEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on":
		return true
	default:
		return false
	}
}

const petRunSpeedScale = 0.8

/*
================
RunSpeed

Port-only, not native: with Pacing, grab and growth pets move at four fifths
of their projected run speed. It scales a speed the pet is given to move
with, never the native parameter the follow and battle rules compare (the
server's follow-run install). Vehicle and captured quest actor speeds keep
their values.
================
*/
func (p Policies) RunSpeed(band uint16, speed float32) float32 {
	if p.Pacing && (band == domain.GrowthPetBand || band == domain.PickupPetBand) {
		return speed * petRunSpeedScale
	}
	return speed
}
