/*
===========================================================================

hawk.go - the attacking hawk's strike notification

The v1.150 client registers 357A at 74DC91 (-> 775510 -> 8E2D30): the hawk
instance's effect token, the struck target and the packed damage word. The
v1.188 server writes the same three fields under 30D1 from 582750. It is
neither a cast nor an entity spawn: the client flies the hawk it already
draws for that effect instance.

===========================================================================
*/

package wire

const OpSummonedHawkStrike uint16 = 0x357a

// HawkFatalBit is the damage word's high bit: the strike killed its target
// (582750 ORs 0x8000 when the target's life state reads 2 or 3).
const HawkFatalBit uint16 = 0x8000

/*
================
HawkStrikeFrame
================
*/
func HawkStrikeFrame(instance, target uint32, damage uint16) Frame {
	return Frame{Opcode: OpSummonedHawkStrike, Payload: NewWriter(10).U32(instance).U32(target).U16(damage).Payload()}
}
