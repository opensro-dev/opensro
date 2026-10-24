/*
===========================================================================

fortressMap.test.mjs - the battle aide's fortress map publication

Synthetic textdata rows in the retail column layout; no game data is read.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";

import {
	fortressMapImageReferences,
	fortressMapPages,
	fortressMapZones,
	fortressStructures
} from "../../build/data/buildFortressMapAsset.mjs";

/*
================
characterRow

A characterdata row with the columns the map reads set.
================
*/
function characterRow( id, codeName, values ) {
	const cols = Array.from( { length: 113 }, () => "0" );
	cols[0] = "1";
	cols[1] = String( id );
	cols[2] = codeName;
	cols[5] = "SN_" + codeName;
	cols[54] = "xxx";
	for ( const [column, value] of Object.entries( values ) ) cols[Number( column )] = String( value );
	return cols;
}

test("fortress pages are the mapinfo rows of id 1000 and up that name a world", () => {
	const row = ( id, world ) => [
		String( id ),
		"1",
		"name",
		"interface\\worldmap\\map\\map_fort_jangan.ddj",
		...[ 1024, 1024, 1024, 819, 1024, 819, 69, 71, 74, 67, -48, 48, 48, 0 ].map( String ),
		world
	];
	const pages = fortressMapPages( [ row( 1, "xxx" ), row( 1001, "INS_FORT_JA" ), row( 1002, "xxx" ) ] );
	assert.deepEqual( pages, [ {
		id: 1001,
		world: "INS_FORT_JA",
		image: "interface/worldmap/map/map_fort_jangan.ddj",
		texture: [ 1024, 1024 ],
		picture: [ 1024, 819 ],
		size: [ 1024, 819 ],
		bounds: [ 69, 71, 74, 67, -48, 48, 48, 0 ]
	} ] );
});

test("zones are the page worlds' rows the client world places, by id", () => {
	const zone = (
		id,
		code,
		type,
		world
	) => [ "1", String( id ), code, type, "1", "0", "0", "0", "0", "DEFAULT", world ];
	const zones = fortressMapZones(
		[
			zone( 92, "POS_B", "DEFENSIVE_POSITION", "INS_FORT_JA" ),
			zone( 91, "POS_A", "GUARD_TOWER", "INS_FORT_JA" ),
			zone( 93, "POS_UNPLACED", "GATE", "INS_FORT_JA" ),
			zone( 1, "GATE_DUNGEON", "TELEPORT", "xxx" )
		],
		[
			{ name: "POS_A", regionId: 0x4648, x: 1, y: 2, z: 3 },
			{ name: "pos_b", regionId: 0x4649, x: 4, y: 5, z: 6 },
			{ name: "GATE_DUNGEON", regionId: 1, x: 0, y: 0, z: 0 }
		],
		new Set( [ "INS_FORT_JA" ] )
	);
	assert.deepEqual( zones.map( row => [ row.id, row.type, row.regionId, row.x ] ), [
		[ 91, "GUARD_TOWER", 0x4648, 1 ],
		[ 92, "DEFENSIVE_POSITION", 0x4649, 4 ]
	] );
});

test("structures carry their fields and upgrade targets until the first xxx", () => {
	const structures = fortressStructures(
		[
			characterRow( 10, "STRUCTURE_T_01", {
				54: "etc\\strut_guard_tower.ddj",
				57: 1,
				59: 500,
				77: 220,
				110: 10000,
				111: 1000
			} ),
			characterRow( 11, "STRUCTURE_T_02", { 57: 2, 89: 20527 } ),
			characterRow( 12, "MOB_NOT_A_STRUCTURE", {} )
		],
		[
			[ "1", "STRUCTURE_T_01", "STRUCTURE_T_01", "STRUCTURE_T_02", "STRUCTURE_T_99", "xxx", "STRUCTURE_T_02" ],
			[ "0", "STRUCTURE_T_02", "STRUCTURE_T_01", "STRUCTURE_T_01", "xxx", "xxx", "xxx" ]
		]
	);
	assert.deepEqual( Object.keys( structures ), [ "10", "11" ] );
	assert.equal( structures[10].icon, "etc/strut_guard_tower.ddj" );
	assert.deepEqual(
		[ structures[10].level, structures[10].maxHp, structures[10].defence, structures[10].upgradeGold ],
		[ 1, 500, 220, 10000 ]
	);
	assert.deepEqual( structures[10].upgrades, [ 11, 0 ] );
	assert.equal( structures[11].upgrades, null, "a disabled row adds no targets" );
	assert.equal( structures[11].skill, 20527 );
	assert.deepEqual( fortressMapImageReferences( structures ), [
		"icon/etc/strut_guard_tower.ddj",
		"icon/etc/strut_guard_tower_d.ddj",
		"icon/etc/strut_guard_tower_r.ddj",
		"interface/worldmap/map/gate.ddj"
	] );
});
