/*
===========================================================================

aiNavProjection.mjs - the dungeon route tables GameWorld's cave route reads

Data.pk2's navmesh folder carries one AINavData_<region>.DAT per dungeon
region: the native cave route's block, cell and portal tables and their
crossing points (#1698). The server parses and validates each file against
the dungeon's own nav meshes (apps/server/internal/game/world/movement,
ainavdata.go); this module only packs the bytes into the projection.

===========================================================================
*/
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const AINAV_FILE_PATTERN = /^AINavData_(\d+)\.DAT$/i;
const AINAV_PROJECTION = path.join( "world-authority", "movement", "dungeon", "ainav-data.json" );

/*
================
buildAINavProjection

Packs every AINavData_<region>.DAT the client's navmesh folder holds into
one JSON file beside the dungeon resources, the bytes in base64 as
dungeon-resources.json carries its DOF files. The server parses and
validates each file against the dungeon's own nav meshes; a folder without
any writes nothing, and the cave route keeps its fallback.
================
*/
export async function buildAINavProjection( bundleRoot, sourceRoot ) {
	let names;
	try {
		names = await readdir( sourceRoot );
	} catch ( error ) {
		if ( error.code === "ENOENT" ) {
			return;
		}
		throw error;
	}
	const files = [];
	for ( const name of names.sort() ) {
		const match = AINAV_FILE_PATTERN.exec( name );
		if ( match === null ) {
			continue;
		}
		const raw = await readFile( path.join( sourceRoot, name ) );
		files.push( {
			regionId: Number( match[1] ),
			sourcePath: `navmesh/${name}`,
			byteLength: raw.length,
			rawBase64: raw.toString( "base64" )
		} );
	}
	if ( files.length === 0 ) {
		return;
	}
	files.sort( ( left, right ) => left.regionId - right.regionId );
	const target = path.join( bundleRoot, AINAV_PROJECTION );
	await mkdir( path.dirname( target ), { recursive: true } );
	const document = { format: "sro-server-dungeon-ainav", version: 1, files };
	await writeFile(
		target,
		`${JSON.stringify( document, null, 2 )}
`
	);
}
