/*
===========================================================================

equipmentVisualRecords.mjs - the worn-item records every equipment builder reads

One owner for what an itemdata row means to the dress: its resolved model,
its compound-model slot, whether it is an avatar, its armour class, mask
and priority. The equipment visuals, particles and booth roster all read
these records rather than re-deriving a row's TypeIDs.

===========================================================================
*/
import path from "node:path";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";

// TypeID3 of an avatar item.
const AVATAR_TYPE_ID3 = 13;

/*
================
visualSocket

Retail 868A80. This is the visual socket map, not inventory admission:
ammunition and jewelry remain equipped but have no compound-model slot.
================
*/
export function visualSocket( tid ) {
	const [a, b, c, d] = tid;
	if ( a !== 3 || b !== 1 ) return null;
	if ( [ 1, 2, 3, 9, 10, 11 ].includes( c ) ) return [ null, 0, 2, 1, 4, 3, 5 ][d] ?? null;
	if ( c === 6 ) return 6;
	if ( c === 4 && (d === 1 || d === 2) ) return 7;
	if ( c === 7 ) return 8;
	return null;
}

/*
================
isAvatarItem

The avatar items (TypeID 3.1.13.x): dress, hat, attachment and flag.
They are worn but have no compound-model slot (visualSocket is null).
================
*/
export function isAvatarItem( tid ) {
	return tid[0] === 3 && tid[1] === 1 && tid[2] === AVATAR_TYPE_ID3;
}

/*
================
loadEquipmentRecords

Every enabled itemdata row of the tree at root, by reference id.
================
*/
export function loadEquipmentRecords( root ) {
	const rows = new Map();
	for ( const file of listTextDataShardNamesSync( root, /^itemdata.*\.txt$/i ) ) {
		for ( const c of readTextDataRowsSync( path.join( root, file ) ) ) {
			if ( c[0] !== "1" ) {
				continue;
			}
			const id = Number( c[1] );
			if ( !Number.isSafeInteger( id ) || id <= 0 ) throw Error( "Invalid item reference" );
			const raw = c[52]?.trim(),
				model = !raw || raw.toLowerCase() === "xxx" ? null : "res/" + raw.replaceAll( "\\", "/" ).toLowerCase();
			if ( model && !model.endsWith( ".bsr" ) ) throw Error( `Invalid model ${id}: ${model}` );
			rows.set( id, {
				id,
				code: c[2],
				linkedCode: c[4],
				tid: c.slice( 9, 13 ).map( Number ),
				country: Number( c[14] ),
				specialState: Number( c[15] ),
				sex: Number( c[58] ),
				visualMaskOverride: Number( c[130] ),
				model
			} );
		}
	}
	const byCode = new Map( [ ...rows.values() ].map( r => [ r.code, r ] ) );
	for ( const row of rows.values() ) {
		// 7F0270 resolves column4 into common+5C. 8E8880/8EAA90 try ONE
		// linked record when common+11C is empty; this is not recursive inheritance.
		const linked = byCode.get( row.linkedCode );
		row.resolvedModel = row.model ?? linked?.model ?? null;
		row.modelSource = row.model ? row.id : linked?.model ? linked.id : null;
		row.slot = visualSocket( row.tid );
		row.avatar = isAvatarItem( row.tid );
		row.armorClass = row.tid[0] === 3 && row.tid[1] === 1 ?
			({ 1: 1, 9: 1, 2: 2, 10: 2, 3: 3, 11: 3 }[row.tid[2]] ?? 0) :
			0;
		row.thiefSuit = row.tid.join( "." ) === "3.1.7.2";
		row.visualMask = row.visualMaskOverride < 0 ?
			([ 1, 2, 3, 9, 10, 11 ].includes( row.tid[2] ) && row.slot !== null ? 1 << row.slot : 0) :
			row.visualMaskOverride;
		row.visualPriority = row.avatar ?
			(row.tid[3] === 3 ? 90 : row.tid[3] === 4 ? 110 : 70) :
			row.slot === 8 ?
			50 :
			150;
	}
	return rows;
}
