/*
===========================================================================

monster-scale.ts - the model scale and material slot a spawn record picks

Monsters and companions size their model from the record's scale percent
(characterdata column 48, record +0x108); a monster's grade can enlarge it
further. Pure: the presentation applies the result to the actor.

===========================================================================
*/

// The grade factors CICMonster_DeserializeSpawnPacket (861B00) applies.
const CHAMPION_FACTOR = 1.5;
const GIANT_FACTOR = 3;
const PARTY_FACTOR = 1.7;
const RARITY_CHAMPION = 1;
const RARITY_GIANT = 4;
const RARITY_PARTY = 6;
// The material slot an ordinary champion takes.
const CHAMPION_MATERIAL_SLOT = 2;
// A record percent of 100 keeps the default 1.0 scale.
const DEFAULT_SCALE_PERCENT = 100;

/*
================
playerLookalike

861B00: player-lookalike thief/hunter records bypass rarity enlargement and
keep their authored material slot.
================
*/
function playerLookalike( tidWord: number ): boolean {
	return (tidWord & 0x7fe) === 0xc6 && [ 2, 3 ].includes( (tidWord >>> 11) & 31 );
}

/*
================
monsterScale

861B00: the grade factor times the record percent. Other grades (including
unique and titan) have no extra size multiplier.
================
*/
export function monsterScale( rarity: number, tidWord: number, percent = DEFAULT_SCALE_PERCENT ): number {
	let factor = 1;
	if ( !playerLookalike( tidWord ) ) {
		if ( rarity === RARITY_CHAMPION ) factor = CHAMPION_FACTOR;
		else if ( rarity === RARITY_GIANT ) factor = GIANT_FACTOR;
		else if ( rarity === RARITY_PARTY ) factor = Math.fround( PARTY_FACTOR );
	}
	return Math.fround( factor * percent / 100 );
}

/*
================
cosScale

CICCos_DeserializeSpawnPacket (8554E0, 855743): a companion's model scale is
its record percent over 100, with no grade factor. A percent of 100 keeps
the default 1.0.
================
*/
export function cosScale( percent = DEFAULT_SCALE_PERCENT ): number {
	return percent === DEFAULT_SCALE_PERCENT ? 1 : Math.fround( percent / 100 );
}

/*
================
monsterMaterialSlot

The same spawn branch selects BMT slot 2 for ordinary champions. Other
record kinds retain their authored slot; unavailable slots fall back at
admission.
================
*/
export function monsterMaterialSlot( rarity: number, tidWord: number, kind = 0 ): number {
	if ( playerLookalike( tidWord ) ) return 0;
	const slot = kind === 1 || kind === 3 || kind === 4 ? kind : 0;
	return rarity === RARITY_CHAMPION && slot <= 1 ? CHAMPION_MATERIAL_SLOT : slot;
}
