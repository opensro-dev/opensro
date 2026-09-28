/*
===========================================================================

gameplay.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import fc from "fast-check";
import { root } from "../../tools/project.mjs";
/*
================
load
================
*/
async function load( name ) {
	return import(
		sourceFileUrl( path.join( root, "src/engine/runtime/simulation/worker/session/world/gameplay", name + ".ts" ) )
			.href
	);
}
const { createMovement } = await load( "movement/movement" ),
	{ createNavigation } = await load( "movement/navigation/navigation" ),
	{ createInventory } = await load( "inventory/inventory" ),
	{ createCombat } = await load( "combat/combat" ),
	{ createTargeting } = await load( "targeting/targeting" );
const pose = { regionId: 0x6b4f, x: 60, y: 10, z: 100, angle: 0 };

test("level-up packets publish visual feedback for peers and sound only for the local player", async () => {
	const { createGameplay } = await load( "gameplay" ),
		events = [],
		sounds = [],
		game = createGameplay( () => {}, s => sounds.push( s ), e => events.push( e ) );
	game.seed( { ...pose, gid: 7, heading: 0 } );
	assert.deepEqual( events.splice( 0 ), [ { kind: "orb-gauge", value: 0 }, { kind: "orb-clear" } ] );
	for ( const gid of [ 7, 8 ] ) {
		assert.equal( game.receive( { opcode: 0x36b0, payload: Uint8Array.of( gid, 0, 0, 0 ) }, 100 ), true );
	}
	assert.deepEqual( events, [ { kind: "level-up", gid: 7 }, { kind: "level-up", gid: 8 } ] );
	assert.deepEqual( sounds, [ "SND_LEVUP" ] );
	assert.throws( () => game.receive( { opcode: 0x36b0, payload: Uint8Array.of( 7 ) }, 101 ), /level-up/ );
	assert.equal( events.length, 2 );
	game.dispose();
});
test("seated ground and entity clicks stand without issuing or predicting travel", async () => {
	const { createGameplay } = await load( "gameplay" );
	for (
		const command of [
			{ kind: "move", destination: { ...pose, x: 300 } },
			{ kind: "ground-move", query: {} },
			{ kind: "select", gid: 8 },
			{ kind: "attack", gid: 8 },
			{ kind: "pickup", gid: 8 }
		]
	) {
		const sent = [],
			game = createGameplay( f => sent.push( f ) ),
			local = { ...pose, gid: 7, heading: 0, movementMode: 4, appearanceState: [ 1, 0, 0 ] };
		game.bootstrap( { simulationProtocolVersion: 1 } );
		game.seed( local );
		game.command( command, 100, undefined, local );
		assert.deepEqual( sent, [ { opcode: 0x7017, payload: Uint8Array.of( 4 ) } ] );
		assert.equal( game.take().pose.x, pose.x );
		game.dispose();
	}
});
const { createEntityMotion } = await load( "../entities/motion/motion" );

test("live speed changes retime local and peer travel from the current position and reset on entry", () => {
	const packet = Buffer.alloc( 14 );
	packet.writeUInt32LE( 7 );
	packet[4] = 1;
	packet.writeUInt16LE( pose.regionId, 5 );
	packet.writeInt16LE( 260, 7 );
	packet.writeInt16LE( 10, 9 );
	packet.writeInt16LE( 100, 11 );
	const local = createMovement( () => {} );
	local.seed( pose );
	local.native( packet, 0, 7 );
	local.speeds( 40, 100, 1000 );
	assert.equal( local.state().pose.x, 110 );
	local.step( 2000 );
	assert.equal( local.state().pose.x, 210 );
	local.speeds( 20, 50, 2000 );
	local.step( 2500 );
	assert.equal( local.state().pose.x, 235 );
	local.clear();
	local.seed( pose );
	local.native( packet, 0, 7 );
	local.step( 1000 );
	assert.equal( local.state().pose.x, 110 );
	const remote = createEntityMotion(),
		entity = { ...pose, gid: 7, heading: 0, movementMode: 3, walkSpeed: 20, runSpeed: 50 };
	remote.receive( packet, entity, 0 );
	const faster = { ...entity, walkSpeed: 40, runSpeed: 100 };
	assert.equal( remote.speeds( entity, faster, 1000 ).x, 110 );
	assert.equal( remote.step( 2000 )[0].x, 210 );
	assert.equal( remote.speeds( faster, entity, 2000 ).x, 210 );
	assert.equal( remote.step( 2500 )[0].x, 235 );
});

test("live speed changes preserve combat displacement timing", () => {
	const local = createMovement( () => {} );
	local.seed( pose );
	const command = { kind: 8, gid: 7, token: 99, destination: { ...pose, x: 1060 } };
	local.displace( command, 0 );
	local.step( 500 );
	const before = local.state().pose.x;
	assert.equal( before, 310 );
	assert.equal( local.state().moving, true );
	local.speeds( 40, 100, 500 );
	assert.equal( local.state().pose.x, before );
	const control = createMovement( () => {} );
	control.seed( pose );
	control.displace( command, 0 );
	local.step( 1000 );
	control.step( 1000 );
	assert.deepEqual( local.state().pose, control.state().pose );
});

