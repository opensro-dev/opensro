/*
===========================================================================

structureZones.mjs - the event-zone placements GameWorld spawns structures on

SR_GameServer's CObjectStringIfo_Load (98C7F0) reads navmesh\objectstring.ifo
from Data.pk2: a JMXVOBJI1000 header, a count, then one line per placed
event zone, `%x %x %d %d %x %x %x %x "name"`: the object id, a flag word,
the region's X and Z, then x, y, z and yaw as raw float32 bits. The names
are eventzonedata.txt's zone codenames (STRUCTURE_POS_JA_GATE_01, ...), so
the server joins the two to place each fortress structure.

===========================================================================
*/
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const HEADER = "JMXVOBJI1000";
const FORMAT = "sro-server-structure-zones";
const VERSION = 1;
const LINE =
	/^0x([0-9a-f]{1,8}) 0x([0-9a-f]{1,8}) (-?\d+) (-?\d+) 0x([0-9a-f]{1,8}) 0x([0-9a-f]{1,8}) 0x([0-9a-f]{1,8}) 0x([0-9a-f]{1,8}) "([^"]+)"$/i;

/*
================
floatBits

One %x float32 word as the number it encodes.
================
*/
function floatBits( hex ) {
	const view = new DataView( new ArrayBuffer( 4 ) );
	view.setUint32( 0, Number.parseInt( hex, 16 ) >>> 0 );
	const value = view.getFloat32( 0 );
	if ( !Number.isFinite( value ) ) throw new Error( `objectstring.ifo float 0x${hex} is not finite` );
	return value;
}

/*
================
parseObjectStringIfo

Every declared row, or an error naming the first malformed one. The region
id packs Z over X as the wire does (regionZ << 8 | regionX).
================
*/
export function parseObjectStringIfo( text ) {
	const lines = text.split( /\r?\n/ ).filter( line => line.trim() !== "" );
	if ( lines[0] !== HEADER ) throw new Error( `objectstring.ifo header ${JSON.stringify( lines[0] )}` );
	const count = Number( lines[1] );
	if ( !Number.isSafeInteger( count ) || count < 0 || count !== lines.length - 2 ) {
		throw new Error( `objectstring.ifo declares ${lines[1]} rows, holds ${lines.length - 2}` );
	}
	return lines.slice( 2 ).map( ( line, index ) => {
		const m = LINE.exec( line.trim() );
		if ( !m ) throw new Error( `objectstring.ifo row ${index + 1} is malformed: ${line}` );
		const regionX = Number( m[3] ), regionZ = Number( m[4] );
		if ( regionX < 0 || regionX > 255 || regionZ < 0 || regionZ > 255 ) {
			throw new Error( `objectstring.ifo row ${index + 1} region ${regionX},${regionZ}` );
		}
		return {
			name: m[9],
			objectId: Number.parseInt( m[1], 16 ) >>> 0,
			flags: Number.parseInt( m[2], 16 ) >>> 0,
			regionId: (regionZ << 8) | regionX,
			x: floatBits( m[5] ),
			y: floatBits( m[6] ),
			z: floatBits( m[7] ),
			yaw: floatBits( m[8] )
		};
	} );
}

/*
================
buildStructureZoneProjection

Writes world-authority/structure-zones.json into the bundle.
================
*/
export async function buildStructureZoneProjection( bundleRoot, objectStringFile ) {
	const zones = parseObjectStringIfo( await readFile( objectStringFile, "latin1" ) );
	const target = path.join( bundleRoot, "world-authority", "structure-zones.json" );
	await mkdir( path.dirname( target ), { recursive: true } );
	await writeFile( target, `${JSON.stringify( { format: FORMAT, version: VERSION, zones }, null, 2 )}\n` );
	return zones.length;
}
