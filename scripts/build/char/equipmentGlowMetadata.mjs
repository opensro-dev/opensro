/*
===========================================================================

equipmentGlowMetadata.mjs - native equipment glow catalog and texture binding

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { clientV150ResinfoRoot, extractedRoot, retailTextdataRoot } from "../world/paths.mjs";
import { listTextDataShardNamesSync, readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";
import { readCharacterTexture } from "../shared/nativeCharacterTextures.mjs";

// 915490 consumes itemoption's group, then the ordered itemtypenumber vector.

// Port-only, not native: the later client's tables (scripts/import_vsro_item_glow.py)
// feed the Experimental "Enhanced item glow" row; absent, only native rows publish.
export const vsroItemGlowRoot = path.join( extractedRoot, "VsroItemGlow_extracted", "resinfo" );

/*
================
glowTierGroups

itemtypenumber.txt's rows by group: threshold, UV rate, option texture,
stage-0 gain (4 MODULATE, 5 MODULATE2X), colours, period, alpha test.
================
*/
function glowTierGroups( file ) {
	const groups = new Map();
	for ( const line of fs.readFileSync( file, "latin1" ).split( /\r?\n/ ) ) {
		if ( !line.trim() || line.trim().startsWith( "//" ) ) continue;
		const c = line.trim().split( /\s+/ ), group = Number( c[0] );
		if ( c.length !== 10 || ![ 4, 5 ].includes( Number( c[4] ) ) || Number( c[5] ) !== 7 ) {
			throw Error( "Unimplemented equipment glow texture operation: " + line );
		}
		const row = {
			threshold: Number( c[1] ),
			uv: c[2].split( "," ).map( Number ),
			texture: c[3].replaceAll( "\\", "/" ).toLowerCase(),
			gain: Number( c[4] ) === 5 ? 2 : 1,
			color1: c[6].split( "," ).map( Number ),
			color2: c[7].split( "," ).map( Number ),
			period: Number( c[8] ),
			alphaTest: Number( c[9] ) !== 0
		};
		if ( !groups.has( group ) ) groups.set( group, [] );
		groups.get( group ).push( row );
	}
	return groups;
}

/*
================
glowItemGroups

itemoption.txt: item codename to its itemtypenumber group.
================
*/
function glowItemGroups( file, groups ) {
	const codes = new Map();
	for ( const line of fs.readFileSync( file, "latin1" ).split( /\r?\n/ ) ) {
		const c = line.trim().split( /\s+/ );
		if ( !c[0] || c[0].startsWith( "//" ) ) continue;
		if ( c.length !== 2 || !groups.has( Number( c[1] ) ) ) throw Error( "Invalid equipment glow group: " + line );
		codes.set( c[0], Number( c[1] ) );
	}
	return codes;
}

/*
================
equipmentGlowCatalog

Join authored item groups and thresholds to their active reference item IDs.
Native rows come from the v1.150 tables; when the later client's tables are
imported, an item also carries their rows marked enhanced (port-only). The
three roots default to the real tables; tests pass fixture roots.
================
*/
export function equipmentGlowCatalog(
	vsroRoot = vsroItemGlowRoot,
	nativeResinfoRoot = clientV150ResinfoRoot,
	textdataRoot = retailTextdataRoot
) {
	const nativeGroups = glowTierGroups( path.join( nativeResinfoRoot, "itemtypenumber.txt" ) );
	const nativeCodes = glowItemGroups( path.join( nativeResinfoRoot, "itemoption.txt" ), nativeGroups );
	let enhancedGroups = new Map(), enhancedCodes = new Map();
	if ( fs.existsSync( path.join( vsroRoot, "itemtypenumber.txt" ) ) ) {
		enhancedGroups = glowTierGroups( path.join( vsroRoot, "itemtypenumber.txt" ) );
		enhancedCodes = glowItemGroups( path.join( vsroRoot, "itemoption.txt" ), enhancedGroups );
	}
	const items = new Map();
	for ( const file of listTextDataShardNamesSync( textdataRoot, /^itemdata.*\.txt$/i ) ) {
		for ( const line of readTextDataLinesSync( path.join( textdataRoot, file ) ) ) {
			const c = splitTextDataRow( line );
			if ( c[0] !== "1" ) continue;
			const native = nativeCodes.has( c[2] ) ? nativeGroups.get( nativeCodes.get( c[2] ) ) : [];
			const enhanced = enhancedCodes.has( c[2] ) ? enhancedGroups.get( enhancedCodes.get( c[2] ) ) : [];
			if ( !native.length && !enhanced.length ) continue;
			items.set( Number( c[1] ), [
				...native.map( row => ({ ...row, enhanced: false }) ),
				...enhanced.map( row => ({ ...row, enhanced: true }) )
			] );
		}
	}
	return items;
}

