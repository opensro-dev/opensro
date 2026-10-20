/*
===========================================================================

dungeon-room-culling.test.mjs - the dungeon block an entity stands in

Port-only, not native (Experimental "Dungeon character PVS", on unless
explicitly off). The simulation worker publishes each dungeon entity's
block from its navigation owner (entities.ts withBlock), and the presenter
hides an actor whose block the active block does not draw. The local
player and its mount never hide, a rider is judged by its mount, and an
actor whose holder has left keeps the block copied onto it at assembly.
Neither runs outdoors.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { publishCharacters } = await import( "../../src/engine/runtime/characters/presentation-finalize.ts" );
const { experimentalOptions, experimentalVideo } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);

const DUNGEON = 0x8001, OUTDOOR = 25000, WEST_ROOM = 3, EAST_ROOM = 4, DOOR_X = 500;
const roomAt = x => x < DOOR_X ? WEST_ROOM : EAST_ROOM;
function flush( e ) {
	const b = e.take();
	if ( b ) e.ack( b.sequence );
	return b;
}
function world( calls ) {
	const e = createEntities(
		pose => pose,
		undefined,
		undefined,
		( from, to ) => to,
		( pose, owner ) => {
			calls.push( { regionId: pose.regionId, owner } );
			return roomAt( pose.x );
		}
	);
	e.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 2029, kind: "monster", tidWord: 0 } ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: OUTDOOR, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush( e );
	return e;
}
function spawn( region, x, { moving = false, distance = 100 } = {} ) {
	const p = Buffer.alloc( 49 + (moving ? 5 : 0) + 1 );
	p.writeUInt32LE( 2029 );
	p.writeUInt32LE( 7, 4 );
	p.writeUInt16LE( region, 8 );
	p.writeFloatLE( x, 10 );
	p.writeFloatLE( 11, 14 );
	p.writeFloatLE( 40, 18 );
	p[24] = moving ? 1 : 0;
	p[25] = 3;
	let o = 29;
	if ( moving ) {
		p.writeUInt16LE( region, 26 );
		p.writeInt16LE( Math.round( x ) + distance, 28 );
		p.writeInt16LE( 11, 30 );
		p.writeInt16LE( 40, 32 );
		o = 34;
	}
	p[o] = 1;
	p.writeFloatLE( 10, o + 3 );
	p.writeFloatLE( 20, o + 7 );
	p.writeFloatLE( 1, o + 11 );
	p[o + 16] = 1;
	return { opcode: 0x30d7, payload: p };
}
function snap( region, x ) {
	const p = Buffer.alloc( 20 );
	p.writeUInt32LE( 7, 0 );
	p.writeUInt16LE( region, 4 );
	p.writeFloatLE( x, 6 );
	p.writeFloatLE( 11, 10 );
	p.writeFloatLE( 40, 14 );
	return { opcode: 0xb2f5, payload: p };
}

test("a dungeon spawn publishes the room its navigation resolves", () => {
	const calls = [], e = world( calls );
	e.receive( spawn( DUNGEON, 200 ), 0 );
	flush( e );
	assert.equal( defined( e.read( 7 ) ).dungeonBlock, WEST_ROOM );
	assert.ok( calls.length > 0 && calls.every( c => c.regionId === DUNGEON ) );
	e.dispose();
});

test("an outdoor spawn never asks for a room and publishes none", () => {
	const calls = [], e = world( calls );
	e.receive( spawn( OUTDOOR, 200 ), 0 );
	flush( e );
	assert.equal( calls.length, 0 );
	assert.equal( defined( e.read( 7 ) ).dungeonBlock, undefined );
	e.dispose();
});

test("a walking dungeon entity changes room when it crosses the door", () => {
	const e = world( [] );
	e.receive( spawn( DUNGEON, 460, { moving: true, distance: 100 } ), 0 );
	flush( e );
	assert.equal( defined( e.read( 7 ) ).dungeonBlock, WEST_ROOM );
	for ( let at = 1000; at <= 30000; at += 1000 ) {
		e.step( at );
		flush( e );
	}
	const done = defined( e.read( 7 ) );
	assert.ok( done.x >= DOOR_X, `walked to ${done.x}` );
	assert.equal( done.dungeonBlock, EAST_ROOM );
	e.dispose();
});

test("a position correction restamps the room without a movement step", () => {
	const e = world( [] );
	e.receive( spawn( DUNGEON, 200 ), 0 );
	flush( e );
	assert.equal( defined( e.read( 7 ) ).dungeonBlock, WEST_ROOM );
	e.receive( snap( DUNGEON, 600 ), 1000 );
	flush( e );
	const corrected = defined( e.read( 7 ) );
	assert.ok( corrected.x >= DOOR_X, `snapped to ${corrected.x}` );
	assert.equal( corrected.dungeonBlock, EAST_ROOM );
	e.dispose();
});

test("an unchanged room adds nothing to the journal", () => {
	const e = world( [] );
	e.receive( spawn( DUNGEON, 200 ), 0 );
	flush( e );
	e.groundSpawns();
	assert.equal( flush( e ), null );
	e.dispose();
});

function present( frameOverrides = {}, rendererOverrides = {} ) {
	const published = [];
	const renderer = {
		setCharacterAssembly() {},
		setCharacterActors( actors ) {
			published.push( ...actors );
		},
		dungeonBlockHides: ( block, activeBlock ) => activeBlock !== undefined && block !== activeBlock,
		...rendererOverrides
	};
	// The presenter's frame is wider than this test feeds it; only the fields it reads are given.
	const frame = /** @type {any} */ ({
		local: undefined,
		gameplay: { navigationBlock: WEST_ROOM },
		next: new Map( [ 1, 2, 3 ].map( gid => [ gid, { gid, model: "m" + gid } ] ) ),
		entitiesByGid: new Map( [ [ 1, { gid: 1, dungeonBlock: WEST_ROOM } ], [ 2, {
			gid: 2,
			dungeonBlock: EAST_ROOM
		} ], [ 3, { gid: 3 } ] ] ),
		seconds: 0,
		blindHeld: false,
		...frameOverrides
	});
	const bindings = /** @type {any} */ ({
		renderer,
		resources: { retainWanted() {} },
		mallPreview: { skin: () => null, step: () => [] }
	});
	const catalog = /** @type {any} */ ({ catalog: new Map(), dress: null, manifest: 0 });
	publishCharacters( frame, { displayed: new Map() }, catalog, bindings );
	return new Map( published.map( a => [ a.gid, a ] ) );
}

