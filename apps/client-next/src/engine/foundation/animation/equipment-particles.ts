/*
===========================================================================

equipment-particles.ts - validation of the published per-item particle tables

The dress catalog carries effect programs keyed by item id: the native rare
glows, the port-only enchant sparks and the port-only worn auras of the
later clients' mall avatars. Each table is checked once when the catalog
loads, so the appearance assembly can trust its rows.

===========================================================================
*/
import type { ModelParticle } from "./model-particles";

const MAX_ITEM_PARTICLES = 128;
const MAX_THRESHOLD = 255;

/*
================
validateEquipmentParticles

Rows on an item's own bones (a weapon or shield branch): a named bone,
never the character's origin. avatar admits the worn auras instead, whose
bones are the wearer's, which may sit at the origin ("none" in the later
client's table) and which may leave the scale to the wearer.
================
*/
export function validateEquipmentParticles(
	value: Record<string, readonly ModelParticle[]> | undefined,
	avatar = false
) {
	if ( value === undefined ) return;
	for ( const [id, rows] of Object.entries( value ) ) {
		if ( !/^\d+$/.test( id ) || !Array.isArray( rows ) || rows.length > MAX_ITEM_PARTICLES ) {
			throw Error( "Invalid equipment particle catalog" );
		}
		for ( const row of rows ) {
			if ( !row || typeof row.effectPath !== "string" ) throw Error( "Invalid equipment particle row" );
			const path: string = row.effectPath;
			const anchored = avatar ?
				typeof row.root === "boolean" && typeof row.bone === "string" && (row.root || !!row.bone) :
				row.root === false && typeof row.bone === "string" && !!row.bone;
			// An aura without an authored scale follows the wearer's.
			const scaled = avatar && row.scale === undefined ||
				typeof row.scale === "number" && Number.isFinite( row.scale ) && row.scale > 0;
			if (
				!path.endsWith( ".efp" ) || path.includes( ".." ) || path.startsWith( "/" ) || path.includes( ":" ) ||
				!anchored || !scaled ||
				!Array.isArray( row.offset ) || row.offset.length !== 3 ||
				![ ...row.offset ].every( Number.isFinite ) ||
				(row.threshold !== undefined &&
					(!Number.isInteger( row.threshold ) || row.threshold < 1 || row.threshold > MAX_THRESHOLD))
			) throw Error( "Invalid equipment particle row" );
		}
	}
}
