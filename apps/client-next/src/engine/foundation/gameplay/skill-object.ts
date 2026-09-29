/*
===========================================================================

skill-object.ts - strict native CISkillObj spawn decoding

777220 dispatches FFFFFFFF/0054 to CISkillObj. Its skill ID precedes the
ordinary object header; interpreting it as a catalog reference loses the row.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";

export const DYNAMIC_OBJECT_REFERENCE = 0xffffffff;
export const SKILL_OBJECT_TYPE = 0x54;
const SKILL_OBJECT_ROW_BYTES = 30;

/*
================
decodeSkillObject

86C440 reads the list body; 86C420 adds one appearance byte for a single
spawn. Reject malformed rows before they can enter the entity journal.
================
*/
export function decodeSkillObject( payload: Uint8Array, single: boolean ): EntityState {
	if ( payload.length !== SKILL_OBJECT_ROW_BYTES + (single ? 1 : 0) ) {
		throw Error( "Invalid skill object row length" );
	}
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	if ( view.getUint32( 0, true ) !== DYNAMIC_OBJECT_REFERENCE || view.getUint16( 4, true ) !== SKILL_OBJECT_TYPE ) {
		throw Error( "Invalid skill object discriminator" );
	}
	const skillId = view.getUint32( 6, true ), gid = view.getUint32( 10, true );
	const x = view.getFloat32( 16, true ), y = view.getFloat32( 20, true ), z = view.getFloat32( 24, true );
	if ( !skillId || !gid || !Number.isFinite( x ) || !Number.isFinite( y ) || !Number.isFinite( z ) ) {
		throw Error( "Invalid skill object identity or position" );
	}
	return {
		gid,
		refObjId: DYNAMIC_OBJECT_REFERENCE,
		kind: "skill-object",
		regionId: view.getUint16( 14, true ),
		x,
		y,
		z,
		heading: view.getUint16( 28, true ),
		name: "",
		skillObject: { skillId, ...(single ? { appear: view.getUint8( 30 ) } : {}) }
	};
}
