/*
===========================================================================

structureZones.test.mjs - the objectstring.ifo event-zone parser

Synthetic rows in the retail line grammar; no game data is read.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

import { buildStructureZoneProjection, parseObjectStringIfo } from "../../build/server/structureZones.mjs";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

test("rows decode region, float32 position and yaw bits, and the zone name", () => {
	const text = [
		"JMXVOBJI1000",
		"1",
		'0x46482801 0x00000000 72 70 0x4435269b 0xb8000000 0x43c5f736 0x3fc90fd7 "STRUCTURE_POS_JA_GUARD_TOWER_03"'
	].join( "\n" );
	const [zone] = parseObjectStringIfo( text );
	assert.equal( zone.name, "STRUCTURE_POS_JA_GUARD_TOWER_03" );
	assert.equal( zone.objectId, 0x46482801 );
	assert.equal( zone.regionId, (70 << 8) | 72 );
	assert.ok( Math.abs( zone.x - 724.6 ) < 0.01 );
	assert.ok( Math.abs( zone.z - 395.93 ) < 0.01 );
	assert.ok( Math.abs( zone.yaw - Math.PI / 2 ) < 1e-6 );
});

test("a wrong header, row count or line is refused", () => {
	assert.throws( () => parseObjectStringIfo( "JMXVOBJI0999\n0\n" ), /header/ );
	assert.throws( () => parseObjectStringIfo( "JMXVOBJI1000\n2\n" ), /declares/ );
	assert.throws( () => parseObjectStringIfo( "JMXVOBJI1000\n1\nnot a row\n" ), /malformed/ );
});

/*
================
structure zone projection merge

The port-only placement file joins the native zones when it exists
(Constantinople's 49 zones); without it the projection is the native
file alone, and a name both files place differently is refused.
================
*/
test("the port-only placement file joins, absent leaves native alone, conflicts refuse", async () => {
	const dir = await mkdtemp( path.join( os.tmpdir(), "structure-zones-" ) );
	try {
		const nativeFile = path.join( dir, "objectstring.ifo" );
		const portOnlyFile = path.join( dir, "objectstring.portonly.ifo" );
		const jaRow = '0x46482801 0x00000000 72 70 0x4435269b 0xb8000000 0x43c5f736 0x3fc90fd7 "STRUCTURE_POS_JA_GATE_01"';
		const ctRow = '0x3c5d4403 0x00000000 93 60 0x43c1ea10 0xbfb64737 0x4436ffe6 0x00000000 "STRUCTURE_POS_CT_FORT_STONE_01"';
		await writeFile( nativeFile, `JMXVOBJI1000\n1\n${jaRow}\n`, "latin1" );

		const bundle = path.join( dir, "bundle" );
		assert.equal( await buildStructureZoneProjection( bundle, nativeFile, portOnlyFile ), 1 );

		await writeFile( portOnlyFile, `JMXVOBJI1000\n1\n${ctRow}\n`, "latin1" );
		assert.equal( await buildStructureZoneProjection( bundle, nativeFile, portOnlyFile ), 2 );
		const written = JSON.parse( await readFile( path.join( bundle, "world-authority", "structure-zones.json" ), "utf8" ) );
		assert.deepEqual( written.zones.map( zone => zone.name ),
			[ "STRUCTURE_POS_JA_GATE_01", "STRUCTURE_POS_CT_FORT_STONE_01" ] );

		// An identical re-placement stands; a moved one refuses.
		await writeFile( portOnlyFile, `JMXVOBJI1000\n1\n${jaRow}\n`, "latin1" );
		assert.equal( await buildStructureZoneProjection( bundle, nativeFile, portOnlyFile ), 1 );
		await writeFile( portOnlyFile,
			`JMXVOBJI1000\n1\n${jaRow.replace( "72 70", "73 70" )}\n`, "latin1" );
		await assert.rejects( buildStructureZoneProjection( bundle, nativeFile, portOnlyFile ), /re-places/ );
	} finally {
		await rm( dir, { recursive: true, force: true } );
	}
});