test("376F updates local entity channels and live movement through the production world owner", async () => {
	const { createWorldCore } = await load( "../core" ), core = createWorldCore( () => {} );
	const drain = () => {
		const b = core.take();
		if ( b ) core.ack( b.sequence );
		return b?.events ?? [];
	};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		character: { name: "fixture" },
		localPlayerEntry: { modelRef: 1907, startProfile: pose }
	} );
	drain();
	core.receive( { opcode: 0x32a6, payload: Uint8Array.of( 7, 0, 0, 0, 0, 0, 0, 0 ) }, 0 );
	drain();
	const move = Buffer.alloc( 14 );
	move.writeUInt32LE( 7 );
	move[4] = 1;
	move.writeUInt16LE( pose.regionId, 5 );
	move.writeInt16LE( 260, 7 );
	move.writeInt16LE( 10, 9 );
	move.writeInt16LE( 100, 11 );
	core.receive( { opcode: 0xb738, payload: move }, 0 );
	core.step( 500 );
	drain();
	const speed = Buffer.from( "07000000000020420000c842", "hex" ); // GID 7, walk 40, run 100
	core.receive( { opcode: 0x376f, payload: speed }, 1000 );
	core.step( 2000 );
	const events = drain(),
		state = events.find( e => e.kind === "state" && e.entity.gid === 7 ),
		game = events.findLast( e => e.kind === "gameplay" );
	assert.equal( state.entity.walkSpeed, 40 );
	assert.equal( state.entity.runSpeed, 100 );
	assert.equal( game.state.pose.x, 210 );
	for ( const bad of [ speed.subarray( 0, 11 ), Buffer.concat( [ speed, Buffer.of( 0 ) ] ) ] ) {
		assert.throws( () => core.receive( { opcode: 0x376f, payload: bad }, 2001 ), /speed channels/ );
	}
	const invalid = Buffer.from( speed );
	invalid.writeFloatLE( NaN, 4 );
	assert.throws( () => core.receive( { opcode: 0x376f, payload: invalid }, 2001 ), /speed channels/ );
	core.dispose();
});
test("rebirth sends the native choice once, gates level and life, and releases only on local revival", async () => {
	const { createGameplay } = await load( "gameplay" ), sent = [], game = createGameplay( f => sent.push( f ) );
	const local = { ...pose, gid: 7, heading: 0, appearanceState: [ 2, 0, 0 ] };
	game.bootstrap( { character: { level: 10 } } );
	game.seed( local );
	assert.equal(
		game.command( { kind: "rebirth", choice: 2 }, 0, undefined, { ...local, appearanceState: [ 1, 0, 0 ] } ),
		null
	);
	game.command( { kind: "rebirth", choice: 2 }, 1, undefined, local );
	assert.deepEqual( sent, [ { opcode: 0x32dc, payload: Uint8Array.of( 2 ) } ] );
	assert.equal( game.take().rebirthPending, true );
	game.command( { kind: "rebirth", choice: 1 }, 2, undefined, local );
	assert.equal( sent.length, 1 );
	game.receive( { opcode: 0x3122, payload: Uint8Array.of( 8, 0, 0, 0, 0, 1 ) }, 3 );
	game.command( { kind: "rebirth", choice: 1 }, 4, undefined, local );
	assert.equal( sent.length, 1 );
	game.receive( { opcode: 0x3122, payload: Uint8Array.of( 7, 0, 0, 0, 0, 1 ) }, 5 );
	assert.equal( game.take().rebirthPending, false );
	game.bootstrap( { character: { level: 11 } } );
	game.seed( local );
	assert.equal( game.command( { kind: "rebirth", choice: 2 }, 6, undefined, local ), null );
	game.command( { kind: "rebirth", choice: 1 }, 7, undefined, local );
	assert.equal( sent.length, 2 );
	game.resetWorld();
	assert.equal( game.take().rebirthPending, false );
	game.dispose();
});
test("inventory icons survive baseline, reference replacement, item movement and reset", () => {
	const owner = createInventory( () => {} );
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c, icon: "item/etc/hp_potion_01.ddj" } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	assert.equal( owner.state().inventory[0].icon, "item/etc/hp_potion_01.ddj" );
	owner.references( [ { refObjId: 1, typeFlags: 0x6c, name: "Potion", icon: "item/etc/hp_potion_02.ddj" } ] );
	assert.equal( owner.state().inventory[0].icon, "item/etc/hp_potion_02.ddj" );
	owner.move( 13, 14, 10, 0 );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0, 13, 14, 10, 0, 0 ) );
	assert.equal( owner.state().inventory[0].icon, "item/etc/hp_potion_02.ddj" );
	owner.clear();
	owner.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 1 ) } ]
	} );
	assert.equal( owner.state().inventory[0].icon, undefined );
});
test("remote movement uses spawn gait, visits active entities only and stops after removal", () => {
	const m = createEntityMotion(),
		entity = {
			...pose,
			gid: 7,
			refObjId: 1933,
			kind: "monster",
			name: "fixture",
			heading: 0,
			walkSpeed: 8,
			runSpeed: 22,
			movementMode: 2
		};
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( 7 );
	p[4] = 1;
	p.writeUInt16LE( pose.regionId, 5 );
	p.writeInt16LE( 100, 7 );
	p.writeInt16LE( 10, 9 );
	p.writeInt16LE( 100, 11 );
	assert.deepEqual( m.step( 0 ), [] );
	m.receive( p, entity, 0 );
	assert.equal( m.step( 1000 )[0].x, 68 );
	assert.equal( m.step( 5000 )[0].x, 100 );
	assert.deepEqual( m.step( 5016 ), [] );
	m.receive( p, entity, 6000 );
	m.remove( 7 );
	assert.deepEqual( m.step( 6016 ), [] );
});
/*
================
receipt
================
*/
function receipt( id, to, accepted = true ) {
	return Buffer.from(
		JSON.stringify( {
			v: 1,
			id,
			gid: 100007,
			accepted,
			error: accepted ? undefined : "blocked",
			serverTimeMs: 1000,
			world: { spawn: to, moveSegment: { from: pose, startedAtMs: 1000, arrivesAtMs: 2000 } }
		} )
	);
}
/*
================
bundle
================
*/
function bundle( blockedIndices = [] ) {
	const blocked = Buffer.alloc( 9216 );
	for ( const i of blockedIndices ) blocked[i] = 1;
	return {
		navmesh: {
			regionSize: 1920,
			tileSize: 20,
			tilesPerAxis: 96,
			regions: [ {
				dx: 0,
				dz: 0,
				blockedTiles: blocked.toString( "base64" ),
				tileCellIds: Buffer.alloc( 36864 ).toString( "base64" ),
				cells: { count: 1 },
				objects: []
			} ]
		}
	};
}
test("published tile collision stops at wall and prevents corner squeezing", () => {
	const nav = createNavigation();
	nav.install( pose.regionId, bundle( [ 5 * 96 + 4 ] ) );
	const hit = nav.clip( pose, { ...pose, x: 200 } );
	assert.ok( hit.x < 80 && hit.x > 79.9 );
	assert.equal( hit.z, 100 );
	nav.clear();
	nav.install( pose.regionId, bundle( [ 2 * 96 + 3 ] ) );
	const corner = nav.clip( { ...pose, x: 50, z: 50 }, { ...pose, x: 70, z: 70 } );
	assert.ok( corner.x < 60 && corner.z < 60 );
	assert.equal( nav.clip( { ...pose, regionId: 1 }, { ...pose, regionId: 1, x: 80 } ), null );
});
test("navigation admission is atomic and missing object coverage never pretends clear", () => {
	const nav = createNavigation();
	nav.install( pose.regionId, bundle() );
	const bad = bundle();
	bad.navmesh.regions[0].tileCellIds = "AA==";
	assert.throws( () => nav.install( pose.regionId, bad ) );
	assert.equal( nav.clip( pose, { ...pose, x: 80 } ).x, 80 );
	const object = bundle();
	object.navmesh.regions[0].objects = [ { assetId: 1 } ];
	nav.install( pose.regionId, object );
	assert.equal( nav.clip( pose, { ...pose, x: 80 } ), null );
});
test("movement request IDs survive resets and authoritative clipped segments win", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.navigation( pose.regionId, bundle() );
	const frame = m.request( { ...pose, x: 200 }, 0 );
	assert.equal( frame.opcode, 9 );
	assert.deepEqual( [ ...frame.payload.slice( 5 ) ], [ 1, 79, 107, 200, 0, 10, 0, 100, 0 ] );
	m.step( 100 );
	assert.equal( m.state().pose.x, 65 );
	m.receive( receipt( 1, { ...pose, x: 80 } ), 100 );
	m.step( 600 );
	assert.equal( m.state().pose.x, 70 );
	m.step( 1100 );
	assert.equal( m.state().pose.x, 80 );
	assert.equal( m.state().pendingMoves, 0 );
	m.clear();
	m.seed( pose );
	const next = m.request( { ...pose, x: 70 }, 0 );
	assert.equal( new DataView( next.payload.buffer ).getUint32( 1, true ), 2 );
});
test("out-of-order receipts cannot roll back newer authority; rejected moves retain prior server segment", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.request( { ...pose, x: 100 }, 0 );
	m.request( { ...pose, x: 120 }, 1 );
	m.receive( receipt( 2, { ...pose, x: 90 }, false ), 10 );
	m.receive( receipt( 1, { ...pose, x: 100 } ), 20 );
	m.step( 1010 );
	assert.equal( m.state().pose.x, 90 );
	assert.equal( m.state().acknowledgedMove, 2 );
	assert.equal( m.state().error, "blocked" );
	assert.throws( () => m.receive( receipt( 99, pose ), 0 ), /Unsolicited/ );
});
test("missing movement receipts fail instead of accumulating or replaying indefinitely", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.request( pose, 0 );
	assert.throws( () => m.step( 10001 ), /timed out/ );
});
test("death retires predictions and late receipts through revival without changing walk speed", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.navigation( pose.regionId, bundle() );
	m.mode( 2, 0 );
	m.request( { ...pose, x: 400 }, 0 );
	m.step( 500 );
	m.life( 2, 750 );
	const corpse = m.state().pose;
	assert.equal( corpse.x, 75 );
	assert.equal( m.state().pendingMoves, 0 );
	assert.equal( m.state().moving, false );
	m.receive( receipt( 1, { ...pose, x: 400 } ), 1000 );
	m.step( 20000 );
	assert.deepEqual( m.state().pose, corpse );
	assert.throws( () => m.request( { ...pose, x: 500 }, 20000 ), /while dead/ );
	m.life( 2, 21000 );
	assert.deepEqual( m.state().pose, corpse );
	m.correct( corpse );
	m.life( 1, 22000 );
	m.receive( receipt( 1, { ...pose, x: 400 } ), 23000 );
	assert.deepEqual( m.state().pose, corpse );
	m.request( { ...pose, x: 400 }, 24000 );
	m.step( 25000 );
	assert.equal( m.state().pose.x, 95 );
});
test("dead local movement ignores native travel until LIFE-alive", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.life( 2, 0 );
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( 7 );
	p[4] = 1;
	p.writeUInt16LE( pose.regionId, 5 );
	p.writeInt16LE( 260, 7 );
	p.writeInt16LE( 10, 9 );
	p.writeInt16LE( 100, 11 );
	m.native( p, 100, 7 );
	m.step( 1000 );
	assert.deepEqual( m.state().pose, pose );
	assert.equal( m.state().moving, false );
	m.life( 1, 2000 );
	m.native( p, 2000, 7 );
	m.step( 3000 );
	assert.equal( m.state().pose.x, 110 );
});
test("movement lifecycle interleavings cannot restart or time out a corpse", () => {
	fc.assert(
		fc.property(
			fc.array( fc.constantFrom( "move", "die", "alive", "receipt", "correct", "tick" ), {
				minLength: 1,
				maxLength: 80
			} ),
			events => {
				const m = createMovement( () => {} );
				m.seed( pose );
				let dead = false, id = 0, now = 0, corpse = pose;
				for ( const event of events ) {
					now += 25;
					switch ( event ) {
						case "move":
							if ( !dead && m.state().pendingMoves < 30 ) {
								m.request( { ...pose, x: 200 }, now );
								id++;
							}
							break;
						case "die":
							m.life( 2, now );
							if ( !dead ) corpse = m.state().pose;
							dead = true;
							break;
						case "alive":
							m.life( 1, now );
							dead = false;
							break;
						case "receipt":
							if ( id ) {
								m.receive( receipt( id, { ...pose, x: 200 } ), now );
							}
							break;
						case "correct":
							m.correct( m.state().pose );
							break;
						case "tick":
							m.step( now );
							break;
					}
					if ( dead ) {
						assert.equal( m.state().moving, false );
						assert.equal( m.state().pendingMoves, 0 );
						assert.deepEqual( m.state().pose, corpse );
					}
				}
				if ( dead ) assert.doesNotThrow( () => m.step( now + 20000 ) );
			}
		),
		{ numRuns: 100, seed: 20260913 }
	);
});
/*
================
item
================
*/
function item( ref, quantity ) {
	const p = Buffer.alloc( 6 );
	p.writeUInt32LE( ref );
	p.writeUInt16LE( quantity, 4 );
	return [ ...p ];
}
test("gold pickup grants are decoded without touching bag slots or acknowledging another transaction", () => {
	const heard = [], inv = createInventory( () => {}, cue => heard.push( cue ) );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	inv.move( 13, 14, 10, 100 );
	const before = inv.state().inventory;
	const grant = Buffer.from( [ 1, 6, 254, 0xdc, 5, 0, 0 ] );
	assert.equal( inv.receive( 0xb06d, grant ), true );
	assert.deepEqual( inv.state().inventory, before );
	assert.equal( inv.state().inventoryPending, true );
	assert.deepEqual( heard, [] );
	for ( let n = 3; n < grant.length; n++ ) {
		assert.throws( () => inv.receive( 0xb06d, grant.subarray( 0, n ) ), /gold pickup/ );
	}
});
test("unrelated pickup cannot acknowledge a move and timed-out transactions cannot be retried", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	inv.move( 13, 14, 10, 100 );
	inv.receive( 0xb06d, Uint8Array.of( 1, 6, 15, ...item( 1, 3 ) ) );
	assert.equal( inv.state().inventoryPending, true );
	assert.throws( () => inv.move( 13, 16, 10, 101 ), /unavailable/ );
	inv.step( 10099 );
	inv.receive( 0xb06d, Uint8Array.of( 1, 0, 13, 14, 10, 0, 0 ) );
	assert.equal( inv.state().inventoryPending, false );
	inv.use( 14, 11000 );
	inv.receive( 0xb5bd, Uint8Array.of( 1, 15, 2, 0, 0x6c, 0 ) );
	assert.equal( inv.state().inventoryPending, true );
	inv.receive( 0xb5bd, Uint8Array.of( 1, 14, 9, 0, 0x6c, 0 ) );
	assert.equal( inv.state().inventoryPending, false );
	inv.use( 14, 12000 );
	assert.throws( () => inv.step( 22000 ), /reconnect/ );
	assert.equal( inv.state().inventoryPending, true );
	assert.throws( () => inv.receive( 0xb5bd, Uint8Array.of( 1, 14, 8, 0, 0x6c, 0 ) ), /reconnect/ );
	assert.throws( () => inv.use( 14, 22001 ), /reconnect/ );
});
test("rejected transport submission leaves movement, inventory and targeting available", () => {
	let blocked = true;
	const sent = [],
		send = frame => {
			if ( blocked ) throw new Error( "backpressure" );
			sent.push( frame );
		};
	const m = createMovement( send ), inv = createInventory( send ), target = createTargeting( send );
	m.seed( pose );
	m.navigation( pose.regionId, bundle() );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	assert.throws( () => m.request( { ...pose, x: 100 }, 0 ), /backpressure/ );
	assert.equal( m.state().pendingMoves, 0 );
	m.step( 10001 );
	assert.deepEqual( m.state().pose, pose );
	assert.throws( () => inv.move( 13, 14, 1 ), /backpressure/ );
	assert.throws( () => inv.use( 13 ), /backpressure/ );
	assert.equal( inv.state().inventoryPending, false );
	assert.throws( () => target.select( 9 ), /backpressure/ );
	assert.equal( target.state().targetPending, 0 );
	blocked = false;
	m.request( { ...pose, x: 100 }, 0 );
	inv.use( 13 );
	target.select( 9 );
	assert.deepEqual( sent.map( f => f.opcode ), [ 9, 0x75bd, 0x745a ] );
	assert.equal( m.state().pendingMoves, 1 );
	assert.equal( inv.state().inventoryPending, true );
	assert.equal( target.state().targetPending, 9 );
});
test("gameplay reset publishes no inventory or pending command from the old world", async () => {
	const { createGameplay } = await load( "gameplay" );
	const game = createGameplay( () => {} );
	game.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	game.seed( { ...pose, gid: 7, heading: 0 } );
	game.command( { kind: "item-use", slot: 13 }, 0 );
	assert.equal( game.take().inventoryPending, true );
	game.reset();
	const state = game.take();
	assert.deepEqual( state.inventory, [] );
	assert.equal( state.inventoryPending, false );
	assert.equal( state.localGid, 0 );
	assert.equal( state.pose, null );
});
test("inventory remains authoritative through split, rejected use and absolute consume", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	inv.move( 13, 14, 4 );
	assert.equal( inv.state().inventory[0].quantity, 10 );
	assert.throws( () => inv.move( 13, 15, 1 ), /unavailable/ );
	inv.receive( 0xb06d, Uint8Array.of( 1, 0, 13, 14, 4, 0, 0 ) );
	assert.deepEqual( inv.state().inventory.map( i => [ i.slot, i.quantity ] ), [ [ 13, 6 ], [ 14, 4 ] ] );
	inv.use( 14 );
	inv.receive( 0xb5bd, Uint8Array.of( 2, 5 ) );
	assert.equal( inv.state().inventory.find( i => i.slot === 14 ).quantity, 4 );
	inv.use( 14 );
	inv.receive( 0xb5bd, Uint8Array.of( 1, 14, 3, 0, 0x6c, 0 ) );
	assert.equal( inv.state().inventory.find( i => i.slot === 14 ).quantity, 3 );
});
test("invalid multi-transfer cannot commit the valid prefix", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	assert.throws(
		() => inv.receive( 0xb06d, Uint8Array.of( 1, 0, 13, 14, 4, 0, 1, 0, 99, 15, 1, 0 ) ),
		/empty source/
	);
	assert.equal( inv.state().inventory.length, 1 );
	assert.equal( inv.state().inventory[0].quantity, 10 );
});
test("server-pinned combat impacts use one token and finalize only their own bracket", () => {
	const c = createCombat(),
		fixture = JSON.parse(
			fs.readFileSync(
				path.resolve( root, "../server/internal/game/item/wire/testdata/skill_action_result_fixture.json" ),
				"utf8"
			)
		);
	const grant = Buffer.alloc( 14 );
	grant[0] = 1;
	grant.writeUInt32LE( fixture.expect.targetGid, 1 );
	grant[5] = 1;
	grant.writeUInt32LE( 54, 6 );
	c.receive( 0xb45a, grant );
	for ( const row of fixture.scenarios ) {
		c.receive( row.opcode, Buffer.from( row.payloadHex, "hex" ) );
		assert.equal( c.state().vitals[0].hp, row.currentHp );
		c.receive( row.opcode, Buffer.from( row.payloadHex, "hex" ) );
		assert.equal( c.state().vitals[0].hp, row.currentHp );
	}
	assert.equal( c.state().casts.length, 2 );
	c.receive( 0xb505, Uint8Array.of( 2, 0, 1, 0, 0, 0 ) );
	c.step( 200 );
	assert.deepEqual( c.state().casts.map( c => c.token ), [ 2 ] );
	c.remove( fixture.expect.targetGid );
	assert.equal( c.state().casts.length, 1 );
	c.remove( c.state().casts[0].caster );
	assert.equal( c.state().casts.length, 0 );
});
test("target grants require matching intent and despawn clears pending selection", () => {
	const t = createTargeting( () => {} );
	t.select( 9 );
	const p = Buffer.alloc( 11 );
	p[0] = 1;
	p.writeUInt32LE( 8, 1 );
	t.receive( 0xb45a, p );
	assert.equal( t.state().target, 0 );
	p.writeUInt32LE( 9, 1 );
	t.receive( 0xb45a, p );
	assert.equal( t.state().target, 9 );
	t.remove( 9 );
	assert.equal( t.state().target, 0 );
});

