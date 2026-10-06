/*
===========================================================================

minimap.test.mjs - tests for minimap-markers.ts, minimap-tiles.ts,
minimap-catalog.ts, minimap-floor.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
const { minimapEntityIcon, minimapMarkers, minimapEdge, minimapHunting, minimapOffset } = await import(
	"../../src/engine/foundation/ui/minimap-markers.ts"
);
const { minimapTiles, minimapDungeons, minimapArt } = await import( "../../src/engine/foundation/ui/minimap-tiles.ts" );
const { minimapNpcPositions } = await import( "../../src/engine/foundation/ui/minimap-catalog.ts" );
const { minimapSameFloor, minimapPoseKey } = await import( "../../src/engine/foundation/ui/minimap-floor.ts" );
const { academyPacket } = await import( "../../src/engine/foundation/gameplay/academy.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const pose = { regionId: 257, x: 800, y: 10, z: 800, angle: 0 }, game = { localGid: 1, pose };
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ], u16 = n => [ n & 255, n >>> 8 & 255 ];
const entity = ( gid, extra = {} ) => ({ ...pose, gid, heading: 0, kind: "monster", rarity: 0, ...extra });
test("NPC catalog preserves signed dungeon sectors, float32 coordinates and first duplicate", () => {
	const rows = minimapNpcPositions( {
		format: "sro-npcpos",
		version: 1,
		rows: [ "7\t-32767\t605.84003\t-9.9799995\t202.72", "7\t257\t0\t0\t0" ]
	} );
	assert.deepEqual( rows.get( 7 ), {
		regionId: 32769,
		x: Math.fround( 605.84003 ),
		y: Math.fround( -9.9799995 ),
		z: Math.fround( 202.72 ),
		angle: 0
	} );
});
test("all rarity bytes use equality 3; transparent state removes every ordinary actor class", () => {
	for ( let rarity = 0; rarity < 256; rarity++ ) {
		assert.ok(
			defined( minimapEntityIcon( entity( 2, { rarity } ) ) ).endsWith(
				rarity === 3 ? "_unique.png" : "_monster.png"
			)
		);
	}
	for ( const [kind, icon] of [ [ "cos", "animal" ], [ "npc", "npc" ], [ "player", "otherplayer" ] ] ) {
		assert.ok( defined( minimapEntityIcon( entity( 2, { kind } ) ) ).endsWith( "_" + icon + ".png" ) );
		assert.equal( minimapEntityIcon( entity( 2, { kind, appearanceState: [ 1, 0, 4 ] } ) ), null );
	}
	assert.equal( minimapEntityIcon( entity( 2, { kind: "ground-item" } ) ), null );
});
test("3122 status writer crosses the entity journal and changes minimap eligibility without despawn", async () => {
	const { createEntities } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
	);
	const fixture = JSON.parse(
			await readFile( "../server/internal/game/enterworld/testdata/monster_spawn_fixture.json", "utf8" )
		),
		owner = createEntities();
	const flush = () => {
		const b = owner.take();
		if ( b ) owner.ack( b.sequence );
		return b;
	};
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: fixture.refObjSnapshot,
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 25256, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	flush();
	for ( const p of fixture.packets ) {
		owner.receive( { opcode: p.opcode, payload: Buffer.from( p.payloadHex, "hex" ) } );
	}
	const original = defined( defined( flush() ).events.find( e => e.kind === "spawn" ) ).entity;
	assert.ok( minimapEntityIcon( original ) );
	for ( const status of [ 4, 0 ] ) {
		owner.receive( { opcode: 0x3122, payload: Uint8Array.from( [ ...u32( original.gid ), 4, status ] ) } );
		const entity = defined( defined( flush() ).events.find( e => e.kind === "state" ) ).entity;
		assert.equal( minimapEntityIcon( entity ) === null, status === 4 );
		assert.equal( owner.count(), 1 );
	}
	assert.notEqual( defined( original.appearanceState )[2], 4 );
	owner.dispose();
});
test("registry has no 512 prefix; strict circular cutoff excludes corners and equality", () => {
	const rows = Array.from( { length: 600 }, ( _, i ) => entity( i + 2 ) );
	rows[599] = { ...rows[599], rarity: 3 };
	assert.equal( minimapMarkers( pose, game, rows, 1920, 55 ).length, 600 );
	assert.ok( defined( minimapMarkers( pose, game, rows, 1920, 55 ).at( -1 ) ).path.endsWith( "_unique.png" ) );
	assert.equal(
		minimapMarkers( pose, game, [ entity( 2, { x: 847 } ), entity( 3, { x: 840, z: 840 } ) ], 1920, 55 ).length,
		0
	);
});
test("party and academy retain separate self, offline, world, order and edge rules", () => {
	const social = {
		self: 1,
		localName: "Me",
		members: [ { id: 1, name: "Me", war: 65537, region: 257, x: 800, y: 10, z: 800 }, {
			id: 2,
			name: "Friend",
			war: 65537,
			region: 257,
			x: 850,
			y: 10,
			z: 800
		} ]
	};
	const academy = {
		member: true,
		members: [ { id: 9, name: "Offline", offline: true, war: 65537, ...pose }, {
			id: 3,
			name: "Student",
			offline: false,
			war: 65537,
			...pose,
			x: 850
		} ]
	};
	const markers = minimapMarkers( pose, { ...game, social, academy }, [], 1920, 55 );
	assert.deepEqual( markers.map( m => [ m.size, m.x, m.path.split( "_" ).at( -1 ) ] ), [ [
		32,
		47,
		"apprenticeshiparrow.png"
	], [ 16, 47, "partyarrow.png" ] ] );
	const live = entity( 8, { kind: "player", name: "Friend", x: 810 } );
	assert.equal( defined( minimapMarkers( pose, { ...game, social }, [ live ], 1920, 55 ).at( -1 ) ).x, 10 );
	assert.equal( minimapMarkers( pose, { ...game, social, fortress: { worldId: 2 } }, [], 1920, 55 ).length, 0 );
	for ( const kind of [ "party", "apprenticeship", "quest" ] ) {
		assert.equal( minimapEdge( [ 0, 0 ], kind ).rotation, 0 );
		assert.ok( minimapEdge( [ 47, 0 ], kind ).path.endsWith( "arrow.png" ) );
		assert.equal( minimapEdge( [ 46, 0 ], kind ).size, kind === "quest" ? 16 : 8 );
	}
});
test("academy location and online lifecycle packets preserve detached snapshots and signed coordinates", () => {
	let state = { member: true, localMemberId: 1, members: [ { id: 2, name: "Peer", offline: true } ], rows: [] };
	const old = state;
	state = academyPacket( state, {
		opcode: 0x3ac5,
		payload: Uint8Array.from( [
			13,
			...u32( 2 ),
			1,
			0,
			65,
			...u32( 65537 ),
			...u16( 32769 ),
			...u16( -25 ),
			...u16( 300 ),
			...u16( -80 )
		] )
	} );
	assert.deepEqual( [ state.members[0].x, state.members[0].y, state.members[0].z ], [ -25, 300, -80 ] );
	assert.equal( old.members[0].x, undefined );
	state = academyPacket( state, { opcode: 0x3ac5, payload: Uint8Array.from( [ 5, ...u32( 2 ), 1, 0 ] ) } );
	assert.equal( state.members[0].offline, false );
	state = academyPacket( state, { opcode: 0x3ac5, payload: Uint8Array.from( [ 4, ...u32( 2 ), 0 ] ) } );
	assert.equal( state.members.length, 0 );
});
test("B5ED creates hunting records; duplicate owner, unspawned movement and ordered teardown retain native ownership", () => {
	let source = entity( 8 );
	const c = createCombat( gid => gid === 8 ? source : undefined );
	c.references( [ { id: 4, status: false, effectRider: false, huntingPoint: true, stealthDuration: true } ] );
	c.seed( 1, {} );
	c.cooldownReferences( 1, [] );
	const start = ( token, owner ) =>
		Uint8Array.from( [ ...u32( 4 ), ...u32( token ), ...u32( owner ), 1, 0, 65, ...u32( 10000 ) ] );
	c.receive( 0xb5ed, start( 50, 8 ), 0 );
	const previous = c.state().huntingPoints;
	c.receive( 0xb5ed, start( 51, 8 ), 1 );
	assert.equal( c.state().huntingPoints[0].token, 50 );
	source = undefined;
	const p = new Uint8Array( 20 ), v = new DataView( p.buffer );
	v.setUint16( 0, 257, true );
	v.setFloat32( 2, 900, true );
	v.setFloat32( 6, 10, true );
	v.setFloat32( 10, 800, true );
	v.setUint16( 14, 16384, true );
	v.setUint32( 16, 8, true );
	c.receive( 0x30e3, p, 2 );
	assert.equal( previous[0].x, 800 );
	assert.equal( c.state().huntingPoints[0].x, 900 );
	assert.equal( minimapHunting( pose, c.state(), [], 1920 )[0].x, 47 );
	c.receive( 0xb5ed, start( 50, 9 ), 3 );
	assert.equal( c.state().huntingPoints[1].regionId, 0 );
	c.receive( 0xb6a0, Uint8Array.from( [ 1, ...u32( 50 ) ] ), 4 );
	assert.deepEqual( c.state().huntingPoints.map( p => p.gid ), [ 9 ] );
	c.clear();
	assert.deepEqual( c.state().huntingPoints, [] );
});
test("B5ED linked source retains its subject and authored duration without creating a hunting point", () => {
	const c = createCombat();
	c.references( [ {
		id: 7246,
		status: false,
		effectRider: false,
		huntingPoint: false,
		stealthDuration: false,
		effectDurationMs: 1800000
	} ] );
	c.cooldownReferences( 1, [] );
	const p = Uint8Array.from( [ ...u32( 7246 ), ...u32( 50 ), ...u32( 8 ), 4, 0, 80, 101, 101, 114 ] );
	c.receive( 0xb5ed, p, 500 );
	const previous = c.state().attachedEffects;
	assert.deepEqual( previous[0].subject, { gid: 8, name: "Peer" } );
	assert.equal( previous[0].gid, 1 );
	assert.equal( previous[0].remainingMs, 1800000 );
	assert.equal( previous[0].receivedAtMs, 500 );
	assert.deepEqual( c.state().huntingPoints, [] );
	c.receive( 0xb6a0, Uint8Array.from( [ 1, ...u32( 50 ) ] ), 600 );
	assert.equal( c.state().attachedEffects.length, 0 );
	assert.equal( previous.length, 1 );
	assert.throws( () => c.receive( 0xb5ed, Uint8Array.from( [ ...p, ...u32( 100 ) ] ), 700 ), /length/ );
});
test("dungeon navigation owns floor identity, invalidates admission and clears queries", () => {
	const mesh = y => ({
		vertices: Float32Array.from( [ 0, y, 0, 100, y, 0, 100, y, 100, 0, y, 100 ] ),
		cells: Uint16Array.from( [ 0, 1, 2, 0, 2, 3 ] ),
		edges: Uint32Array.from( [ 1, 2, 0, 65535, 3, 0, 0, 2, 0, 1, 4, 1 ] ),
		bounds: [ 0, y, 0, 100, y, 100 ],
		passThrough: false
	});
	const m = createMovement( () => {} ),
		p = { regionId: 32769, x: 10, y: 10, z: 50, angle: 0 },
		other = { ...p, y: 100 };
	m.seed( p );
	m.minimapFloors( [ p, other ] );
	assert.equal( m.state().navigationFloor, undefined );
	m.navigation( 32769, {
		regionId: 32769,
		complete: true,
		objects: [ { x: 0, y: 0, z: 0, yaw: 0, mesh: mesh( 10 ), block: 0, floor: 0 }, {
			x: 0,
			y: 0,
			z: 0,
			yaw: 0,
			mesh: mesh( 100 ),
			block: 1,
			floor: 1
		} ]
	} );
	const s = m.state();
	assert.equal( s.navigationFloor, 0 );
	assert.equal( s.minimapFloors[minimapPoseKey( other )], 1 );
	assert.equal( minimapSameFloor( p, other, s ), false );
	assert.equal( minimapSameFloor( p, p, s ), true );
	m.clear();
	assert.equal( m.state().navigationFloor, undefined );
	assert.deepEqual( m.state().minimapFloors, {} );
});
test("dungeon tiles use floor-specific art and negative logical coordinate sectors; absent art stays absent", () => {
	const floor = {
		floorIndex: 1,
		floorLabel: "F2",
		directory: "donwhang",
		prefix: "dh_a01_floor02",
		tiles: [ "127x127", "128x127" ]
	};
	const map = minimapDungeons( {
		format: "sro-mission-dungeon-minimap-manifest",
		version: 1,
		dungeons: [ { sectorId: 32769, floors: [ floor ] } ]
	} );
	assert.equal( defined( map.get( 32769 ) )[0], floor );
	const p = { ...pose, regionId: 32769, x: -10, z: -10 };
	assert.equal( minimapTiles( p, 160 ).length, 0 );
	const art = new Set(
		floor.tiles.map( t => "/assets/images/media_extracted/minimap_d/donwhang/dh_a01_floor02_" + t + ".png" )
	);
	const tiles = minimapTiles( p, 160, floor, art );
	assert.equal( tiles.length, 2 );
	assert.ok( tiles.every( t => t.path.includes( "/donwhang/dh_a01_floor02_" ) ) );
	assert.equal( minimapTiles( pose, 160 ).length, 0 );
});
test("hunting has no roster floor gate; the region adapter uses zero offset for either dungeon", () => {
	const target = { ...pose, regionId: 32769, x: 900 };
	assert.deepEqual( minimapOffset( pose, target, 1920 ), [ 100, -0 ] );
	assert.equal( minimapSameFloor( pose, target, game ), false );
	assert.equal( minimapHunting( pose, { huntingPoints: [ { ...target, gid: 8, token: 4 } ] }, [], 1920 )[0].x, 47 );
});

test("unique marker keeps its 12px artwork readable alongside 8px ordinary dots", () => {
	const p = { regionId: 25256, x: 960, y: 0, z: 960, angle: 0 };
	const markers = minimapMarkers(
		p,
		null,
		[ { ...p, gid: 1, kind: "monster", rarity: 3, heading: 0 }, {
			...p,
			gid: 2,
			kind: "monster",
			rarity: 0,
			heading: 0
		} ],
		128,
		64
	);
	assert.deepEqual( markers.map( m => m.size ), [ 12, 8 ] );
});

test("all outdoor region grids request only retail artwork, including every edge and hole", async () => {
	const catalog = JSON.parse(
			await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/mission-dungeon-minimap.json", "utf8" )
		),
		art = minimapArt( catalog ),
		seen = new Set();
	for ( let regionId = 0; regionId < 32768; regionId++ ) {
		for ( const tile of minimapTiles( { ...pose, regionId }, 160, undefined, art ) ) {
			assert.ok( art.has( tile.path.toLowerCase() ) );
			seen.add( tile.path.toLowerCase() );
		}
	}
	const outdoor = [ ...art ].filter( p => p.includes( "/minimap/" ) );
	assert.equal( seen.size, outdoor.length );
	for ( const p of outdoor ) assert.ok( seen.has( p ) );
	const edge = minimapTiles( { ...pose, regionId: (110 << 8) | 77 }, 160, undefined, art );
	assert.ok( edge.some( t => t.path.endsWith( "/77x110.png" ) ) );
	assert.ok( !edge.some( t => t.path.endsWith( "/77x111.png" ) ) );
	assert.throws( () => minimapArt( { ...catalog, tilePaths: undefined } ), /catalog/ );
	assert.throws(
		() => minimapArt( { ...catalog, tilePaths: [ catalog.tilePaths[0], catalog.tilePaths[0] ] } ),
		/path/
	);
	assert.throws(
		() => minimapArt( { ...catalog, tilePaths: [ "/assets/images/Media_extracted/minimap/../bad.png" ] } ),
		/path/
	);
});

test("minimap catalog commits complete coverage atomically and disposes pending work", async () => {
	const { createMinimapResources } = await import( "../../src/engine/runtime/ui/hud/minimap.ts" );
	let id = 0, result = null;
	const cancelled = [];
	const assets = {
		available: () => 4,
		request: () => ++id,
		take: () => {
			const r = result;
			result = null;
			return r;
		},
		cancel: id => cancelled.push( id )
	};
	const owner = createMinimapResources( assets, "https://fixture.invalid" );
	owner.step( false, true );
	assert.equal( owner.art(), undefined );
	const catalog = await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/mission-dungeon-minimap.json" );
	result = { kind: "bytes", buffer: Uint8Array.from( catalog ).buffer };
	assert.equal( owner.step( false, true ), true );
	assert.ok( defined( owner.art() ).size > 0 );
	owner.dispose();
	assert.equal( owner.art(), undefined );
	const other = createMinimapResources( assets, "https://fixture.invalid" );
	other.step( false, true );
	other.dispose();
	assert.deepEqual( cancelled, [ 2 ] );
});
