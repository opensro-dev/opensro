/*
===========================================================================

emote.ts - which actors play an emote, and when their hands are emptied

An emote routes by the actor's runtime class (778190): a player plays any
action, a COS only the one its type word admits. While an emote or a stall
keeper's greeting plays, a player's hand attachments (weapon and shield)
are hidden, so the hands that wave do not wave a weapon.

===========================================================================
*/

// 582110: the COS type word's class bits and the family bands it admits.
const COS_TYPE_MASK = 0x7fe;
const COS_TYPE_WORD = 0x1c6;
const COS_BAND_SHIFT = 11;
const COS_BAND_A = 3;
const COS_BAND_B = 4;
// The one action a COS plays, and the emote it plays as.
const COS_EMOTE_ACTION = 1;
const COS_EMOTE = 0;
// Action 2's clip leaves the previous attachment flag unchanged (A5F2AB).
const ATTACHMENT_KEEPING_EMOTE = "emote2";

/*
================
emoteRoute

778190 dispatches by runtime class before examining the COS type word;
582110 accepts character/COS family 3, subtype 3 or 4 only. The emote the
actor plays, or null when it plays none.
================
*/
export function emoteRoute( player: boolean, cos: boolean, tidWord: number, action: number ): number | null {
	if ( player ) return action;
	const band = (tidWord & 0xffff) >>> COS_BAND_SHIFT;
	const admitted = cos && action === COS_EMOTE_ACTION && (tidWord & COS_TYPE_MASK) === COS_TYPE_WORD &&
		(band === COS_BAND_A || band === COS_BAND_B);
	return admitted ? COS_EMOTE : null;
}

/*
================
emoteAttachments

8E6310 -> 85EA20 -> A94CF0 writes branch+98=0 for the hand attachments, and
A5F2AB skips those branches. Action 2 leaves the previous flag unchanged.
8E6470 restores them on exit only when GetRideObj is null.
================
*/
export function emoteAttachments(
	hidden: boolean,
	previous: string | undefined,
	current: string | undefined,
	player: boolean,
	mounted: boolean
): boolean {
	if ( !player ) return false;
	if ( current !== previous ) {
		if ( previous && !mounted ) hidden = false;
		if ( current && current !== ATTACHMENT_KEEPING_EMOTE ) hidden = true;
	}
	return !current && !mounted ? false : hidden;
}

/*
================
stallAttachments

A living player keeping a stall plays the stall motion (80, installed by
8E6B50): a greeting whose hands wave and offer, so its hand attachments are
hidden while the stall stands and return when it closes (#3443).

INFERENCE: 8E6B50 is taken to empty the hands the way the emote entry
8E6310 does (85EA20 -> A94CF0), since motion 80 is drawn for empty hands;
the owner reports the original shows no weapon at a stall.
================
*/
export function stallAttachments( player: boolean, keepingStall: boolean, dead: boolean ): boolean {
	return player && keepingStall && !dead;
}