test("motion output contains only owned pose fields and gait retiming crosses regions continuously", () => {
	const m = createEntityMotion(),
		entity = {
			gid: 7,
			refObjId: 1,
			kind: "npc",
			name: "retained",
			regionId: 257,
			x: 1919,
			y: 0,
			z: 10,
			heading: 0,
			walkSpeed: 8,
			runSpeed: 20,
			movementMode: 3,
			mountedOn: 9
		};
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( 7 );
	p[4] = 1;
	p.writeUInt16LE( 258, 5 );
	p.writeInt16LE( 10, 7 );
	p.writeInt16LE( 10, 11 );
	m.receive( p, entity, 0 );
	const at = m.mode( { ...entity, movementMode: 2 }, 100 );
	assert.equal( at.regionId, 258 );
	assert.ok( Math.abs( at.x - 1 ) < 1e-8 );
	assert.deepEqual( Object.keys( at ).sort(), [ "gid", "heading", "moving", "regionId", "x", "y", "z" ] );
	const next = m.step( 225 )[0];
	assert.ok( Math.abs( next.x - 2 ) < 1e-8 );
	const end = m.step( 2000 )[0];
	assert.equal( end.x, 10 );
	assert.deepEqual( m.step( 2001 ), [] );
});

test("local native motion shares stop and gait transitions without replaying pending commands", async () => {
	const { createGameplay } = await load( "gameplay" ), sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {} );
	game.seed( { ...pose, gid: 7, heading: 0 } );
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( 7 );
	p[4] = 1;
	p.writeUInt16LE( pose.regionId, 5 );
	p.writeInt16LE( 160, 7 );
	p.writeInt16LE( pose.y, 9 );
	p.writeInt16LE( pose.z, 11 );
	game.receive( { opcode: 0xb738, payload: p }, 0 );
	game.step( 100 );
	assert.equal( game.take().pose.x, 65 );
	game.receive( { opcode: 0x3122, payload: Uint8Array.of( 7, 0, 0, 0, 1, 2 ) }, 100 );
	game.step( 300 );
	assert.equal( game.take().pose.x, 69 );
	game.receive( { opcode: 0x3122, payload: Uint8Array.of( 7, 0, 0, 0, 1, 0 ) }, 350 );
	assert.equal( game.take().pose.x, 70 );
	game.step( 1000 );
	assert.equal( game.take(), null );
	assert.equal( sent.length, 0 );
	game.dispose();
});
test("mode changes preserve server arrival times and outstanding receipt identities", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.request( { ...pose, x: 80 }, 0 );
	m.receive( receipt( 1, { ...pose, x: 80 } ), 100 );
	m.mode( 2, 300 );
	m.step( 600 );
	assert.equal( m.state().pose.x, 70, "server segment retains its arrival time" );
	m.request( { ...pose, x: 90 }, 601 );
	m.mode( 0, 700 );
	const stopped = m.state().pose.x;
	m.step( 800 );
	assert.equal( m.state().pose.x, stopped );
	assert.equal( m.state().pendingMoves, 1 );
	m.receive( receipt( 2, { ...pose, x: 90 } ), 900 );
	m.step( 1900 );
	assert.equal( m.state().pose.x, 90 );
	assert.equal( m.state().pendingMoves, 0 );
});

