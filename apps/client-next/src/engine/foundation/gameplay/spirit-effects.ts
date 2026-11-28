/*
===========================================================================

spirit-effects.ts - the dark force a Devil's Spirit plays on its wearer

Port-only, not native to v1.150 (SRO_NASRUN). The later client's
itemeffect.txt names, for each Spirit, the skill effect record its
wearer plays on four transitions: worn (equip), taken off (unequip),
woken (awake) and its clock running out (expire). The records are the
VSRO client's ITEMSKILL_*_AVATAR_NASRUN_* rows (avatar_nasrun01_equip01,
unequip01 and awake01.efp on Bip01 Spine1, with their sounds); the build
publishes the table by codename (dress.spiritItemEffects).

Only a transition the client observes plays: the 0x3314 / 0x377C visuals
any viewer receives (equip, unequip), the owner's 0x31E8 clock frame
taking a dormant Spirit to a running clock (awake), and the owner's lent
skill strip emptying while the Spirit stays worn (expire). A spawn, a
re-entry or a redress carries none of them, so none replays an effect.
The server sends the wake and the lapse to the owner alone, so only the
wearer sees those two.

===========================================================================
*/

export type SpiritTransition = "equip" | "unequip" | "awake" | "expire";

/*
================
spiritEffectRecord

The named effect record the Spirit codename plays on transition, or
undefined when the table names none (Nasrun off, or an item that is no
Spirit).
================
*/
export function spiritEffectRecord(
	table: Readonly<Record<string, Readonly<Record<string, string>>>> | undefined,
	codename: string | undefined,
	transition: SpiritTransition
): string | undefined {
	if ( !table || !codename ) return undefined;
	return table[codename]?.[transition];
}

/*
================
spiritWoke

Whether a Spirit's clock frame wakes it: its data word goes from dormant
(0) to a running clock. An extension frame lengthens a clock already
running, so it is no wake.
================
*/
export function spiritWoke( before: number, after: number ): boolean {
	return before === 0 && after !== 0;
}

/*
================
spiritLapsed

Whether the owner's lent skill strip says the worn Spirit's clock ran
out: the strip empties while a Spirit is still worn. A Spirit taken off
empties the strip too, but its 0x377C visual and the move result have
already taken it out of the avatar socket by then.
================
*/
export function spiritLapsed( before: readonly number[], after: readonly number[], worn: boolean ): boolean {
	return worn && before.length > 0 && after.length === 0;
}
