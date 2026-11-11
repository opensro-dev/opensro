/*
===========================================================================

worldLayers.mjs - which placements belong to a seasonal world layer

The later client's seasonal decoration (Halloween towns and the other
event layers) is imported into the game root's own .o2 files by
scripts/world_layer_import.py, which also writes Map_extracted/
layerobject.json: the layers it imported and the (region, uid, layer)
of every member placement. A game root without the file has no layers.

The region bundle tags each member placement with its layer so the
client draws it only while that layer's event group is active.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";

export const WORLD_LAYER_TABLE = "layerobject.json";
export const WORLD_LAYER_FORMAT = "opensro-world-layers-v1";
const UID_MASK = 0xffff;

const tables = new Map();

/*
================
parseWorldLayerTable

{ layers, members } from the table's JSON text: members maps
"region:uid" (uid as its 16 bits) to the layer id.
================
*/
export function parseWorldLayerTable( text, sourcePath = WORLD_LAYER_TABLE ) {
	const table = JSON.parse( text );
	if (
		table?.format !== WORLD_LAYER_FORMAT || !Array.isArray( table.layers ) || !Array.isArray( table.placements )
	) {
		throw new Error( `${sourcePath}: not a ${WORLD_LAYER_FORMAT} table` );
	}
	const layers = new Set( table.layers.map( layer => layer.id ) );
	const members = new Map();
	for ( const row of table.placements ) {
		const [region, uid, layer] = row;
		if ( !Number.isInteger( region ) || !Number.isInteger( uid ) || !layers.has( layer ) ) {
			throw new Error( `${sourcePath}: invalid placement row ${JSON.stringify( row )}` );
		}
		members.set( `${region}:${uid & UID_MASK}`, layer );
	}
	return { layers: table.layers, members };
}

/*
================
readWorldLayerTable

The game root's table, read once per extracted root; an absent file is
an empty table.
================
*/
export function readWorldLayerTable( sourceExtractedRoot ) {
	let table = tables.get( sourceExtractedRoot );
	if ( !table ) {
		const tablePath = path.join( sourceExtractedRoot, "Map_extracted", WORLD_LAYER_TABLE );
		table = readFile( tablePath, "utf8" ).then(
			text => parseWorldLayerTable( text, tablePath ),
			error => {
				if ( error.code === "ENOENT" ) return { layers: [], members: new Map() };
				throw error;
			}
		);
		tables.set( sourceExtractedRoot, table );
	}
	return table;
}

/*
================
placementLayer

The layer a parsed .o2 placement belongs to, or undefined. The parser
gives regionId as hex text and uid signed.
================
*/
export function placementLayer( table, placement ) {
	return table.members.get( `${Number( placement.regionId )}:${placement.uid & UID_MASK}` );
}