test("frameless selections never block a following NPC selection", async () => {
	const { createGameplay } = await load( "gameplay" ),
		sent = [],
		game = createGameplay( frame => sent.push( frame ) );
	game.bootstrap( {} );
	game.seed( { ...pose, gid: 1, heading: 0 } );
	for ( const kind of [ "player", "item" ] ) {
		game.command( { kind: "select", gid: 2 }, 0, { gid: 2, kind } );
		game.step( 86400000 );
		const state = game.take();
		assert.equal( state.targetPending, 0 );
		assert.equal( state.target, 2 );
	}
	assert.doesNotThrow( () => game.command( { kind: "select", gid: 3 }, 86400000, { gid: 3, kind: "npc" } ) );
	assert.equal( game.take().targetPending, 3 );
	assert.equal( sent.length, 2, "same-GID reselection does not send another grant request" );
	game.dispose();
});

test("silent selection refusal expires, ignores late grants and permits a new intent", async () => {
	const { createGameplay } = await load( "gameplay" ), game = createGameplay( () => {} );
	game.bootstrap( {} );
	game.seed( { ...pose, gid: 1, heading: 0 } );
	game.command( { kind: "select", gid: 2 }, 100, { gid: 2, kind: "npc" } );
	game.take();
	game.step( 10099 );
	assert.equal( game.take(), null );
	game.step( 10100 );
	const expired = game.take();
	assert.equal( expired.targetPending, 0 );
	assert.match( expired.error, /not confirmed/ );
	game.command( { kind: "select", gid: 3 }, 10101, { gid: 3, kind: "monster" } );
	const grant = Buffer.alloc( 11 );
	grant[0] = 1;
	grant.writeUInt32LE( 2, 1 );
	game.receive( { opcode: 0xb45a, payload: grant }, 10102 );
	assert.equal( game.take().target, 0 );
	grant.writeUInt32LE( 3, 1 );
	game.receive( { opcode: 0xb45a, payload: grant }, 10103 );
	const accepted = game.take();
	assert.equal( accepted.target, 3 );
	assert.equal( accepted.error, null );
	game.dispose();
});

