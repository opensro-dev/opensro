/*
===========================================================================

mostwanted.go - the killer's own Most Wanted state (port-only, not native)

#1849: while SRO_MOST_WANTED is on, the streak holder - and only the
streak holder - is told its wanted state, so the client can draw it as a
buff-board row: at world entry, on every counted kill that changes the
level or the progress toward it, when the job suit goes on over a saved
streak, and once more cleared when the streak ends. 0x3FB2 is a port-only
opcode: no v1.150 client or v1.188 server uses it, and only this port's
browser client consumes it. It carries display state only; the bonuses
stay the server's stat projection (action/mostwanted.go), so what the row
shows is what applies.

Body, little-endian:

	u8 level          (0 = no visible streak; the row is gone)
	u8 killsIntoLevel (counted kills toward the next level)
	u8 killsPerLevel  (counted kills one level takes)
	u8 job            (the dressed job: domain JobTrader/JobThief/JobHunter, 0 = unsuited)
	u8 damagePercent  (damage dealt and reduced at this level, 0 below 3)

===========================================================================
*/

package wire

// OpMostWantedState is the port-only S->C wanted state of the recipient.
const OpMostWantedState uint16 = 0x3FB2

/*
================
MostWantedState

One recipient's wanted state: its level, its progress toward the next,
the dressed job whose icon the row wears, and the level's damage percent.
================
*/
type MostWantedState struct {
	Level          uint8
	KillsIntoLevel uint8
	KillsPerLevel  uint8
	Job            uint8
	DamagePercent  uint8
}

/*
================
EncodeMostWantedState
================
*/
func EncodeMostWantedState(s MostWantedState) []byte {
	return []byte{s.Level, s.KillsIntoLevel, s.KillsPerLevel, s.Job, s.DamagePercent}
}