test("the presenter hides only actors in a block the active block does not draw", () => {
	const byGid = present();
	assert.equal( byGid.get( 1 ).pickable, undefined );
	assert.equal( byGid.get( 2 ).opacity, 0 );
	assert.equal( byGid.get( 2 ).pickable, false );
	assert.equal( byGid.get( 3 ).opacity, undefined, "an entity with no room is never culled" );

	const noHook = present( {}, { dungeonBlockHides: undefined } );
	assert.ok(
		[ ...noHook.values() ].every( a => a.pickable === undefined && a.opacity === undefined ),
		"no hook, no culling"
	);
});

test("the presenter judges against the same frame's navigation block", () => {
	const crossed = present( { gameplay: { navigationBlock: EAST_ROOM } } );
	assert.equal( crossed.get( 1 ).opacity, 0, "the west actor hides on the frame the viewer crosses east" );
	assert.equal( crossed.get( 1 ).pickable, false );
	assert.equal( crossed.get( 2 ).opacity, undefined, "the east actor appears on that same frame" );

	const noActive = present( { gameplay: { navigationBlock: undefined } } );
	assert.ok(
		[ ...noActive.values() ].every( a => a.opacity === undefined ),
		"no active block, nothing hides"
	);
});

test("the local player and its mount never hide", () => {
	const byGid = present( {
		local: { gid: 1, mountedOn: 2 },
		next: new Map( [ 1, 2, 3 ].map( gid => [ gid, { gid, model: "m" + gid } ] ) ),
		entitiesByGid: new Map( [ [ 1, { gid: 1, dungeonBlock: EAST_ROOM } ], [ 2, {
			gid: 2,
			dungeonBlock: EAST_ROOM
		} ], [ 3, { gid: 3, dungeonBlock: EAST_ROOM } ] ] )
	} );
	assert.equal( byGid.get( 1 ).opacity, undefined, "the local body stands where the camera stands" );
	assert.equal( byGid.get( 2 ).opacity, undefined, "the local mount carries the camera" );
	assert.equal( byGid.get( 3 ).opacity, 0, "a stranger in the same hidden block still hides" );
});

