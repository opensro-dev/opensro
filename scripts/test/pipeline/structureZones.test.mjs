/*
===========================================================================

structureZones.test.mjs - the objectstring.ifo event-zone parser

Synthetic rows in the retail line grammar; no game data is read.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

import { parseObjectStringIfo } from "../../build/server/structureZones.mjs";

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
