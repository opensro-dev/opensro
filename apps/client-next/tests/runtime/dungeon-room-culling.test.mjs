/*
===========================================================================

dungeon-room-culling.test.mjs - dungeon character PVS and floor selection admission

Port-only, not native (Experimental "Dungeon character PVS", on unless
explicitly off). The simulation worker publishes each dungeon entity's
block from its navigation owner (entities.ts withBlock), and
dungeonCharacterAdmission filters out other-room and other-floor entities
before top-K character selection while exempting the local party, the
selected target, active cast combatants, and mount/rider pairs. Neither
runs outdoors or when the Experimental switch is off.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createCharacterSelection, dungeonCharacterAdmission } = await import(
	"../../src/engine/foundation/animation/character-selection.ts"
);
const { minimapPoseKey } = await import(
	"../../src/engine/foundation/gameplay/minimap-floor-key.ts"
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

const FLOOR_1 = 0, FLOOR_4 = 3;

function entityAt( gid, overrides = {} ) {
	const floor = overrides.floor ?? FLOOR_4;
	return {
		gid,
		kind: "monster",
		refObjId: 2029,
		regionId: DUNGEON,
		x: 100 + gid * 20,
		y: floor * 50,
		z: 100,
		heading: 0,
		dungeonBlock: WEST_ROOM,
		...overrides
	};
}

function selectDungeon( entities, gameplayOverrides = {}, options = {} ) {
	const anchor = options.anchor ?? { regionId: DUNGEON, x: 100, y: 0, z: 100, angle: 0 };
	const minimapFloors = {};
	for ( const e of entities ) {
		minimapFloors[minimapPoseKey( e )] = e.floor ?? FLOOR_4;
	}
	const gameplay = /** @type {any} */ ({
		localGid: 1,
		pose: anchor,
		navigationBlock: WEST_ROOM,
		navigationFloor: FLOOR_4,
		minimapFloors,
		target: 0,
		casts: [],
		vitals: [],
		inventory: [],
		questMarkers: [],
		attachedEffects: [],
		...gameplayOverrides
	});
	const dungeonBlockHides = options.dungeonBlockHides === null ?
		undefined :
		options.dungeonBlockHides ?? (( block, activeBlock ) => activeBlock !== undefined && block !== activeBlock);
	const admit = dungeonCharacterAdmission( {
		entities,
		anchor,
		gameplay,
		localGid: options.localGid ?? gameplay.localGid,
		mountedOn: options.mountedOn,
		dungeonCharacterPvs: options.dungeonCharacterPvs ?? true,
		dungeonBlockHides
	} );
	const selection = createCharacterSelection( options.limit ?? 375 );
	return selection.select( entities, anchor, options.localGid ?? gameplay.localGid, options.mountedOn, admit )
		.map( e => e.gid );
}

