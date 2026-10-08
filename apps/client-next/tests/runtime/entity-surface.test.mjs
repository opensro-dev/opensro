/*
===========================================================================

entity-surface.test.mjs - tests for entities.ts, navigation.ts,
navigation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as assetBytes } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createNavigation } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
);
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const bytes = p => assetBytes( p, CLIENT_PUBLIC_ROOT );
const product = await createNavigationResources().resolve(
	bytes( "/assets/world/outdoor/regions/region-61a8.json" ),
	0x61a8,
	async path => bytes( path )
);
function flush( e ) {
	const b = e.take();
	if ( b ) e.ack( b.sequence );
	return b;
}
function owner( nav, kind = "npc" ) {
	const e = createEntities( ( p, r, c ) => nav.surface( p, r, undefined, c ), undefined, undefined, nav.clip );
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 2029, kind, tidWord: kind === "cos" ? 0x11c6 : 0 } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 25000, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( e );
	return e;
}
function spawn( x, z, { moving = false, kind = "npc", y = 11, distance = 100 } = {} ) {
	const p = Buffer.alloc( 49 + (moving ? 5 : 0) + (kind === "monster" ? 1 : kind === "cos" ? 8 : 0) );
	p.writeUInt32LE( 2029 );
	p.writeUInt32LE( 7, 4 );
	p.writeUInt16LE( 25000, 8 );
	p.writeFloatLE( x, 10 );
	p.writeFloatLE( y, 14 );
	p.writeFloatLE( z, 18 );
	p[24] = moving ? 1 : 0;
	p[25] = 3;
	let o = 29;
	if ( moving ) {
		p.writeUInt16LE( 25000, 26 );
		p.writeInt16LE( Math.round( x ) + distance, 28 );
		p.writeInt16LE( y, 30 );
		p.writeInt16LE( Math.round( z ), 32 );
		o = 34;
	}
	p[o] = 1;
	p.writeFloatLE( 10, o + 3 );
	p.writeFloatLE( 20, o + 7 );
	p.writeFloatLE( 1, o + 11 );
	p[o + 16] = 1;
	return { opcode: 0x30d7, payload: p };
}
for ( const navFirst of [ true, false ] ) {
	test(`Jangan Flora/Mamun and neighboring NPC surfaces (nav first=${navFirst})`, () => {
		for ( const [x, z] of [ [ 1670.79, 260.54 ], [ 1670.68, 580.81 ], [ 1671.18, 472.04 ], [ 1670.92, 408.83 ] ] ) {
			const nav = createNavigation();
			if ( navFirst ) nav.install( 25000, product );
			const e = owner( nav );
			e.receive( spawn( x, z ), 0 );
			flush( e );
			if ( !navFirst ) {
				assert.equal( defined( e.read( 7 ) ).y, 11 );
				nav.install( 25000, product );
				e.groundSpawns();
				flush( e );
			}
			const y = defined( e.read( 7 ) ).y;
			assert.ok( Math.abs( y ) < .001, `published navigation ${x},${z}: ${y}` );
			e.groundSpawns();
			assert.equal( flush( e ), null );
			e.dispose();
		}
	});
}
for ( const kind of [ "npc", "monster", "cos" ] ) {
	test(`${kind} moving spawn and late admission preserve arrival`, () => {
		for ( const navFirst of [ true, false ] ) {
			const nav = createNavigation();
			if ( navFirst ) nav.install( 25000, product );
			const e = owner( nav, kind );
			e.receive( spawn( 1671, 261, { kind, moving: true, distance: 40 } ), 0 );
			flush( e );
			if ( navFirst ) assert.ok( Math.abs( defined( e.read( 7 ) ).y ) < .001 );
			e.step( 1000 );
			flush( e );
			const before = defined( e.read( 7 ) ).x;
			if ( !navFirst ) {
				nav.install( 25000, product );
				e.groundSpawns();
				flush( e );
				assert.equal( defined( e.read( 7 ) ).x, before );
			}
			e.step( 2000 );
			flush( e );
			assert.ok( Math.abs( defined( e.read( 7 ) ).x - (navFirst ? 1711 : 1691) ) < .001 );
			assert.ok( Number.isFinite( defined( e.read( 7 ) ).y ) );
			assert.notEqual( defined( e.read( 7 ) ).y, 11 );
			e.step( 5000 );
			flush( e );
			assert.equal( defined( e.read( 7 ) ).x, 1711 );
			assert.equal( defined( e.read( 7 ) ).moving, false );
			e.dispose();
		}
	});
}

test("spawn initialization selects a raised nav floor and excludes teleport props", () => {
	const heights = Buffer.alloc( 97 * 97 * 4 ),
		mesh = {
			vertices: Float32Array.from( [ 0, 10, 0, 100, 10, 0, 100, 10, 100, 0, 10, 100 ] ),
			cells: Uint16Array.from( [ 0, 1, 2, 0, 2, 3 ] ),
			edges: new Uint32Array(),
			bounds: [ 0, 10, 0, 100, 10, 100 ],
			passThrough: false
		};
	const p = {
		regionId: 25000,
		complete: true,
		objects: [ { x: 0, y: 0, z: 0, yaw: 0, mesh } ],
		navmesh: {
			regionSize: 1920,
			tileSize: 20,
			tilesPerAxis: 96,
			regions: [ {
				dx: 0,
				dz: 0,
				blockedTiles: Buffer.alloc( 9216 ).toString( "base64" ),
				tileCellIds: Buffer.alloc( 36864 ).toString( "base64" ),
				heightMap: heights.toString( "base64" ),
				cells: { count: 1 }
			} ]
		}
	};
	for ( const navFirst of [ true, false ] ) {
		const nav = createNavigation();
		if ( navFirst ) nav.install( 25000, p );
		const e = owner( nav );
		e.receive( spawn( 30, 40 ), 0 );
		flush( e );
		if ( !navFirst ) {
			nav.install( 25000, p );
			e.groundSpawns();
			flush( e );
		}
		assert.equal( defined( e.read( 7 ) ).y, 10 );
		e.dispose();
	}
	const nav = createNavigation();
	nav.install( 25000, p );
	const e = createEntities( ( p, r, c ) => nav.surface( p, r, undefined, c ), undefined, undefined, nav.clip );
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 2029, kind: "teleport", teleport: { radius: 10, height: 20 } } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 25000, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( e );
	e.receive( { opcode: 0x30d7, payload: spawn( 30, 40 ).payload.subarray( 0, 24 ) }, 0 );
	flush( e );
	e.groundSpawns();
	assert.equal( defined( e.read( 7 ) ).y, 11 );
	assert.equal( flush( e ), null );
	e.dispose();
});
