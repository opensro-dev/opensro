/*
===========================================================================

worldLayers.test.mjs - placements answer to their seasonal layer

The layer table world_layer_import.py writes is read once per game root,
an absent table is no layers, and a placement's signed uid finds the
row its 16 bits name. The resource closure follows a compound's branches.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
	parseWorldLayerTable,
	placementLayer,
	readWorldLayerTable,
	WORLD_LAYER_FORMAT,
	WORLD_LAYER_TABLE
} from "../../build/world/maploader/worldLayers.mjs";
import { CPD_SIGNATURE } from "../../build/world/objects/compound.mjs";

const JANGAN = 0x61a8;

/*
================
table
================
*/
function table( placements ) {
	return JSON.stringify( {
		format: WORLD_LAYER_FORMAT,
		layers: [ { id: 5, name: "Halloween (DECO)", kind: 1, nativeEnabled: false } ],
		placements
	} );
}

test( "a placement finds its layer by region and the uid's 16 bits", () => {
	const layers = parseWorldLayerTable( table( [ [ JANGAN, 7, 5 ], [ JANGAN, 0xfffe, 5 ] ] ) );
	assert.equal( placementLayer( layers, { regionId: "0x61a8", uid: 7 } ), 5 );
	assert.equal( placementLayer( layers, { regionId: "0x61a8", uid: -2 } ), 5, "a negative uid is its 16 bits" );
	assert.equal( placementLayer( layers, { regionId: "0x61a8", uid: 8 } ), undefined );
	assert.equal( placementLayer( layers, { regionId: "0x61a9", uid: 7 } ), undefined );
} );

test( "a row naming a layer the table lacks is refused", () => {
	assert.throws( () => parseWorldLayerTable( table( [ [ JANGAN, 7, 9 ] ] ) ), /invalid placement row/ );
	assert.throws( () => parseWorldLayerTable( "{\"format\":\"other\"}" ), /not a/ );
} );

test( "a game root without the table has no layers", async () => {
	const root = await mkdtemp( path.join( tmpdir(), "world-layers-" ) );
	try {
		const empty = await readWorldLayerTable( root );
		assert.equal( empty.members.size, 0 );
		const other = path.join( root, "with" );
		await mkdir( path.join( other, "Map_extracted" ), { recursive: true } );
		await writeFile( path.join( other, "Map_extracted", WORLD_LAYER_TABLE ), table( [ [ JANGAN, 7, 5 ] ] ) );
		assert.equal( (await readWorldLayerTable( other )).members.size, 1 );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
} );

test( "the resource closure follows a compound's branches", () => {
	const name = "x_mas_boxset01";
	const branches = [ "res/etc/x_mas/x_mas_boxset01_01.bsr", "res/etc/x_mas/x_mas_boxset01_02.bsr" ];
	const text = value => Buffer.concat( [ uint32( value.length ), Buffer.from( value, "ascii" ) ] );
	const uint32 = value => {
		const buffer = Buffer.alloc( 4 );
		buffer.writeUInt32LE( value );
		return buffer;
	};
	// JMXVCPD 0101, name and branch offsets (compound.mjs), then the name
	// and the counted branch paths.
	const header = Buffer.alloc( 40 );
	header.write( CPD_SIGNATURE, 0, "ascii" );
	const nameBlock = text( name );
	header.writeUInt32LE( 40, 12 );
	header.writeUInt32LE( 40 + nameBlock.length, 16 );
	const cpd = Buffer.concat( [ header, nameBlock, uint32( branches.length ), ...branches.map( text ) ] );
	const script = fileURLToPath( new URL( "../../analysis/cap110_asset_dependencies.mjs", import.meta.url ) );
	const run = spawnSync( process.execPath, [ script ], {
		input: JSON.stringify( [ { path: "compound/struct/etc/x_mas/x_mas_boxset01.cpd", bytes: cpd.toString( "base64" ) } ] ),
		encoding: "utf8"
	} );
	assert.equal( run.status, 0, run.stderr );
	assert.deepEqual( JSON.parse( run.stdout ), [ branches ] );
} );
