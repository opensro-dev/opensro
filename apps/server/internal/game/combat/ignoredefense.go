/*
===========================================================================

ignoredefense.go - MATTR_AVATAR_MDIA ("Ignore Monster Defense") chance

The reference server's hit-outcome routine reads parameter 0xBC only for a
player attacker, and only against a non-player target (monster or COS).

===========================================================================
*/

package combat

import "opensro.online/server/internal/game/paramkeeper"

// ignoreDefenseParameter is ParamKeeper parameter 0xBC (188), populated flat
// from equipped avatar MATTR_AVATAR_MDIA magic options and a buff's msid.
const ignoreDefenseParameter uint16 = 0xbc

/*
================
IgnoreDefenseChance

Returns attacker parameter 0xBC as a float when the attacker is a player
snapshot (has a parameter graph) and the defender is not (no graph), and 0
otherwise.
================
*/
func IgnoreDefenseChance(attacker, defender Stats) float64 {
	if attacker.graph == nil || defender.graph != nil {
		return 0
	}
	chance, ok := attacker.Param(ignoreDefenseParameter)
	if !ok {
		return 0
	}
	return float64(chance)
}

/*
================
IgnoreDefenseWrites

A buff's msid {percent}: the v1.188 server's buff installer adds it
flat to parameter 0xBC, the chance IgnoreDefenseChance reads. Port-only behind
SRO_NASRUN: no v1.150 row carries msid.
================
*/
func IgnoreDefenseWrites(percent uint32) []paramkeeper.Write {
	if percent == 0 {
		return nil
	}
	return []paramkeeper.Write{{Parameter: ignoreDefenseParameter, Channel: paramkeeper.Flat, Value: float32(percent)}}
}
