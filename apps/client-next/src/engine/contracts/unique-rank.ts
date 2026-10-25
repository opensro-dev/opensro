/*
===========================================================================

unique-rank.ts - the unique kill damage ranking a player receives

Port-only, not native, off by default (server SRO_UNIQUE_DAMAGE_RANK,
client Experimental row). The worker decodes the port-only 0x3FBC frame
into this shape (runtime/.../unique-rank/unique-rank.ts); the gameplay
snapshot carries the latest one and the HUD window draws it. It lives
apart from gameplay.ts because nothing else in the gameplay plane reads
it: one optional feature, one contract.

===========================================================================
*/

/*
================
UniqueDamageRow

One ranked player of a unique kill (port-only 0x3FBC; unique-rank.ts). A
party rank of 0 means the player was in no party.
================
*/
export interface UniqueDamageRow {
	readonly name: string;
	readonly damage: number;
	readonly partyRank: number;
}
/*
================
UniqueDamageRank

The latest unique kill's damage ranking as this player received it. The
sequence rises with every ranking, so the window opens once per kill.
================
*/
export interface UniqueDamageRank {
	readonly sequence: number;
	readonly refObjId: number;
	readonly uniqueName: string;
	readonly total: number;
	readonly rows: readonly UniqueDamageRow[];
	readonly selfRank: number;
	readonly selfDamage: number;
	readonly selfPartyRank: number;
}