/*
================
embedEquipmentGlows

Deduplicate glow images and publish the same native texture contract as base materials.
================
*/
export function embedEquipmentGlows( json, ids, catalog, appendView ) {
	const rows = {};
	json.images ??= [];
	for ( const id of ids ) {
		const source = catalog.get( Number( id ) );
		if ( !source ) continue;
		rows[id] = source.map( ( { texture, ...row } ) => {
			const name = "sro-equipment:" + texture;
			let image = json.images.findIndex( i => i.name === name );
			if ( image < 0 ) {
				const resource = readCharacterTexture( texture );
				image = json.images.length;
				json.images.push( { name, bufferView: appendView( resource.bytes ), mimeType: resource.mime } );
			}
			return { ...row, image };
		} );
	}
	json.extras ??= {};
	json.extras.sroEquipmentGlows = rows;
}

/*
================
equipmentGlowModelIds

Collect every equipment ID sharing a published weapon or shield model.
================
*/

export function equipmentGlowModelIds( equipment, catalog ) {
	const models = new Map();
	for ( const [id, row] of Object.entries( equipment ) ) {
		if ( (row.slot === 6 || row.slot === 7) && catalog.has( Number( id ) ) ) {
			for ( const body of Object.values( row.bodies ) ) {
				if ( !body ) {
					continue;
				}
				if ( !models.has( body.glb ) ) models.set( body.glb, new Set() );
				models.get( body.glb ).add( id );
			}
		}
	}
	return models;
}

/*
================
equipmentGlowGlb

Append glow metadata and aligned texture bytes while retaining existing model chunks.
================
*/
export function equipmentGlowGlb( bytes, ids, catalog ) {
	const length = bytes.readUInt32LE( 12 ), json = JSON.parse( bytes.subarray( 20, 20 + length ) );
	const parts = [ bytes.subarray( 28 + length ) ];
	let size = parts[0].length;
	embedEquipmentGlows( json, ids, catalog, data => {
		const pad = (4 - size % 4) % 4;
		if ( pad ) {
			parts.push( Buffer.alloc( pad ) );
			size += pad;
		}
		const index = json.bufferViews.length;
		json.bufferViews.push( { buffer: 0, byteOffset: size, byteLength: data.length } );
		parts.push( data );
		size += data.length;
		return index;
	} );
	json.buffers[0].byteLength = size;
	const text = Buffer.from( JSON.stringify( json ) ), chunk = Buffer.alloc( (text.length + 3) & ~3, 32 );
	text.copy( chunk );
	const binary = Buffer.concat( parts ), tail = Buffer.alloc( 8 + ((binary.length + 3) & ~3) );
	tail.writeUInt32LE( tail.length - 8 );
	tail.writeUInt32LE( 0x004e4942, 4 );
	binary.copy( tail, 8 );
	const header = Buffer.from( bytes.subarray( 0, 20 ) );
	header.writeUInt32LE( 20 + chunk.length + tail.length, 8 );
	header.writeUInt32LE( chunk.length, 12 );
	return Buffer.concat( [ header, chunk, tail ] );
}
