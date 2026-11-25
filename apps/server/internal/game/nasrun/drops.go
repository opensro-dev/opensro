/*
===========================================================================

drops.go - the Devil's Spirit lucky drops on a unique kill

Port-only, not native (#3456 step 3d), on with SRO_NASRUN like the
tokens they ride beside (tokens.go). The owner picked the odds
(2026-11-16, #3456): on a unique kill, each eligible contributor rolls
for a Devil's Spirit (0.25 % on an easy unique, 0.5 % on a hard one)
and for Sabakun's jewel (2 %) under normal loot rights - that is, the
same roster the token award pays (action/nasrun_tokens.go), with no
daily cap and no account of its own. Nothing is saved: the drop is an
item in the bag or it is not.

This file holds only the odds, as a tier over the token families.
Eligibility, the roll and the grant run in the kill settlement
(action/nasrun_drops.go).

===========================================================================
*/
package nasrun

// The owner's odds in basis points of ten thousand, so the rare ones
// stay whole numbers: one basis point is 0.01 %.
const (
	// SpiritDropEasyBps is the Spirit odds on an easy unique (0.25 %).
	SpiritDropEasyBps = 25
	// SpiritDropHardBps is the Spirit odds on a hard unique (0.5 %).
	SpiritDropHardBps = 50
	// JewelDropBps is Sabakun's jewel's odds on any of them (2 %).
	JewelDropBps = 200
	// DropBps in one whole chance, for the settlement's roll.
	DropBps = 10_000
)

/*
================
LuckyDropOdds

The Spirit and jewel odds (basis points) a kill of codename pays:
hard-family uniques roll the harder Spirit odds, the jewel the same
everywhere, and a monster outside the token table rolls nothing.
================
*/
func LuckyDropOdds(codename string) (spiritBps, jewelBps int, ok bool) {
	switch UniqueTokens(codename) {
	case TokensHard:
		return SpiritDropHardBps, JewelDropBps, true
	case TokensEasy:
		return SpiritDropEasyBps, JewelDropBps, true
	}
	return 0, 0, false
}
