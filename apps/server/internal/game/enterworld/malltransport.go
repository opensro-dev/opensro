/*
===========================================================================

malltransport.go - Item Mall transports move half again as fast

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_MALL_TRANSPORT_SPEED on, the transports the Item Mall sells walk
and run 1.5 times their characterdata speeds. Only move speed changes. The
scale lands on the loaded reference rows, so every owner that reads a
COS's speed (summon, entry spawn, pet AI, formation, combat chase) and the
speeds published to clients agree. Off, the default here, is native; the
GameWorld's Nomad job turns it on.

The mall's transports are the families its MALL_PET_D_CART shop group
sells (refshopgoods): the buffalo, rhinoceros and white elephant scrolls,
each summoning a COS_T_<family>[_<level>] row.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"os"
	"strings"
)

// EnvMallTransportSpeed turns the faster mall transports on ("on", "1", "true").
const EnvMallTransportSpeed = "SRO_MALL_TRANSPORT_SPEED"

// mallTransportSpeedup multiplies a mall transport's walk and run speeds.
const mallTransportSpeedup = 1.5

// mallTransportFamilies are the COS_T_ codename families MALL_PET_D_CART sells.
var mallTransportFamilies = []string{"COS_T_BUFFALO", "COS_T_RHINOCEROS", "COS_T_WHITEELEPHANT"}

const (
	// tidKindMask keeps a characterdata TID word's TypeID1-3 bits (the
	// same mask cossummoning.go and cosrecord.go test inline).
	tidKindMask = 0x7fe
	// tidKindCOS is the masked TID word of a COS (TypeID1-3 1/2/3).
	tidKindCOS = 0x1c6
	// transportBand is a COS's TypeID4 band for a trade transport.
	transportBand = 2
)

/*
================
MallTransportSpeedFromEnv
================
*/
func MallTransportSpeedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMallTransportSpeed))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
isMallTransport
================
*/
func isMallTransport(ref *CharacterRef) bool {
	if ref == nil || ref.TidWord&tidKindMask != tidKindCOS || ref.TidWord>>11 != transportBand {
		return false
	}
	for _, family := range mallTransportFamilies {
		if ref.Codename == family || strings.HasPrefix(ref.Codename, family+"_") {
			return true
		}
	}
	return false
}

/*
================
ApplyMallTransportSpeed

Scales every mall transport reference's walk and run speeds, before any
owner reads them; the count is the rows changed.
================
*/
func (t *TextdataItems) ApplyMallTransportSpeed() (int, error) {
	t.once.Do(t.load)
	if t.archive != nil {
		return 0, fmt.Errorf("%s: references are already archived", EnvMallTransportSpeed)
	}
	scaled := 0
	for _, ref := range t.charactersByCodename {
		if isMallTransport(ref) {
			ref.WalkSpeed *= mallTransportSpeedup
			ref.RunSpeed *= mallTransportSpeedup
			scaled++
		}
	}
	// The summonable list holds copies of the same rows.
	for i := range t.summonableCharacters {
		if ref := &t.summonableCharacters[i]; isMallTransport(ref) {
			ref.WalkSpeed *= mallTransportSpeedup
			ref.RunSpeed *= mallTransportSpeedup
		}
	}
	return scaled, nil
}
