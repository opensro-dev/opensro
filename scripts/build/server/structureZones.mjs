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
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
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
portOnlyObjectStringFiles

The port-only placement files beside objectstring.ifo, in name order:
objectstring.portonly.ifo (scripts/constantinople_import.py) and one
objectstring.<fortress>.portonly.ifo per later fortress importer
(scripts/later_fortress_import.py). Each exists only while its flag is
on. A missing folder lists none.
================
*/
const PORT_ONLY_OBJECT_STRING = /^objectstring\.(?:[a-z0-9_]+\.)?portonly\.ifo$/;

export async function portOnlyObjectStringFiles( navmeshRoot ) {
	let names;
	try {
		names = await readdir( navmeshRoot );
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return [];
		throw error;
	}
	return names.filter( name => PORT_ONLY_OBJECT_STRING.test( name ) ).sort().map( name =>
		path.join( navmeshRoot, name )
	);
}

/*
================
buildStructureZoneProjection

Writes world-authority/structure-zones.json into the bundle. The
port-only placement files (portOnlyObjectStringFiles; one path or a
list) join when they exist: their zone names are the later fortresses',
disjoint from the native file's and from each other's by construction.
A name two files place at the same spot is kept once; a name they place
differently is a build error rather than a silent pick. Without them the
projection is exactly the native one.
================
*/
export async function buildStructureZoneProjection( bundleRoot, objectStringFile, portOnlyStringFiles ) {
	const zones = parseObjectStringIfo( await readFile( objectStringFile, "latin1" ) );
	const placed = new Map( zones.map( zone => [ zone.name, zone ] ) );
	for ( const file of [].concat( portOnlyStringFiles ?? [] ) ) {
		let extra = null;
		try {
			extra = await readFile( file, "latin1" );
		} catch ( error ) {
			if ( error.code !== "ENOENT" ) throw error;
		}
		if ( extra === null ) continue;
		for ( const zone of parseObjectStringIfo( extra ) ) {
			const existing = placed.get( zone.name );
			if ( existing ) {
				if ( JSON.stringify( existing ) !== JSON.stringify( zone ) ) {
					throw new Error(
						`objectstring port-only file ${path.basename( file )} re-places ${zone.name} elsewhere`
					);
				}
				continue;
			}
			placed.set( zone.name, zone );
			zones.push( zone );
		}
	}
	const target = path.join( bundleRoot, "world-authority", "structure-zones.json" );
	await mkdir( path.dirname( target ), { recursive: true } );
	await writeFile( target, `${JSON.stringify( { format: FORMAT, version: VERSION, zones }, null, 2 )}\n` );
	return zones.length;
}