test("missing untagged release requires resynchronization even after target despawns", () => {
	const targeting = createTargeting( () => {} );
	targeting.select( 9, 0, "player" );
	targeting.release( 100 );
	targeting.remove( 9 );
	assert.throws( () => targeting.select( 10, 200 ), /pending/ );
	assert.equal( targeting.step( 10099 ), false );
	assert.throws( () => targeting.step( 10100 ), /release timed out.*reconnect/ );
	targeting.clear();
	assert.doesNotThrow( () => targeting.select( 10, 10101, "player" ) );
});

test("fatal peer impact owns death even without a prior target HP grant", () => {
	const c = createCombat(),
		fixture = JSON.parse(
			fs.readFileSync(
				path.resolve( root, "../server/internal/game/item/wire/testdata/skill_action_result_fixture.json" ),
				"utf8"
			)
		);
	const fatal = fixture.scenarios.find( row => row.currentHp === 0 );
	assert.ok( fatal );
	c.receive( fatal.opcode, Buffer.from( fatal.payloadHex, "hex" ) );
	assert.equal( c.state().vitals.find( row => row.gid === fixture.expect.targetGid ).hp, 0 );
	const token = c.state().casts[0].token, p = Buffer.alloc( 6 );
	p[0] = 2;
	p.writeUInt32LE( token, 2 );
	c.receive( 0xb505, p );
	c.step( 200 );
	assert.equal( c.state().casts.length, 0 );
	assert.equal( c.state().vitals[0].hp, 0 );
	c.remove( fixture.expect.targetGid );
	assert.equal( c.state().vitals.length, 0 );
});

test("ground drops serialize with all inventory commands and await their own result", () => {
	const sent = [], inv = createInventory( f => sent.push( f ) );
	inv.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	assert.throws( () => inv.drop( 7 ) );
	inv.drop( 13, 100 );
	assert.deepEqual( [ ...sent[0].payload ], [ 7, 13 ] );
	assert.equal( inv.state().inventory.length, 1 );
	assert.throws( () => inv.use( 13 ) );
	assert.throws( () => inv.dropGold( 5 ) );
	inv.receive( 0xb06d, Uint8Array.of( 1, 7, 14 ) );
	assert.equal( inv.state().inventoryPending, true );
	inv.receive( 0xb06d, Uint8Array.of( 1, 7, 13 ) );
	assert.equal( inv.state().inventoryPending, false );
	assert.equal( inv.state().inventory.length, 0 );
	inv.dropGold( 1234, 200 );
	assert.deepEqual( [ ...sent[1].payload ], [ 10, 210, 4, 0, 0 ] );
	assert.throws( () => inv.receive( 0xb06d, Uint8Array.of( 1, 10, 210, 4, 0 ) ) );
	assert.equal( inv.state().inventoryPending, true );
	inv.receive( 0xb06d, Uint8Array.of( 1, 10, 1, 0, 0, 0 ) );
	assert.equal( inv.state().inventoryPending, true );
	inv.receive( 0xb06d, Uint8Array.of( 1, 10, 210, 4, 0, 0 ) );
	assert.equal( inv.state().inventoryPending, false );
	for ( const n of [ 0, -1, 1.5, 100000001, NaN, Infinity ] ) assert.throws( () => inv.dropGold( n ) );
	inv.dropGold( 1, 500 );
	inv.receive( 0xb06d, Uint8Array.of( 2, 1 ) );
	assert.equal( inv.state().inventoryPending, false );
	inv.dropGold( 1, 600 );
	assert.throws( () => inv.step( 10600 ), /timed out/ );
	assert.throws( () => inv.dropGold( 1 ), /reconnect/ );
});

test("native sale consumes only its authoritative quantity and never acknowledges a pending move", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	const sale = Uint8Array.of( 1, 9, 13, 3, 0, 17, 0, 0, 0, 0 );
	inv.move( 13, 14, 1 );
	inv.receive( 0xb06d, sale );
	assert.equal( inv.state().inventory[0].quantity, 7 );
	assert.equal( inv.state().inventoryPending, true );
	const before = inv.state().inventory;
	for ( let n = 2; n < sale.length; n++ ) assert.throws( () => inv.receive( 0xb06d, sale.slice( 0, n ) ) );
	assert.equal( inv.state().inventory, before );
	const stale = sale.slice();
	stale[3] = 8;
	assert.throws( () => inv.receive( 0xb06d, stale ), /Stale sale/ );
	assert.equal( inv.state().inventory, before );
	const all = sale.slice();
	all[3] = 7;
	inv.receive( 0xb06d, all );
	assert.equal( inv.state().inventory.length, 0 );
});