test("a rider is judged by its mount, including a pickOwner synthetic ride", () => {
	const byGid = present( {
		next: new Map( [
			[ 5, { gid: 5, model: "m5" } ],
			[ 7, { gid: 7, model: "m7", pickOwner: 8 } ],
			[ 10, { gid: 10, model: "m10", pickOwner: 11 } ],
			[ 13, { gid: 13, model: "m13" } ],
			[ 14, { gid: 14, model: "m14" } ]
		] ),
		entitiesByGid: new Map( [
			[ 5, { gid: 5, mountedOn: 6 } ],
			[ 6, { gid: 6, dungeonBlock: EAST_ROOM } ],
			[ 8, { gid: 8, mountedOn: 9 } ],
			[ 9, { gid: 9, dungeonBlock: EAST_ROOM } ],
			[ 11, { gid: 11, mountedOn: 12 } ],
			[ 12, { gid: 12, dungeonBlock: WEST_ROOM } ],
			[ 13, { gid: 13, mountedOn: 99, dungeonBlock: WEST_ROOM } ],
			[ 14, { gid: 14, mountedOn: 99, dungeonBlock: EAST_ROOM } ]
		] )
	} );
	assert.equal( byGid.get( 5 ).opacity, 0, "the mount's hidden block hides its rider" );
	assert.equal( byGid.get( 7 ).opacity, 0, "a linked ride is judged by the holder's mount" );
	assert.equal( byGid.get( 10 ).opacity, undefined, "a linked ride in a drawn block stays" );
	assert.equal( byGid.get( 13 ).opacity, undefined, "a gone mount falls back to the rider's own block" );
	assert.equal( byGid.get( 14 ).opacity, 0, "that fallback still hides in a hidden block" );
});

test("an actor whose holder has left keeps the block copied onto it", () => {
	const byGid = present( {
		next: new Map( [
			[ 20, { gid: 20, model: "m20", dungeonBlock: EAST_ROOM } ],
			[ 21, { gid: 21, model: "m21", dungeonBlock: WEST_ROOM } ],
			[ 22, { gid: 22, model: "m22", attachment: { gid: 99 }, dungeonBlock: EAST_ROOM } ]
		] ),
		entitiesByGid: new Map()
	} );
	assert.equal( byGid.get( 20 ).opacity, 0, "a sinking corpse is judged by its copied block" );
	assert.equal( byGid.get( 21 ).opacity, undefined, "a corpse in a drawn block stays" );
	assert.equal( byGid.get( 22 ).opacity, 0, "a detached particle tail is judged by its copied block" );
});

test("dungeon character PVS is on unless explicitly false", () => {
	for ( const value of [ undefined, null, {}, { dungeonCharacterPvs: 1 }, { dungeonCharacterPvs: "true" } ] ) {
		assert.equal( experimentalOptions( value ).dungeonCharacterPvs, true );
	}
	const off = experimentalOptions( { dungeonCharacterPvs: false } );
	assert.equal( off.dungeonCharacterPvs, false );
	assert.equal( experimentalVideo( off ).dungeonCharacterPvs, false );
	assert.equal( experimentalVideo( experimentalOptions( {} ) ).dungeonCharacterPvs, true );
	assert.equal( "dungeonRoomCulling" in experimentalOptions( { dungeonRoomCulling: false } ), false );
});
