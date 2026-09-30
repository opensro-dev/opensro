/*
===========================================================================

equipmentGlowMetadata.mjs - native equipment glow catalog and texture binding

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { clientV150ResinfoRoot, retailTextdataRoot } from "../world/paths.mjs";
import { listTextDataShardNamesSync, readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";
import { readCharacterTexture } from "../shared/nativeCharacterTextures.mjs";

// 915490 consumes itemoption's group, then the ordered itemtypenumber vector.

/*
================
equipmentGlowCatalog

Join authored item groups and thresholds to their active reference item IDs.
================
*/
export function equipmentGlowCatalog() {
	const groups = new Map(), items = new Map(), codes = new Map();
	for (
		const line of fs.readFileSync( path.join( clientV150ResinfoRoot, "itemtypenumber.txt" ), "utf8" ).split(
			/\r?\n/
		)
	) {
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
	for (
		const line of fs.readFileSync( path.join( clientV150ResinfoRoot, "itemoption.txt" ), "utf8" ).split( /\r?\n/ )
	) {
		const c = line.trim().split( /\s+/ );
		if ( !c[0] || c[0].startsWith( "//" ) ) continue;
		if ( c.length !== 2 || !groups.has( Number( c[1] ) ) ) throw Error( "Invalid equipment glow group: " + line );
		codes.set( c[0], Number( c[1] ) );
	}
	for ( const file of listTextDataShardNamesSync( retailTextdataRoot, /^itemdata.*\.txt$/i ) ) {
		for ( const line of readTextDataLinesSync( path.join( retailTextdataRoot, file ) ) ) {
			const c = splitTextDataRow( line );
			if ( c[0] === "1" && codes.has( c[2] ) ) {
				items.set( Number( c[1] ), groups.get( codes.get( c[2] ) ) );
			}
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