test("COS sale reaches the live COS owner and preserves player inventory", async () => {
	const { createGameplay } = await load( "gameplay" ), game = createGameplay( () => {} );
	game.bootstrap( {
		refObjSnapshot: [ { refObjId: 102, kind: "cos", tidWord: (2 << 11) | 0x1c6 } ],
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	game.seed( { ...pose, gid: 7, heading: 0 } );
	const record = Buffer.alloc( 29 );
	record.writeUInt32LE( 42 );
	record.writeUInt32LE( 102, 4 );
	record.writeUInt32LE( 100, 8 );
	record[16] = 28;
	record[17] = 1;
	record[18] = 2;
	Buffer.from( item( 1, 10 ) ).copy( record, 19 );
	game.receive( { opcode: 0x3158, payload: record }, 0 );
	const sale = Uint8Array.of( 1, 20, 42, 0, 0, 0, 2, 5, 0, 17, 0, 0, 0, 0 );
	game.receive( { opcode: 0xb06d, payload: sale }, 1 );
	let state = game.take();
	assert.equal( state.cosRecords[0].inventory[0].quantity, 5 );
	assert.equal( state.inventory[0].quantity, 10 );
	const malformed = sale.slice();
	malformed[7] = 6;
	assert.throws( () => game.receive( { opcode: 0xb06d, payload: malformed }, 2 ) );
	game.receive( { opcode: 0xb06d, payload: sale }, 3 );
	state = game.take();
	assert.deepEqual( state.cosRecords[0].inventory, [] );
	assert.equal( state.inventory[0].quantity, 10 );
	game.reset();
	assert.throws( () => game.receive( { opcode: 0xb06d, payload: sale }, 4 ), /absent COS/ );
});

test("mounted prediction carries the native COS move with the same receipt owner", () => {
	const sent = [], m = createMovement( f => sent.push( f ) );
	m.seed( pose );
	const f = m.request( { ...pose, x: 120 }, 0, 0xc00003 );
	assert.equal( f.opcode, 9 );
	assert.deepEqual( [ ...f.payload ], [ 2, 1, 0, 0, 0, 3, 0, 192, 0, 1, 1, 79, 107, 120, 0, 10, 0, 100, 0 ] );
	assert.equal( m.state().pendingMoves, 1 );
	m.receive( receipt( 1, { ...pose, x: 120 } ), 100 );
	assert.equal( m.state().pendingMoves, 0 );
	assert.throws( () => m.request( pose, 101, 0 ) );
});

test("typed pickup receipts replace absolute stack counts and reject invalid bag destinations atomically", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 3630, typeFlags: 0x8ec } ],
		equipItems: [ { refObjId: 3630, slot: 13, body: item( 3630, 45 ) } ]
	} );
	// Independent literal B06D receipt: ordinary potion, final stack count 50.
	const receipt = Buffer.from( "01060d2e0e00003200", "hex" );
	inv.receive( 0xb06d, receipt );
	assert.equal( inv.state().inventory[0].quantity, 50 );
	inv.receive( 0xb06d, receipt );
	assert.equal( inv.state().inventory[0].quantity, 50 );
	const before = inv.state().inventory;
	for ( const slot of [ 0, 12, 45, 255 ] ) {
		const bad = Buffer.from( receipt );
		bad[2] = slot;
		assert.throws( () => inv.receive( 0xb06d, bad ), /non-bag/ );
		assert.deepEqual( inv.state().inventory, before );
	}
	for ( let length = 0; length < receipt.length; length++ ) {
		const p = receipt.subarray( 0, length );
		try {
			inv.receive( 0xb06d, p );
		} catch {}
		assert.deepEqual( inv.state().inventory, before );
	}
	const equipmentShaped = Buffer.concat( [ receipt.subarray( 0, 7 ), Buffer.alloc( 14 ) ] );
	assert.throws( () => inv.receive( 0xb06d, equipmentShaped ), /body/ );
	assert.deepEqual( inv.state().inventory, before );
});

// Endpoints have the same height; a straight XYZ chord would pass under the hill.
test("live correction settles on the hill immediately and retires prediction", () => {
	const b = bundle(), heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let z = 0; z < 97; z++ ) {
		for ( let x = 0; x < 97; x++ ) heights.writeFloatLE( x === 4 ? 30 : 10, (z * 97 + x) * 4 );
	}
	b.navmesh.regions[0].heightMap = heights.toString( "base64" );
	const m = createMovement( () => {} );
	m.seed( pose );
	m.navigation( pose.regionId, b );
	m.request( { ...pose, x: 100 }, 0 );
	m.step( 400 );
	m.correct( { ...pose, x: 80, y: 10, angle: 1234 } );
	assert.equal( m.state().pose.y, 30 );
	assert.equal( m.state().authoritativePose.y, 30 );
	assert.equal( m.state().pose.angle, 1234 );
	assert.equal( m.state().moving, false );
	assert.equal( m.state().pendingMoves, 0 );
	assert.equal( m.step( 1000 ), false );
	assert.equal( m.state().pose.y, 30 );
});
test("predicted and acknowledged local motion follow resident navigation between endpoints", () => {
	const b = bundle(), heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let z = 0; z < 97; z++ ) {
		for ( let x = 0; x < 97; x++ ) heights.writeFloatLE( x === 4 ? 30 : 10, (z * 97 + x) * 4 );
	}
	b.navmesh.regions[0].heightMap = heights.toString( "base64" );
	const m = createMovement( () => {} );
	m.seed( pose );
	m.navigation( pose.regionId, b );
	m.request( { ...pose, x: 100 }, 0 );
	m.step( 400 );
	assert.equal( m.state().pose.x, 80 );
	assert.equal( m.state().pose.y, 30 );
	m.receive( receipt( 1, { ...pose, x: 100 } ), 500 );
	assert.equal( m.state().pose.x, 80 );
	assert.equal( m.state().pose.y, 30, "confirmation keeps the current hill surface" );
	m.step( 750 );
	assert.equal( m.state().pose.x, 90 );
	assert.equal( m.state().pose.y, 20 );
	m.step( 1500 );
	assert.equal( m.state().pose.y, 10 );
});
test("remote motion consumes the same surface resolver and preserves its sampled reference", () => {
	const nav = createNavigation(), b = bundle(), heights = Buffer.alloc( 97 * 97 * 4 );
	for ( let z = 0; z < 97; z++ ) {
		for ( let x = 0; x < 97; x++ ) heights.writeFloatLE( x === 4 ? 30 : 10, (z * 97 + x) * 4 );
	}
	b.navmesh.regions[0].heightMap = heights.toString( "base64" );
	nav.install( pose.regionId, b );
	const m = createEntityMotion( nav.surface ),
		e = { ...pose, gid: 7, heading: 0, walkSpeed: 20, runSpeed: 50, movementMode: 2 };
	const p = Buffer.alloc( 14 );
	p.writeUInt32LE( 7 );
	p[4] = 1;
	p.writeUInt16LE( pose.regionId, 5 );
	p.writeInt16LE( 100, 7 );
	p.writeInt16LE( 10, 9 );
	p.writeInt16LE( 100, 11 );
	m.receive( p, e, 0 );
	assert.equal( m.step( 1000 )[0].y, 30 );
	assert.equal( m.step( 2000 )[0].y, 10 );
	m.receive( p, e, 0 );
	m.step( 1000 );
	const corrected = m.correct( e, { ...pose, x: 80, y: 10, angle: 321 } );
	assert.equal( corrected.y, 30 );
	assert.equal( corrected.heading, 321 );
	assert.equal( corrected.moving, false );
	assert.deepEqual( m.step( 3000 ), [] );
});

test("native movement derives all cardinal headings but preserves explicit stop headings", () => {
	const m = createEntityMotion(), entity = { ...pose, gid: 7, heading: 1234, runSpeed: 50 };
	// 853550 subtracts pi/2 from model yaw before encoding wire heading.
	// These are wire bearings, not the model-facing angles used by the renderer.
	for ( const [x, z, heading] of [ [ 60, 80, 49151 ], [ 80, 100, 0 ], [ 60, 120, 16383 ], [ 40, 100, 32767 ] ] ) {
		const p = Buffer.alloc( 14 );
		p.writeUInt32LE( 7 );
		p[4] = 1;
		p.writeUInt16LE( pose.regionId, 5 );
		p.writeInt16LE( x, 7 );
		p.writeInt16LE( 10, 9 );
		p.writeInt16LE( z, 11 );
		m.receive( p, entity, 0 );
		assert.equal( m.step( 10 )[0].heading, heading );
	}
	const stop = Buffer.alloc( 9 );
	stop.writeUInt32LE( 7 );
	stop.writeUInt16LE( 54321, 6 );
	m.receive( stop, entity, 0 );
	assert.equal( m.step( 0 )[0].heading, 54321 );
});

