/*
===========================================================================

concealment.ts - native observer visibility for characters and skill objects

===========================================================================
*/

import type { AttachedEffect } from "./attached-effects";
import type { SkillMetadata, StatusLevel } from "./skill-catalog";

// CICharactor_UpdateStatesAndOwnedDecorations (85D890), the observer half:
// how the local player sees another character in body status 6 (stealth)
// or 7 (invisibility). The server only installs the effects this reads.
//
//   hide    the character's own hide buffs (+0x1F0), maximised per bit
//           (85CBD0, only while the body byte is 6 or 7)
//   sight   the local player's dtt buffs (+0x1E8, 85CC70)
//   reveal  dttp buffs laid on the character itself (+0x1EC, 85CE40)
//
// Per hidden bit the state starts at hidden and becomes seen when a sight
// or reveal level reaches the hide level within its range; the last hidden
// bit decides. Range is the efr radius that came with the winning level
// (0: any distance), measured against +0x244, the 3D distance the local
// player's state timer keeps (864998).
/*
================
ConcealmentState
================
*/
export type ConcealmentState = "open" | "seen" | "hidden";

// Renderer alpha per state: 0x50 seen, 0 hidden (and +0x740 unpickable),
// except that a hidden party member stays at 0x50.
/*
================
seenAlpha
================
*/
export function seenAlpha() {
	return 0x50 / 255;
}

/*
================
Detection
================
*/
interface Detection {
	readonly levels: number[];
	readonly ranges: number[];
}

/*
================
hideLevels
================
*/
function hideLevels(
	effects: readonly AttachedEffect[],
	skill: ( id: number ) => SkillMetadata | undefined
): number[] | null {
	let levels: number[] | null = null;
	for ( const effect of effects ) {
		const hide = skill( effect.skill )?.hide;
		if ( !hide?.mask ) continue;
		levels ??= Array<number>( 32 ).fill( 0 );
		for ( let bit = 0; bit < 32; bit++ ) {
			if ( hide.mask & 2 ** bit ) levels[bit] = Math.max( levels[bit]!, hide.level );
		}
	}
	return levels;
}

// 85CC70 / 85CE40: the level per bit, with the range of the source that
// raised it.
/*
================
detection
================
*/
function detection(
	effects: readonly AttachedEffect[],
	skill: ( id: number ) => SkillMetadata | undefined,
	field: ( row: SkillMetadata ) => StatusLevel | undefined
): Detection | null {
	let result: Detection | null = null;
	for ( const effect of effects ) {
		const row = skill( effect.skill ), level = row && field( row );
		if ( !row || !level?.mask ) continue;
		result ??= { levels: Array<number>( 32 ).fill( 0 ), ranges: Array<number>( 32 ).fill( 0 ) };
		for ( let bit = 0; bit < 32; bit++ ) {
			if ( level.mask & 2 ** bit && result.levels[bit]! < level.level ) {
				result.levels[bit] = level.level;
				result.ranges[bit] = row.detectRange ?? 0;
			}
		}
	}
	return result;
}

/*
================
covers
================
*/
function covers( source: Detection | null, bit: number, hide: number, distance: number ) {
	if ( !source || source.levels[bit]! < hide ) return false;
	const range = source.ranges[bit]!;
	return range === 0 || Math.fround( distance ) <= range;
}

/*
================
concealmentState
================
*/
export function concealmentState(
	body: number,
	own: readonly AttachedEffect[],
	viewer: readonly AttachedEffect[],
	distance: number,
	skill: ( id: number ) => SkillMetadata | undefined
): ConcealmentState {
	if ( body !== 6 && body !== 7 ) return "open";
	const hide = hideLevels( own, skill );
	if ( !hide ) return "open";
	const sight = detection( viewer, skill, row => row.sight ), reveal = detection( own, skill, row => row.detect );
	let state: ConcealmentState = "open";
	for ( let bit = 0; bit < 32; bit++ ) {
		if ( !hide[bit] ) continue;
		state = covers( sight, bit, hide[bit]!, distance ) || covers( reveal, bit, hide[bit]!, distance ) ?
			"seen" :
			"hidden";
	}
	return state;
}

// The renderer alpha 85D890 installs; undefined leaves it untouched.
/*
================
concealmentAlpha
================
*/
export function concealmentAlpha( state: ConcealmentState, partyMember: boolean ): number | undefined {
	if ( state === "open" ) return undefined;
	if ( state === "seen" || partyMember ) return seenAlpha();
	return 0;
}

/*
================
skillObjectVisible

86C1F0 compares the object's authored hide levels with the viewer's dtt
levels. Unlike character concealment, this branch has no distance or party
exception, and detection admits full visibility. The last populated bit
wins, matching AbnormalStatusMask_MergeSlotMaxValues traversal.
================
*/
export function skillObjectVisible(
	hide: StatusLevel | undefined,
	viewer: readonly AttachedEffect[],
	skill: ( id: number ) => SkillMetadata | undefined
): boolean {
	if ( !hide ) return true;
	const sight = detection( viewer, skill, row => row.sight );
	if ( !sight ) return false;
	let visible = true;
	for ( let bit = 0; bit < 32; bit++ ) {
		if ( hide.mask & 2 ** bit && hide.level ) visible = sight.levels[bit]! >= hide.level;
	}
	return visible;
}
