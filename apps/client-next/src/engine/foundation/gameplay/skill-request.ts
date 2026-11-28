/*
===========================================================================

skill-request.ts - the native 0x72CD skill press

The one encoder of a skill press (an optional object target, never client
damage): the gameplay worker's combat owner sends it for a player's press
and the crowd bench's peers send it for their scripted casts.

===========================================================================
*/

import { TARGET_INTERACT_OPCODE } from "@/engine/contracts/opcodes";

// [u8 1 action][u8 4 skill][u32 id][u8 target kind][u32 gid when kind 1]
const ACTION_EXECUTE = 1, ACTION_SKILL = 4, TARGET_NONE = 0, TARGET_OBJECT = 1;

/*
================
skillRequest
================
*/
export function skillRequest( id: number, gid?: number ) {
	if ( !Number.isInteger( id ) || id <= 0 || id > 0xffffffff ) {
		throw new Error( "Invalid skill ID" );
	}
	const p = new Uint8Array( gid ? 11 : 7 ), v = new DataView( p.buffer );
	p[0] = ACTION_EXECUTE;
	p[1] = ACTION_SKILL;
	v.setUint32( 2, id, true );
	p[6] = gid ? TARGET_OBJECT : TARGET_NONE;
	if ( gid ) {
		v.setUint32( 7, gid, true );
	}
	return { opcode: TARGET_INTERACT_OPCODE, payload: p };
}