test("freeze blocks navigation and a cleared 0x36C7 snapshot releases it; sleep does not", async () => {
	const { createGameplay } = await load( "gameplay" ),
		sent = [],
		game = createGameplay( f => sent.push( f ) ),
		local = { ...pose, gid: 7, heading: 0, appearanceState: [ 1, 0, 0 ] };
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const snap = mask => {
		const payload = new Uint8Array( mask ? 9 : 4 );
		new DataView( payload.buffer ).setUint32( 0, mask, true );
		return { opcode: 0x36c7, payload };
	};
	game.receive( snap( 1 ), 10 );
	game.command( { kind: "move", destination: { ...pose, x: 80 } }, 11, undefined, local );
	assert.equal( sent.length, 0 );
	game.receive( snap( 0 ), 12 );
	game.command( { kind: "move", destination: { ...pose, x: 80 } }, 13, undefined, local );
	assert.equal( sent.length, 1 );
	sent.length = 0;
	game.receive( snap( 0x40 ), 14 );
	game.command( { kind: "move", destination: { ...pose, x: 90 } }, 15, undefined, local );
	assert.equal( sent.length, 1 );
	game.dispose();
});
test("one decal is published with native target categories, ground replacement and world reset", async () => {
	const { createGameplay } = await load( "gameplay" ), game = createGameplay( () => {} );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( { ...pose, gid: 7, heading: 0 } );
	game.command( { kind: "select", gid: 8 }, 0, { gid: 8, kind: "player" } );
	assert.deepEqual( game.take().selectionDecal, { kind: "target", gid: 8, slot: 2 } );
	const destination = { ...pose, x: 80 };
	game.command( { kind: "move", destination }, 1 );
	const moved = game.take();
	assert.deepEqual( moved.selectionDecal, { kind: "ground", pose: destination } );
	assert.equal( moved.target, 8 );
	game.resetWorld();
	assert.equal( game.take().selectionDecal, null );
	game.dispose();
});

test("same-target clicks restore the shared marker without duplicate grants, including pending and stopped movement", async () => {
	const { createGameplay } = await load( "gameplay" );
	for ( const kind of [ "monster", "cos", "player", "npc" ] ) {
		const sent = [], game = createGameplay( f => sent.push( f ) ), entity = { gid: 8, kind };
		game.bootstrap( { simulationProtocolVersion: 1 } );
		game.seed( { ...pose, gid: 7, heading: 0 } );
		game.command( { kind: "select", gid: 8 }, 0, entity );
		for ( let cycle = 0; cycle < 3; cycle++ ) {
			game.command( { kind: "move", destination: { ...pose, x: 80 + cycle } }, cycle + 1 );
			assert.equal( game.take().selectionDecal.kind, "ground" );
			game.command( { kind: "select", gid: 8 }, cycle + 2, entity );
			assert.equal( game.take().selectionDecal.gid, 8 );
		}
		assert.equal( sent.filter( f => f.opcode === 0x745a ).length, 1 );
		if ( kind === "monster" || kind === "npc" ) {
			const p = Buffer.alloc( kind === "monster" ? 14 : 11 );
			p[0] = 1;
			p.writeUInt32LE( 8, 1 );
			p[5] = kind === "monster" ? 1 : 0;
			game.receive( { opcode: 0xb45a, payload: p }, 20 );
		}
		const stop = Buffer.alloc( 9 );
		stop.writeUInt32LE( 7 );
		game.receive( { opcode: 0xb738, payload: stop }, 21 );
		assert.equal( game.take().selectionDecal, null );
		game.command( { kind: "select", gid: 8 }, 22, entity );
		assert.equal( game.take().selectionDecal.gid, 8 );
		assert.equal( sent.filter( f => f.opcode === 0x745a ).length, 1 );
		game.receive( { opcode: 0x36ab, payload: Uint8Array.of( 8, 0, 0, 0 ) }, 23 );
		assert.equal( game.take().selectionDecal, null );
		game.resetWorld();
		assert.equal( game.take().selectionDecal, null );
		game.dispose();
	}
});

test("repeated pending selection preserves deadline and release barrier", () => {
	const sent = [], t = createTargeting( f => sent.push( f ) );
	t.select( 8, 0, "monster" );
	assert.equal( t.select( 8, 9999, "monster" ), null );
	assert.equal( t.step( 10000 ), true );
	t.select( 8, 10001, "player" );
	t.release( 10002 );
	assert.throws( () => t.select( 8, 10003 ), /pending/ );
	assert.throws( () => t.select( 9, 10003 ), /pending/ );
	assert.equal( sent.length, 3 );
	t.clear();
	assert.ok( t.select( 8, 10004, "player" ) );
});

test("an older receipt cannot reverse a newer predicted click or consume its pending identity", () => {
	for ( const accepted of [ true, false ] ) {
		const m = createMovement( () => {} );
		m.seed( pose );
		m.navigation( pose.regionId, bundle() );
		m.request( { ...pose, x: 160 }, 0 );
		m.step( 100 );
		m.request( { ...pose, x: 20 }, 100 );
		m.step( 200 );
		const before = m.state().pose;
		m.receive( receipt( 1, { ...pose, x: 160 }, accepted ), 200 );
		assert.deepEqual( m.state().pose, before );
		assert.equal( m.state().pendingMoves, 1 );
		assert.equal( m.state().acknowledgedMove, 1 );
		m.step( 300 );
		assert.ok( m.state().pose.x < before.x, "latest intent continues toward the second click" );
		m.receive( receipt( 2, { ...pose, x: 25 } ), 300 );
		m.step( 1300 );
		assert.equal( m.state().pose.x, 25 );
		assert.equal( m.state().pendingMoves, 0 );
	}
});

test("a receipt confirming the predicted path cannot rewind progress made during delivery", () => {
	const m = createMovement( () => {} ), to = { ...pose, x: 110 };
	m.seed( pose );
	m.navigation( pose.regionId, bundle() );
	m.request( to, 0 );
	m.step( 100 );
	assert.equal( m.state().pose.x, 65 );
	m.receive( receipt( 1, to ), 100 );
	assert.equal( m.state().pose.x, 65 );
	assert.equal( m.state().authoritativePose.x, 60, "server serialization position remains available" );
	m.step( 200 );
	assert.equal( m.state().pose.x, 70 );
	m.step( 1000 );
	assert.equal( m.state().pose.x, 110 );
});

test("tooltip metadata follows reference updates and item replacement, and is retired on a new login", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		refItemSnapshot: [ {
			refObjId: 1,
			typeFlags: 0x6c,
			name: "First",
			descriptionSymbol: "SN_FIRST_DESC",
			nativeFields: { itemParam1_29c: 120 }
		} ],
		equipItems: [ { refObjId: 1, slot: 13, body: item( 1, 10 ) } ]
	} );
	const first = inv.state().inventory;
	assert.equal( inv.state().inventory, first );
	assert.equal( first[0].tooltip.fields.itemParam1_29c, 120 );
	inv.references( [ {
		refObjId: 2,
		typeFlags: 0x6c,
		name: "Second",
		tooltip: { fields: { itemParam1_29c: 240 }, descriptionSymbol: "SN_SECOND_DESC" }
	} ] );
	const p = Buffer.alloc( 8 );
	p[0] = 13;
	p[1] = 9;
	p.writeUInt32LE( 2, 2 );
	p.writeUInt16LE( 5, 6 );
	assert.equal( inv.receive( 0x3645, p, 0 ), true );
	const second = inv.state().inventory[0];
	assert.equal( second.refObjId, 2 );
	assert.equal( second.quantity, 5 );
	assert.equal( second.tooltip.fields.itemParam1_29c, 240 );
	assert.equal( second.tooltip.descriptionSymbol, "SN_SECOND_DESC" );
	assert.equal( first[0].refObjId, 1 );
	inv.bootstrap( {
		refItemSnapshot: [ { refObjId: 2, typeFlags: 0x6c } ],
		equipItems: [ { refObjId: 2, slot: 13, body: item( 2, 1 ) } ]
	} );
	assert.equal( inv.state().inventory[0].tooltip, undefined );
	inv.clear();
	assert.deepEqual( inv.state().inventory, [] );
});