test("selection rejects other-room and other-floor entities before top-K ranking", () => {
	const entities = [
		entityAt( 1, { kind: "local-player", x: 100, dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		// Nearer in XZ, but in another room or on another floor: must not steal budget slots.
		entityAt( 2, { x: 101, dungeonBlock: EAST_ROOM, floor: FLOOR_4 } ),
		entityAt( 3, { x: 102, dungeonBlock: WEST_ROOM, floor: FLOOR_1 } ),
		// Farther in XZ, but in the active room and on the active floor: must be selected.
		entityAt( 4, { x: 400, dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		// An entity with no resolved room still passes when on the same floor.
		entityAt( 5, { x: 450, dungeonBlock: undefined, floor: FLOOR_4 } )
	];
	assert.deepEqual( selectDungeon( entities, {}, { limit: 3 } ), [ 1, 4, 5 ] );

	const crossedEast = selectDungeon(
		entities,
		{ navigationBlock: EAST_ROOM },
		{ limit: 3 }
	);
	assert.deepEqual( crossedEast, [ 1, 2, 5 ], "crossing into the east room admits east-room entities on that frame" );

	const beforeMinimapRoundTrip = selectDungeon(
		entities,
		{ minimapFloors: {} },
		{ limit: 3 }
	);
	assert.deepEqual(
		beforeMinimapRoundTrip,
		[ 1, 3, 4 ],
		"before minimapFloors resolves a spawn's cell, synchronous room PVS still rejects other rooms without dropping same-room spawns"
	);
});

test("selection admits everything when dungeonCharacterPvs is off or outdoors", () => {
	const entities = [
		entityAt( 1, { kind: "local-player", x: 100, dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		entityAt( 2, { x: 101, dungeonBlock: EAST_ROOM, floor: FLOOR_4 } ),
		entityAt( 3, { x: 102, dungeonBlock: WEST_ROOM, floor: FLOOR_1 } )
	];
	assert.deepEqual(
		selectDungeon( entities, {}, { dungeonCharacterPvs: false } ),
		[ 1, 2, 3 ],
		"Experimental off matches native selection"
	);
	assert.deepEqual(
		selectDungeon(
			entities.map( e => ({ ...e, regionId: OUTDOOR }) ),
			{},
			{ anchor: { regionId: OUTDOOR, x: 100, y: 0, z: 100, angle: 0 } }
		),
		[ 1, 2, 3 ],
		"outdoors never runs dungeon room or floor admission"
	);
});

test("local player, mount, pets, target, and active cast combatants are exempt in other rooms and floors", () => {
	const entities = [
		entityAt( 1, { kind: "local-player", mountedOn: 2, dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 2, { kind: "cos", dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 3, { kind: "cos", dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 4, { kind: "cos", ownerGid: 1, dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 5, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 6, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 7, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 8, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 9, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 10, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 11, { dungeonBlock: EAST_ROOM, floor: FLOOR_1 } )
	];
	const selected = selectDungeon(
		entities,
		{
			activeCos: { gid: 3, refObjId: 2000, hp: 100 },
			target: 5,
			selectionDecal: { kind: "target", gid: 6 },
			casts: [
				// Local player casting at 7 and hitting 8 in results.
				{
					token: 1,
					caster: 1,
					skill: 100,
					target: 7,
					results: [ { target: 8, impacts: [] } ],
					damage: 0,
					fatal: false
				},
				// Monster 9 casting at local pet 3.
				{ token: 2, caster: 9, skill: 101, target: 3, damage: 0, fatal: false },
				// Cancelled cast targeting 10: no longer active, so 10 is not exempt.
				{ token: 3, caster: 1, skill: 102, target: 10, cancelledAtMs: 500, damage: 0, fatal: false }
			],
			castPrediction: {
				token: 4,
				caster: 1,
				skill: 103,
				target: 11,
				damage: 0,
				fatal: false
			}
		},
		{ mountedOn: 2 }
	);
	assert.deepEqual( selected, [ 1, 2, 3, 4, 5, 6, 7, 8, 9, 11 ] );
});

test("a rider is judged by its mount and mount/rider pairs share combat exemptions", () => {
	const entities = [
		entityAt( 1, { kind: "local-player", dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		// Rider 5 on hidden mount 6: both rejected.
		entityAt( 5, { kind: "player", mountedOn: 6, dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		entityAt( 6, { kind: "cos", dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		// Rider 7 on visible mount 8: both admitted even if rider's own record trails in EAST_ROOM.
		entityAt( 7, { kind: "player", mountedOn: 8, dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 8, { kind: "cos", dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		// Rider 9 is targeted while riding hidden mount 10: both admitted.
		entityAt( 9, { kind: "player", mountedOn: 10, dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		entityAt( 10, { kind: "cos", dungeonBlock: EAST_ROOM, floor: FLOOR_1 } ),
		// Rider 13 whose mount 99 is gone falls back to its own room/floor.
		entityAt( 13, { kind: "player", mountedOn: 99, dungeonBlock: WEST_ROOM, floor: FLOOR_4 } ),
		entityAt( 14, { kind: "player", mountedOn: 99, dungeonBlock: EAST_ROOM, floor: FLOOR_4 } )
	];
	assert.deepEqual( selectDungeon( entities, { target: 9 } ), [ 1, 7, 8, 9, 10, 13 ] );
});

test("publishCharacters publishes selected actors without a second room-culling drop", () => {
	const published = [];
	const frame = /** @type {any} */ ({
		local: undefined,
		gameplay: { navigationBlock: WEST_ROOM },
		next: new Map( [ 1, 2 ].map( gid => [ gid, { gid, model: "m" + gid, blindable: gid === 2 } ] ) ),
		seconds: 0,
		blindHeld: true
	});
	const bindings = /** @type {any} */ ({
		renderer: {
			setCharacterAssembly() {},
			setCharacterActors( actors ) {
				published.push( ...actors );
			}
		},
		resources: { retainWanted() {} },
		mallPreview: { skin: () => null, step: () => [] }
	});
	const catalog = /** @type {any} */ ({ catalog: new Map(), dress: null, manifest: 0 });
	publishCharacters( frame, { displayed: new Map() }, catalog, bindings );
	const byGid = new Map( published.map( a => [ a.gid, a ] ) );
	assert.equal( byGid.get( 1 ).opacity, undefined );
	assert.equal( byGid.get( 2 ).opacity, 0 );
	assert.equal( byGid.get( 2 ).pickable, false );
});

test("publishCharacters drops the /hideme local player's body", () => {
	const published = [];
	const frame = /** @type {any} */ ({
		local: { gid: 1, refObjId: 9, debugHidden: true },
		gameplay: undefined,
		next: new Map( [ 1, 2 ].map( gid => [ gid, { gid, model: "m" + gid } ] ) ),
		seconds: 0,
		blindHeld: false
	});
	const bindings = /** @type {any} */ ({
		renderer: {
			setCharacterAssembly() {},
			setCharacterActors( actors ) {
				published.push( ...actors );
			}
		},
		resources: { retainWanted() {} },
		mallPreview: { skin: () => null, step: () => [] }
	});
	const catalog = /** @type {any} */ ({ catalog: new Map(), dress: null, manifest: 0 });
	publishCharacters( frame, { displayed: new Map() }, catalog, bindings );
	const byGid = new Map( published.map( a => [ a.gid, a ] ) );
	assert.equal( byGid.get( 1 ).opacity, 0 );
	assert.equal( byGid.get( 1 ).pickable, false );
	assert.equal( byGid.get( 2 ).opacity, undefined );
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
