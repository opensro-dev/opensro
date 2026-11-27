/*
===========================================================================

drops.go - the Devil's Spirit lucky drops on a unique kill

Port-only, not native (#3456 step 3d), on with SRO_NASRUN like the
tokens they ride beside (tokens.go). The owner picked the odds
(2026-11-16, #3456; raised 2026-11-27 in #general): on a unique kill,
each eligible contributor rolls for a Devil's Spirit (10 % on an easy
unique, 20 % on a hard one) and for Sabakun's jewel (90 %) under
normal loot rights - that is, the
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
	// SpiritDropEasyBps is the Spirit odds on an easy unique (10 %).
	SpiritDropEasyBps = 1_000
	// SpiritDropHardBps is the Spirit odds on a hard unique (20 %).
	SpiritDropHardBps = 2_000
	// JewelDropBps is Sabakun's jewel's odds on any of them (90 %):
	// +0 to +6 spends about 16 jewels on average (alchemy/nasrun.go's
	// 70..20 % ladder), so about 18 kills, the owner's target.
	JewelDropBps = 9_000
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
