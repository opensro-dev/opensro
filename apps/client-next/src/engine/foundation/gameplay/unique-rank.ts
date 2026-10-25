/*
===========================================================================

unique-rank.ts - the unique damage ranking window's rows (port-only, not native)

Turns the 0x3FBC ranking (worker unique-rank.ts) into the rows the window
prints: place, name, damage, share of the unique's damage and party place.
The original has no such window; the server sends it only with
SRO_UNIQUE_DAMAGE_RANK on.

===========================================================================
*/
import type { UniqueDamageRank } from "@/engine/contracts/unique-rank";

// The ranked rows the server sends (wire.UniqueDamageRankMaxRows).
export const UNIQUE_RANK_ROWS = 12;

/*
================
UniqueRankLine

One printed row. self marks the viewer's own place.
================
*/
export interface UniqueRankLine {
	readonly rank: string;
	readonly name: string;
	readonly damage: string;
	readonly share: string;
	readonly party: string;
	readonly self: boolean;
}

/*
================
UniqueRankLines

The top rows, and the viewer's own row below them when the viewer placed
outside the top rows.
================
*/
export interface UniqueRankLines {
	readonly top: readonly UniqueRankLine[];
	readonly own: UniqueRankLine | null;
}

/*
================
groupedDamage

Whole numbers with comma thousands separators, independent of the
browser's locale so every player reads the same digits.
================
*/
export function groupedDamage( value: number ): string {
	return String( Math.max( 0, Math.trunc( value ) ) ).replace( /\B(?=(\d{3})+(?!\d))/g, "," );
}

/*
================
damageShare

A share of the total to one decimal place; nothing dealt reads 0.0%.
================
*/
export function damageShare( damage: number, total: number ): string {
	if ( total <= 0 ) return "0.0%";
	return `${(Math.round( (damage / total) * 1000 ) / 10).toFixed( 1 )}%`;
}

/*
================
partyPlace

A party's place among the unique's parties, or "-" without a party.
================
*/
export function partyPlace( rank: number ): string {
	return rank > 0 ? String( rank ) : "-";
}

/*
================
uniqueRankLines

selfName names the viewer's own row; the frame carries only its place.
================
*/
export function uniqueRankLines( ranking: UniqueDamageRank, selfName: string ): UniqueRankLines {
	const top = ranking.rows.slice( 0, UNIQUE_RANK_ROWS ).map( ( row, i ) => ({
		rank: String( i + 1 ),
		name: row.name,
		damage: groupedDamage( row.damage ),
		share: damageShare( row.damage, ranking.total ),
		party: partyPlace( row.partyRank ),
		self: i + 1 === ranking.selfRank
	}) );
	const own = ranking.selfRank > top.length ?
		{
			rank: String( ranking.selfRank ),
			name: selfName,
			damage: groupedDamage( ranking.selfDamage ),
			share: damageShare( ranking.selfDamage, ranking.total ),
			party: partyPlace( ranking.selfPartyRank ),
			self: true
		} :
		null;
	return { top, own };
}
