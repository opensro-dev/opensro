/*
===========================================================================

navigation-events.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { navigationEventDirection, navigationEvents } = await load( "foundation/navigation/navigation-events.ts" );
const { dungeonOwnerPath } = await load( "foundation/navigation/dungeon-ownership.ts" );
const { objectNavigation } = await load( "foundation/navigation/object-navigation.ts" );
const { createNavigation } = await load(
	"runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
);
const room = ( y = 0 ) => ({
	x: 0,
	y,
	z: 0,
	yaw: 0,
	mesh: {
		vertices: Float32Array.of( 0, 0, 0, 100, 0, 0, 100, 0, 100, 0, 0, 100 ),
		cells: Uint16Array.of( 0, 1, 2, 0, 2, 3 ),
		edges: Uint32Array.of( 0, 2, 0, 1, 4, 1 ),
		bounds: [ 0, 0, 0, 100, 0, 100 ],
		passThrough: false,
		cellEvents: Uint8Array.of( 192, 193 ),
		edgeEvents: Uint8Array.of( 194 ),
		eventNames: [ "source", "destination", "edge" ]
	}
});
test("direction gates use signed cell order including the absent -1 sentinel", () => {
	for ( let byte = 0; byte < 256; byte++ ) {
		for ( const [from, to] of [ [ -1, 0 ], [ 0, -1 ], [ 0, 0 ], [ 2, 9 ], [ 9, 2 ] ] ) {
			assert.equal(
				navigationEventDirection( byte, from, to ),
				(byte & 64) && from < to ? true : (byte & 128) && from > to ? false : null
			);
		}
	}
	assert.throws( () => navigationEventDirection( 256, 0, 1 ), /Invalid/ );
});
test("edge event precedes source-cell exit and destination-cell entry; shared names suppress cell churn", () => {
	const objects = [ room() ], path = dungeonOwnerPath( objects, [ 75, 0, 25 ], [ 25, 0, 75 ] );
	assert.deepEqual( path.spans.map( s => s.cell ), [ 0, 1 ] );
	assert.deepEqual( navigationEvents( objects, path.spans, 7 ).map( e => [ e.name, e.entering, e.context ] ), [
		[ "edge", true, 7 ],
		[ "source", false, 7 ],
		[ "destination", true, 7 ]
	] );
	objects[0].mesh.cellEvents[1] = 192;
	assert.deepEqual( navigationEvents( objects, path.spans, 7 ).map( e => e.name ), [ "edge" ] );
});
test("a physically overlapping placement cannot steal an existing dungeon owner", () => {
	const objects = [ room(), room( .1 ) ];
	const path = dungeonOwnerPath( objects, [ 75, 0, 25 ], [ 25, .1, 75 ] );
	assert.deepEqual( path.spans.map( s => s.placement ), [ 0, 0 ] );
	const nav = createNavigation();
	nav.install( 0x8001, { regionId: 0x8001, complete: true, objects } );
	const from = { regionId: 0x8001, x: 75, y: 0, z: 25, angle: 0 }, output = { slide: false };
	const point = nav.clip( from, { ...from, x: 25, y: .1, z: 75 }, output );
	assert.equal( point.y, 0 );
	assert.deepEqual( output.owner, { placement: 0, cell: 1 } );
	nav.clear();
});
test("unlinked coincident room boundaries do not create a placement transition", () => {
	const a = room(), b = room();
	b.x = 100;
	const path = dungeonOwnerPath( [ a, b ], [ 50, 0, 25 ], [ 150, 0, 25 ] );
	assert.equal( path.stop, .5 );
	assert.equal( path.owner.placement, 0 );
});
test("event response registration is copied, replaced and cleared by the navigation owner", () => {
	const nav = createNavigation(), objects = [ room() ], regionId = 0x8001;
	nav.install( regionId, { regionId, complete: true, objects } );
	const from = { regionId, x: 75, y: 0, z: 25, angle: 0 },
		to = { ...from, x: 25, z: 75 },
		output = { slide: false, context: 7 };
	const response = { enter: 0, exit: 0 };
	nav.registerEvent( "edge", response );
	response.enter = 1;
	assert.ok( nav.clip( from, to, output ) );
	assert.equal( output.events.length, 3 );
	nav.registerEvent( "edge", { enter: 1, exit: 0 } );
	const clipped = nav.clip( from, to, output );
	assert.ok( clipped.x > 50 && clipped.z < 50 );
	assert.deepEqual( output.owner, { placement: 0, cell: 0 } );
	nav.registerEvent( "edge", { enter: 0x10000000, exit: 0 } );
	assert.equal( nav.clip( from, to, output ), null );
	assert.ok( nav.clip( from, to, { slide: false, context: 0 } ) );
	nav.unregisterEvent( "edge" );
	assert.ok( nav.clip( from, to, output ) );
	nav.registerEvent( "edge", { enter: 1, exit: 0 } );
	nav.clear();
	nav.install( regionId, { regionId, complete: true, objects } );
	assert.ok( nav.clip( from, to, output ) );
	assert.throws( () => nav.registerEvent( "edge", { enter: NaN, exit: 0 } ), /Invalid/ );
	nav.clear();
});
test("BMS decoding preserves cell words, both event columns and registry strings", () => {
	const parts = [];
	const u8 = x => parts.push( Buffer.from( [ x ] ) ),
		u16 = x => {
			const b = Buffer.alloc( 2 );
			b.writeUInt16LE( x );
			parts.push( b );
		},
		u32 = x => {
			const b = Buffer.alloc( 4 );
			b.writeUInt32LE( x );
			parts.push( b );
		},
		f32 = x => {
			const b = Buffer.alloc( 4 );
			b.writeFloatLE( x );
			parts.push( b );
		};
	u32( 3 );
	for ( const v of [ [ 0, 0, 0 ], [ 100, 0, 0 ], [ 0, 0, 100 ] ] ) {
		v.forEach( f32 );
		u8( v[0] === 100 ? 64 : v[2] === 100 ? 128 : 0 );
	}
	u32( 1 );
	[ 0, 1, 2, 73 ].forEach( u16 );
	u8( 192 );
	u32( 1 );
	[ 0, 1, 0, 65535 ].forEach( u16 );
	u8( 2 );
	u8( 128 );
	u32( 0 );
	u32( 1 );
	u32( 4 );
	parts.push( Buffer.from( "gate" ) );
	f32( 0 );
	f32( 0 );
	u32( 1 );
	u32( 1 );
	u32( 1 );
	u32( 1 );
	u16( 0 );
	const raw = Buffer.concat( parts ), offsets = Array( 12 ).fill( 0 );
	offsets[7] = 64;
	offsets[11] = 7;
	const [mesh] = objectNavigation( {
		sourcePath: "fixture",
		byteLength: 64 + raw.length,
		headerOffsets: offsets,
		nativePayloads: [ {
			kind: "bms-offset7-post-payload-tail",
			byteOffset: 64,
			byteLength: raw.length,
			rawBase64: raw.toString( "base64" )
		} ]
	} );
	assert.deepEqual( [ ...mesh.vertexDirections ], [ 0, 64, 128 ] );
	assert.deepEqual( [ ...mesh.cellWords ], [ 73 ] );
	assert.deepEqual( [ ...mesh.cellEvents ], [ 192 ] );
	assert.deepEqual( [ ...mesh.edgeEvents ], [ 128 ] );
	assert.deepEqual( mesh.eventNames, [ "gate" ] );
});
test("preferred owner survives coincident triangles and rejects invalid indices", () => {
	const objects = [ room(), room() ];
	const nav = createNavigation(), regionId = 0x8001;
	nav.install( regionId, { regionId, complete: true, objects } );
	const pose = { regionId, x: 75, y: 0, z: 25, angle: 0 };
	const query = { slide: false, sourceOwner: { placement: 1, cell: 0 }, owner: undefined };
	assert.deepEqual( nav.clip( pose, pose, query ), pose );
	assert.deepEqual( query.owner, query.sourceOwner, "zero-speed steps retain their admitted floor" );
	assert.equal(
		dungeonOwnerPath( objects, [ 75, 0, 25 ], [ 25, 0, 75 ], { placement: 1, cell: 0 } ).owner.placement,
		1
	);
	assert.throws(
		() => dungeonOwnerPath( objects, [ 75, 0, 25 ], [ 25, 0, 75 ], { placement: 2, cell: 0 } ),
		/Invalid dungeon owner/
	);
});
test("movement retains the traversed cell as its clock advances and across admission replacement", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const movement = createMovement( () => {} ), regionId = 0x8001, from = { regionId, x: 75, y: 0, z: 25, angle: 0 };
	movement.seed( from );
	movement.navigation( regionId, { regionId, complete: true, objects: [ room() ] } );
	movement.request( { ...from, x: 25, z: 75 }, 0 );
	movement.step( 100 );
	assert.deepEqual( movement.state().navigationOwner, { placement: 0, cell: 0 } );
	movement.step( 1000 );
	assert.deepEqual( movement.state().navigationOwner, { placement: 0, cell: 1 } );
	// A replacement admission clones its placements; the same placed object is
	// relocated by world geometry, as the native cell pointer survives a reload.
	movement.navigation( regionId, { regionId, complete: true, objects: [ room() ] } );
	assert.deepEqual( movement.state().navigationOwner, { placement: 0, cell: 1 } );
	movement.step( 1100 );
	assert.deepEqual( movement.state().navigationOwner, { placement: 0, cell: 1 } );
	// An admission without that object must not guess a stale index.
	movement.navigation( regionId, { regionId, complete: true, objects: [ { ...room(), x: 500 } ] } );
	assert.equal( movement.state().navigationOwner, undefined );
	movement.clear();
});

const stairs = () => ({
	x: 0,
	y: 0,
	z: 0,
	yaw: 0,
	mesh: {
		vertices: Float32Array.of( 0, 0, 0, 50, 20, 0, 100, 0, 0, 0, 0, 10, 50, 20, 10, 100, 0, 10 ),
		cells: Uint16Array.of( 0, 1, 3, 1, 4, 3, 1, 2, 4, 2, 5, 4 ),
		edges: new Uint32Array(),
		bounds: [ 0, 0, 0, 100, 20, 10 ],
		passThrough: false
	}
});
test("connected ramp ownership follows triangle height instead of rejecting its endpoint chord", () => {
	const path = dungeonOwnerPath( [ stairs() ], [ 10, 4, 5 ], [ 90, 4, 5 ] );
	assert.equal( path.stop, 1 );
	assert.equal( path.owner.cell, 3 );
	assert.ok( path.spans.length >= 3 );
	const nav = createNavigation(), regionId = 0x8001;
	nav.install( regionId, { regionId, complete: true, objects: [ stairs() ] } );
	const p = { regionId, x: 10, y: 4, z: 5, angle: 0 }, out = { slide: false };
	const to = nav.clip( p, { ...p, x: 90 }, out );
	assert.equal( to.x, 90 );
	assert.ok( Math.abs( to.y - 4 ) < 1e-6 );
	assert.equal( out.owner.cell, 3 );
});
test("movement receipt preserves the stair layer over an overlapping lower floor", async () => {
	const { createMovement } = await load( "runtime/simulation/worker/session/world/gameplay/movement/movement.ts" );
	const m = createMovement( () => {} ), regionId = 0x8001, p = { regionId, x: 10, y: 4, z: 5, angle: 0 };
	m.seed( p );
	m.navigation( regionId, { regionId, complete: true, objects: [ stairs(), room( 3 ) ] } );
	m.request( { ...p, x: 90 }, 0 );
	m.step( 400 );
	assert.equal( m.state().navigationOwner.placement, 0 );
	assert.ok( Math.abs( m.state().pose.y - 12 ) < 1e-6 );
	m.receive(
		Buffer.from(
			JSON.stringify( {
				v: 1,
				id: 1,
				gid: 7,
				accepted: true,
				serverTimeMs: 400,
				world: { spawn: { ...p, x: 90 }, moveSegment: { from: p, startedAtMs: 0, arrivesAtMs: 1600 } }
			} )
		),
		400,
		7
	);
	m.step( 800 );
	assert.equal( m.state().navigationOwner.placement, 0 );
	assert.ok( Math.abs( m.state().pose.y - 20 ) < 1e-6 );
	m.request( { ...p, x: 20, y: 8 }, 801 );
	m.step( 1001 );
	assert.equal( m.state().navigationOwner.placement, 0 );
	assert.ok( m.state().pose.y > 3 );
});

test("remote movement retains the stair layer across updates, gait changes and navigation replacement", async () => {
	const { createEntityMotion } = await load( "runtime/simulation/worker/session/world/entities/motion/motion.ts" );
	const nav = createNavigation(), regionId = 0x8001, objects = [ stairs(), room( 3 ) ];
	nav.install( regionId, { regionId, complete: true, objects } );
	const motion = createEntityMotion(
		( pose, reference, cursor ) => nav.surface( pose, reference, undefined, cursor ),
		nav.clip
	);
	const entity = {
		gid: 9,
		regionId,
		x: 10,
		y: 4,
		z: 5,
		heading: 0,
		movementMode: 3,
		runSpeed: 20,
		walkSpeed: 10,
		spawnDestination: { regionId, x: 90, y: 4, z: 5, angle: 0 }
	};
	motion.spawn( entity, 0 );
	let update = motion.step( 1000 )[0];
	assert.ok( Math.abs( update.y - 12 ) < .001 );
	update = motion.mode( { ...entity, ...update, movementMode: 2 }, 1000 );
	assert.ok( Math.abs( update.y - 12 ) < .001 );
	update = motion.step( 2000 )[0];
	assert.ok( Math.abs( update.y - 16 ) < .001 );
	// Admission replaces placement indices; a stale cursor must not bind the other floor.
	nav.install( regionId, { regionId, complete: true, objects: [ room( 3 ), stairs() ] } );
	motion.surfaceReference( { ...entity, ...update } );
	update = motion.step( 2500 )[0];
	assert.ok( Math.abs( update.y - 18 ) < .001 );
	motion.remove( 9 );
	assert.deepEqual( motion.step( 4000 ), [] );
});

test("source exit keeps requested XZ; destination entry insets into source before side blocking", () => {
	const nav = createNavigation(), regionId = 0x8001, objects = [ room() ];
	nav.install( regionId, { regionId, complete: true, objects } );
	const from = { regionId, x: 75, y: 0, z: 25, angle: 0 },
		to = { ...from, x: 25, z: 75 },
		out = { slide: false, context: 7 };
	nav.registerEvent( "source", { enter: 0, exit: 1 } );
	assert.deepEqual( nav.clip( from, to, out ), to );
	assert.deepEqual( out.owner, { placement: 0, cell: 0 } );
	assert.deepEqual( out.events.map( e => e.name ), [ "edge", "source" ] );
	nav.unregisterEvent( "source" );
	nav.registerEvent( "destination", { enter: 1, exit: 0 } );
	const clipped = nav.clip( from, to, out );
	assert.ok( clipped.x > 50 && clipped.z < 50 );
	assert.deepEqual( out.owner, { placement: 0, cell: 0 } );
	objects[0].mesh.edges[4] = 6;
	nav.install( regionId, { regionId, complete: true, objects } );
	assert.deepEqual( nav.clip( from, to, out ), clipped );
	assert.equal( out.events.at( -1 ).name, "destination" );
	nav.clear();
});
