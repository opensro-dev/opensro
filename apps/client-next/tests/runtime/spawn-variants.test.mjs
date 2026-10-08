/*
===========================================================================

spawn-variants.test.mjs - tests for character-spawn.ts, peer-appearance.ts,
motion.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { decodeCharacterSpawn } = await import( "../../src/engine/foundation/gameplay/character-spawn.ts" ),
	{ decodePeerAppearance } = await import( "../../src/engine/foundation/gameplay/peer-appearance.ts" ),
	{ createEntityMotion } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
	);
function str( s ) {
	const text = Buffer.from( s ), len = Buffer.alloc( 2 );
	len.writeUInt16LE( text.length );
	return Buffer.concat( [ len, text ] );
}
const u32 = n => {
	const p = Buffer.alloc( 4 );
	p.writeUInt32LE( n );
	return p;
};
function cos( band, moving = false ) {
	const base = Buffer.alloc( moving ? 49 : 44 );
	base.writeUInt32LE( 3914 );
	base.writeUInt32LE( 7, 4 );
	base.writeUInt16LE( 257, 8 );
	base[24] = moving ? 1 : 0;
	base[25] = 2;
	let scalar = 29;
	if ( moving ) {
		base.writeUInt16LE( 257, 26 );
		base.writeInt16LE( 100, 28 );
		scalar = 34;
	}
	base.writeFloatLE( 10, scalar + 3 );
	base.writeFloatLE( 20, scalar + 7 );
	base.writeFloatLE( 100, scalar + 11 );
	const parts = [ base, Buffer.from( [ 0, 3 ] ), str( "Pet" ), u32( 99 ) ];
	if ( band === 3 || band === 4 ) parts.push( str( "Growth" ) );
	if ( [ 2, 3, 4, 5, 6 ].includes( band ) ) {
		parts.push( str( "Owner" ) );
		if ( band !== 6 ) {
			parts.push( Buffer.from( [ 2 ] ) );
			if ( band !== 4 ) parts.push( Buffer.from( [ 3 ] ) );
		}
	}
	if ( band === 5 ) parts.push( u32( 1907 ) );
	if ( band !== 1 ) parts.push( u32( 123 ) );
	parts.push( Buffer.from( [ 1 ] ) );
	return Buffer.concat( parts );
}
test("COS name masks and all native sub-kind tails consume exact boundaries", () => {
	for ( let band = 1; band <= 6; band++ ) {
		for ( const moving of [ false, true ] ) {
			const p = cos( band, moving ), e = decodeCharacterSpawn( p, "cos", 0x1c6 | (band << 11), true );
			assert.equal( e.name, band === 3 || band === 4 ? "Growth" : "Pet" );
			assert.equal( e.ownerGid, band === 1 ? undefined : 123 );
			assert.equal( e.pvpState, [ 2, 3, 5 ].includes( band ) ? 3 : undefined );
			assert.equal( e.spawnDestination?.x, moving ? 100 : undefined );
			assert.equal(
				e.tidWord,
				0x1c6 | (band << 11),
				"reference subtype survives admission for presentation routing"
			);
			for ( let n = 0; n < p.length; n++ ) {
				assert.throws( () => decodeCharacterSpawn( p.subarray( 0, n ), "cos", 0x1c6 | (band << 11), true ) );
			}
			if ( moving ) {
				const motion = createEntityMotion( undefined, ( _from, to ) => to );
				motion.spawn( e, 0 );
				assert.equal( motion.step( 1000 )[0].x, 10 );
			}
		}
	}
});
test("native peer ride gate and nonzero appearance metadata preserve the full row", () => {
	const row = JSON.parse(
			fs.readFileSync( "../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", "utf8" )
		).scenarios[0],
		raw = Buffer.from( row.payloadHex, "hex" );
	const tail = raw.indexOf( Buffer.from( row.characterName ) ) + Buffer.byteLength( row.characterName );
	raw[tail] = 2;
	raw[tail + 1] = 4;
	raw[tail + 2] = 7;
	raw[tail + 3] = 1;
	raw[tail + 4] = 6;
	const p = Buffer.concat( [ raw.subarray( 0, tail + 5 ), u32( 77 ), raw.subarray( tail + 5 ) ] ),
		entity = decodePeerAppearance( p, new Map(), true );
	assert.equal( entity.mountedOn, 77 );
	assert.equal( entity.jobType, 2 );
	assert.equal( entity.jobGrade, 4 );
	assert.equal( defined( entity.appearanceState )[3], 7 );
	for ( let n = 0; n < p.length; n++ ) {
		assert.throws( () => decodePeerAppearance( p.subarray( 0, n ), new Map(), true ) );
	}
});

test("peer title is a UTF-16 string followed by an ID, not a nested packet", () => {
	const row = JSON.parse(
			fs.readFileSync( "../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", "utf8" )
		).scenarios[0],
		raw = Buffer.from( row.payloadHex, "hex" );
	const tail = raw.indexOf( Buffer.from( row.characterName ) ) + Buffer.byteLength( row.characterName );
	raw[tail + 6] = 4;
	const title = Buffer.from( "\u738b\u8005", "utf16le" ), length = Buffer.from( [ 2, 0 ] );
	const at = raw.length - 3;
	const p = Buffer.concat( [ raw.subarray( 0, at ), length, title, u32( 1234 ), raw.subarray( at ) ] );
	const e = decodePeerAppearance( p, new Map(), true );
	assert.equal( e.titleText, "\u738b\u8005" );
	assert.equal( e.titleId, 1234 );
	for ( let n = 0; n < p.length; n++ ) {
		assert.throws( () => decodePeerAppearance( p.subarray( 0, n ), new Map(), true ) );
	}
});

test("moving spawn rejects a dungeon transition before entity admission", () => {
	const p = cos( 2, true );
	p.writeUInt16LE( 0x8001, 8 );
	p.writeUInt16LE( 0x8002, 26 );
	assert.throws( () => decodeCharacterSpawn( p, "cos", 0x11c6, true ), /requires teleport/ );
});

test("peer and COS spawn skill variants consume their metadata-dependent bytes before names", () => {
	const refs = new Map( [ [ 17, { token: true, status: true } ], [ 18, { token: false, status: false } ] ] ),
		skills = Buffer.concat( [ Buffer.from( [ 2 ] ), u32( 17 ), u32( 90 ), Buffer.from( [ 1 ] ), u32( 18 ) ] );
	const row = JSON.parse(
			fs.readFileSync( "../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", "utf8" )
		).scenarios[0],
		raw = Buffer.from( row.payloadHex, "hex" ),
		at = raw.indexOf( Buffer.from( row.characterName ) ) - 3;
	assert.equal( raw[at], 0 );
	const peer = Buffer.concat( [ raw.subarray( 0, at ), skills, raw.subarray( at + 1 ) ] );
	assert.equal( decodePeerAppearance( peer, new Map(), true, new Map(), refs ).name, row.characterName );
	assert.equal( defined( decodePeerAppearance( peer, new Map(), true, new Map(), refs ).spawnSkills ).length, 2 );
	for ( const moving of [ false, true ] ) {
		const raw = cos( 4, moving ),
			at = moving ? 49 : 44,
			p = Buffer.concat( [ raw.subarray( 0, at ), skills, raw.subarray( at + 1 ) ] ),
			e = decodeCharacterSpawn( p, "cos", 0x21c6, true, refs );
		assert.equal( e.name, "Growth" );
		assert.equal( defined( e.spawnSkills )[0].token, 90 );
		for ( let n = 0; n < p.length; n++ ) {
			assert.throws( () => decodeCharacterSpawn( p.subarray( 0, n ), "cos", 0x21c6, true, refs ) );
		}
	}
});
test("86B14F: a transformed peer carries its skin RefObj; a monster skin brings no equipment", () => {
	const row = JSON.parse(
			fs.readFileSync( "../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", "utf8" )
		).scenarios[0],
		raw = Buffer.from( row.payloadHex, "hex" );
	const at = 4 + 2 + 2 + 2;
	assert.equal( raw[at], 0, "the fixture row is untransformed" );
	const p = Buffer.concat( [ raw.subarray( 0, at ), Buffer.from( [ 1 ] ), u32( 1933 ), raw.subarray( at + 1 ) ] ),
		refs = new Map( [ [ 1933, { tidWord: 0xc6 | 0x800 } ] ] );
	const e = decodePeerAppearance( p, new Map(), true, refs );
	assert.deepEqual( e.transformSkin, { refObjId: 1933, player: false, equipment: [], revision: 1 } );
	assert.equal( e.gid, row.gid );
	assert.equal( decodePeerAppearance( raw, new Map(), true, refs ).transformSkin, undefined );
	assert.throws( () => decodePeerAppearance( p, new Map(), true, new Map() ), /skin reference/ );
	for ( let n = 0; n < p.length; n++ ) {
		assert.throws( () => decodePeerAppearance( p.subarray( 0, n ), new Map(), true, refs ) );
	}
});
test("4DD6B0: a Duplicate wears the copied player; only slotted equipment is dressed, at plus 0", () => {
	const row = JSON.parse(
			fs.readFileSync( "../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", "utf8" )
		).scenarios[0],
		raw = Buffer.from( row.payloadHex, "hex" );
	const at = 4 + 2 + 2 + 2, armour = 0x2c | 0x80 | 0x800, etc = 0x6c;
	const p = Buffer.concat( [
		raw.subarray( 0, at ),
		Buffer.from( [ 1 ] ),
		u32( 1907 ),
		Buffer.from( [ 4, 2 ] ),
		u32( 3643 ),
		u32( 5 ),
		raw.subarray( at + 1 )
	] );
	const e = decodePeerAppearance(
		p,
		new Map( [ [ 3643, armour ], [ 5, etc ] ] ),
		true,
		new Map( [ [ 1907, { tidWord: 0x26 } ] ] )
	);
	assert.deepEqual( e.transformSkin, {
		refObjId: 1907,
		player: true,
		equipment: [ { slot: 0, refObjId: 3643, typeFlags: armour, plus: 0 } ],
		revision: 1
	} );
	assert.throws(
		() => decodePeerAppearance( p, new Map( [ [ 5, etc ] ] ), true, new Map( [ [ 1907, { tidWord: 0x26 } ] ] ) ),
		/skin equipment/
	);
});

/*
================
standingRow

A standing actor's shared block (85FB20) with no buffs and a named mask,
after the RefObjID and an optional class prefix.
================
*/
function standingRow( refObjId, prefix, name ) {
	const block = Buffer.alloc( 40 );
	block.writeUInt32LE( 7, 0 );
	block.writeUInt16LE( 17991, 4 );
	block.writeFloatLE( 849, 6 );
	block.writeFloatLE( 1065, 14 );
	block[21] = 2;
	block.writeFloatLE( 10, 28 );
	block.writeFloatLE( 20, 32 );
	block.writeFloatLE( 100, 36 );
	return Buffer.concat( [ u32( refObjId ), prefix, block, Buffer.from( [ 0, 1 ] ), str( name ) ] );
}