test("COS and player item presentations resolve through the same cached reference owner", () => {
	const inv = createInventory( () => {} );
	inv.bootstrap( {
		refItemSnapshot: [ {
			refObjId: 1,
			typeFlags: 0x6c,
			name: "Native potion",
			nativeFields: { itemParam1_29c: 120 }
		} ]
	} );
	const item = {
		slot: 0,
		refObjId: 1,
		typeFlags: 0x6c,
		quantity: 1,
		plus: 0,
		variance: "0",
		durability: 0,
		magic: []
	};
	const first = inv.present( item );
	assert.equal( first.name, "Native potion" );
	assert.equal( first.tooltip.fields.itemParam1_29c, 120 );
	assert.equal( inv.present( item ), first );
	inv.references( [ {
		refObjId: 1,
		typeFlags: 0x6c,
		name: "Localized potion",
		tooltip: { fields: { itemParam1_29c: 140 } }
	} ] );
	const next = inv.present( item );
	assert.notEqual( next, first );
	assert.equal( next.tooltip.fields.itemParam1_29c, 140 );
	assert.equal( next.name, "Localized potion" );
	inv.clear();
	assert.equal( inv.present( item ).tooltip, undefined );
});

test("live timed effect reaches gameplay publication", async () => {
	const { createGameplay } = await load( "gameplay" ),
		sent = [],
		game = createGameplay( frame => sent.push( frame ) );
	game.bootstrap( {
		refSkillSnapshot: [ {
			id: 5410,
			group: 373,
			level: 1,
			status: false,
			effectRider: false,
			effectDurationMs: 3600000
		} ]
	} );
	game.seed( { ...pose, gid: 100001, heading: 0 } );
	game.take();
	const p = Uint8Array.of( 161, 134, 1, 0, 34, 21, 0, 0, 7, 0, 0, 0 );
	assert.equal( game.receive( { opcode: 0xb419, payload: p }, 100 ), true );
	assert.equal( game.take().attachedEffects.find( e => e.skill === 5410 ).remainingMs, 3600000 );
	game.resetWorld();
	game.seed( { ...pose, gid: 100001, heading: 0 } );
	assert.deepEqual( game.take().attachedEffects, [] );
	assert.equal( game.receive( { opcode: 0xb419, payload: p }, 200 ), true );
	assert.equal( game.take().attachedEffects[0].remainingMs, 3600000 );
	game.command( { kind: "effect-cancel", skillId: 5410, token: 7 }, 201 );
	assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 1, 5, 34, 21, 0, 0, 7, 0, 0, 0, 0 ] );
	assert.equal( game.take().attachedEffects.length, 1, "cancellation waits for server teardown" );
	game.command( { kind: "effect-cancel", skillId: 5410, token: 0 }, 202 );
	assert.deepEqual(
		[ ...sent.at( -1 ).payload ],
		[ 1, 5, 34, 21, 0, 0, 0 ],
		"ordinary buff uses native tokenless form"
	);
	assert.equal(
		game.command( { kind: "effect-cancel", skillId: 5410, token: 8 }, 202 ),
		null,
		"stale icon command is harmless after teardown"
	);
	const count = sent.length;
	assert.equal(
		game.command( { kind: "effect-cancel", skillId: 5411, token: 7 }, 202 ),
		null,
		"a valid token does not authorize another skill ID"
	);
	assert.equal( sent.length, count );
	game.receive( { opcode: 0xb6a0, payload: Uint8Array.of( 1, 7, 0, 0, 0 ) }, 203 );
	assert.deepEqual( game.take().attachedEffects, [] );
	game.reset();
	assert.throws( () => game.receive( { opcode: 0xb419, payload: p }, 300 ), /reference authority/ );
	game.dispose();
});

test("Berserk command waits for authoritative points and seeds presentation once", async () => {
	const { createGameplay } = await load( "gameplay" ),
		sent = [],
		events = [],
		game = createGameplay( f => sent.push( f ), () => {}, e => events.push( e ) );
	const local = { ...pose, gid: 7, heading: 0 };
	game.seed( local );
	assert.equal( game.command( { kind: "berserk" }, 0, undefined, local ), null );
	game.receive( { opcode: 0x30b3, payload: Uint8Array.of( 4, 5, 0, 0, 0, 0 ) }, 1 );
	events.length = 0;
	game.seed( local );
	assert.equal( events.length, 0 );
	game.command( { kind: "berserk" }, 2, undefined, local );
	assert.deepEqual( sent, [ { opcode: 0x7341, payload: Uint8Array.of( 1 ) } ] );
	assert.equal( game.receive( { opcode: 0xb341, payload: Uint8Array.of( 2, 1 ) }, 3 ), true );
	game.receive( { opcode: 0x30b3, payload: Uint8Array.of( 4, 0, 0, 0, 0, 0 ) }, 4 );
	assert.equal( game.command( { kind: "berserk" }, 5, undefined, local ), null );
	assert.throws( () => game.receive( { opcode: 0xb341, payload: Uint8Array.of( 2 ) }, 6 ) );
	game.dispose();
});

test("accepted turns preserve current prediction despite an older server start position", () => {
	const m = createMovement( () => {} );
	m.seed( pose );
	m.navigation( pose.regionId, bundle() );
	m.request( { ...pose, x: 160 }, 0 );
	m.step( 200 );
	const turn = { ...pose, x: 70, z: 200 };
	m.request( turn, 200 );
	m.step( 300 );
	const before = m.state().pose;
	const response = {
		v: 1,
		id: 2,
		gid: 100007,
		accepted: true,
		serverTimeMs: 1000,
		world: { spawn: turn, moveSegment: { from: { ...pose, x: 65 }, startedAtMs: 1000, arrivesAtMs: 3000 } }
	};
	m.receive( Buffer.from( JSON.stringify( response ) ), 300 );
	assert.deepEqual( m.state().pose, before, "an accepted turn cannot rewind to the serialization origin" );
	m.step( 500 );
	assert.ok( m.state().pose.z > before.z );
});

test("a short move completed before acknowledgement does not restart from the server origin", () => {
	const m = createMovement( () => {} ), to = { ...pose, x: 65 };
	m.seed( pose );
	m.navigation( pose.regionId, bundle() );
	m.request( to, 0 );
	m.step( 200 );
	assert.equal( m.state().moving, false );
	m.receive( receipt( 1, to ), 300 );
	assert.equal( m.state().pose.x, to.x );
	assert.equal( m.state().moving, false );
});

test("closing a target publishes local intent immediately while retaining the release barrier", () => {
	const sent = [], t = createTargeting( frame => sent.push( frame ) );
	t.select( 8, 0, "player" );
	t.release( 100 );
	assert.equal( t.state().target, 0 );
	assert.equal( t.state().targetPending, 8 );
	assert.equal( sent.at( -1 ).opcode, 0x74b3 );
	assert.throws( () => t.select( 9, 200 ), /pending/ );
	t.receive( 0xb4b3, Uint8Array.of( 1 ) );
	assert.equal( t.state().targetPending, 0 );
	t.select( 9, 300, "player" );
	assert.equal( t.state().target, 9 );
});

test("a failed release send preserves the displayed target", () => {
	const t = createTargeting( frame => {
		if ( frame.opcode === 0x74b3 ) throw Error( "backpressure" );
	} );
	t.select( 8, 0, "player" );
	assert.throws( () => t.release( 100 ), /backpressure/ );
	assert.equal( t.state().target, 8 );
	assert.equal( t.state().targetPending, 0 );
});
