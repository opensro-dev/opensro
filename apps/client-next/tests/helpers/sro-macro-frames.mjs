/*
===========================================================================

sro-macro-frames.mjs - SRO Macro server frames for the macro tests

The 0x3555 replay and 0xB554 answer bytes the server's macro package
(apps/server/internal/game/macro) writes, so the client tests decode what
the server sends.

===========================================================================
*/

const OP_MACRO_ANSWER = 0xb554;
const OP_MACRO_CONFIG = 0x3555;
const MACRO_POTION = 0;
const MACRO_SKILL = 1;
const MACRO_HUNT = 2;
const MACRO_POTION_BYTES = 0x48;
const MACRO_HUNT_BYTES = 0x0c;
const POTION_VERSION = 3;
const SKILL_VERSION = 2;
const HUNT_VERSION = 2;
const AVAILABLE_ALL = 7;
const SECTION_COUNT = 3;
const SKILL_RECORD_BYTES = 5;

/*
================
u16
================
*/
function u16( value ) {
	return [ value & 0xff, value >>> 8 ];
}

/*
================
macroReplay

A 0x3555 full replay as the server writes it (ConfigPayload): every kind
available, unsaved potion and hunt sections as version 0, length 0.
================
*/
/**
 * @param {readonly { slot: number, skill: number }[]} list
 */
function skillList( list ) {
	if ( list.length === 0 ) return [ ...u16( 1 ), 0 ];
	const out = [ ...u16( list.length * SKILL_RECORD_BYTES ) ];
	for ( const { slot, skill } of list ) {
		out.push( slot, skill & 0xff, skill >>> 8 & 0xff, skill >>> 16 & 0xff, skill >>> 24 & 0xff );
	}
	return out;
}

/**
 * @param {{ potion?: number[], hunt?: number[], potionOn?: number, skillOn?: number, huntOn?: number,
 *   skillFlags?: number[],
 *   attack?: { slot: number, skill: number }[], buff?: { slot: number, skill: number }[] }} [sections]
 */
export function macroReplay(
	{
		potion,
		hunt,
		potionOn = 0,
		skillOn = 0,
		huntOn = 0,
		skillFlags = [ 1, 0, 0, 1 ],
		attack = [],
		buff = []
	} = {}
) {
	const flags = [ ...skillFlags ];
	while ( flags.length < 4 ) flags.push( 0 );
	const out = [ 0, AVAILABLE_ALL, SECTION_COUNT ];
	out.push( MACRO_POTION, potionOn );
	if ( potion ) out.push( ...u16( POTION_VERSION ), ...u16( MACRO_POTION_BYTES ), ...potion );
	else out.push( 0, 0, 0, 0 );
	out.push(
		MACRO_SKILL,
		skillOn,
		...u16( SKILL_VERSION ),
		flags[0],
		flags[1],
		...skillList( attack ),
		flags[2],
		flags[3],
		...skillList( buff )
	);
	out.push( MACRO_HUNT, huntOn );
	if ( hunt ) out.push( ...u16( HUNT_VERSION ), ...u16( MACRO_HUNT_BYTES ), ...hunt );
	else out.push( 0, 0, 0, 0 );
	return { opcode: OP_MACRO_CONFIG, payload: Uint8Array.from( out ) };
}

/*
================
macroAnswer
================
*/
/** @param {number[]} bytes */
export function macroAnswer( ...bytes ) {
	return { opcode: OP_MACRO_ANSWER, payload: Uint8Array.from( bytes ) };
}