test("a fortress structure's hit points, zone and state precede the shared block (4FA0B0)", () => {
	const prefix = Buffer.concat( [ u32( 1170000 ), u32( 84 ), Buffer.from( [ 4, 0 ] ) ] );
	const stone = decodeCharacterSpawn( standingRow( 19553, prefix, "Stone" ), "structure", 0x2c6, false );
	assert.deepEqual( [ stone.structureHp, stone.eventStructId, stone.structureState, stone.gid, stone.name ], [
		1170000,
		84,
		4,
		7,
		"Stone"
	] );
	const headquarters = Buffer.concat( [ standingRow( 19553, prefix, "HQ" ), u32( 0 ), Buffer.from( [ 1 ] ) ] );
	const hq = decodeCharacterSpawn( headquarters, "structure", 0x2c6 | (5 << 11), true );
	assert.equal( hq.guildId, 0 );
	assert.equal( hq.guildName, undefined, "an unheld headquarters sends no guild name" );
	assert.equal( hq.spawnAppearance, 1 );
});

test("a thief or hunter monster ends with its trade equipment variant (861B00)", () => {
	for ( const band of [ 1, 2, 3, 4 ] ) {
		const trade = band === 2 || band === 3;
		const row = Buffer.concat( [
			standingRow( 1, Buffer.alloc( 0 ), "Thief" ),
			Buffer.from( trade ? [ 0, 9 ] : [ 0 ] )
		] );
		const e = decodeCharacterSpawn( row, "monster", 0xc6 | (band << 11), false );
		assert.equal( e.tradeVariant, trade ? 9 : undefined );
	}
});
