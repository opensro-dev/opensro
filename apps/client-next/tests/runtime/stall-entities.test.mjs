/*
===========================================================================

stall-entities.test.mjs - stall appearance through the entity wire journal

Local owners have no peer appearance row. Stall broadcasts must still publish
their title mode, while peer updates preserve the rest of their spawn state.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );
const peer = JSON.parse( readFileSync(
	new URL( "../../../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", import.meta.url ),
	"utf8"
) ).scenarios[0];
const LOCAL_GID = 7, STALL_TITLE_MODE = 4;

/*
================
flushCore
================
*/
function flushCore( core ) {
	const batch = core.take();
	if ( batch ) core.ack( batch.sequence );
	return batch?.events ?? [];
}

test("core publishes an owner rename only after its successful title-edit receipt", () => {
	const sent = [], core = createWorldCore( frame => sent.push( frame ) );
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: {
			modelRef: peer.modelRefObjId,
			startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
		}
	} );
	core.receive( { opcode: 0x32a6, payload: Buffer.from( [ LOCAL_GID, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	core.receive( { opcode: 0xb049, payload: Buffer.from( [ 1 ] ) }, 0 );
	core.receive( stallFrame( 0x30df, LOCAL_GID, "Original", 1234 ), 0 );
	const opened = flushCore( core ).findLast( event => event.kind === "state" && event.entity.gid === LOCAL_GID );
	assert.ok( opened?.kind === "state" );
	core.command( { kind: "stall-title", text: "Renamed 王者" }, 1 );
	assert.equal( sent.at( -1 )?.opcode, 0x71a8 );
	assert.equal( sent.at( -1 )?.payload[0], 7 );
	assert.equal( flushCore( core ).filter( event => event.kind === "state" ).length, 0 );
	core.receive( { opcode: 0xb1a8, payload: Buffer.from( [ 2, 1 ] ) }, 2 );
	assert.equal( flushCore( core ).filter( event => event.kind === "state" ).length, 0 );
	core.command( { kind: "stall-title", text: "Accepted 王者" }, 3 );
	core.receive( { opcode: 0xb1a8, payload: Buffer.from( [ 1, 7 ] ) }, 4 );
	const renamed = flushCore( core ).find( event => event.kind === "state" && event.entity.gid === LOCAL_GID );
	assert.ok( renamed?.kind === "state" );
	assert.equal( renamed.entity.titleText, "Accepted 王者" );
	assert.equal( renamed.entity.titleId, 1234 );
	assert.equal( defined( renamed.entity.appearanceState )[6], STALL_TITLE_MODE );
	assert.equal( opened.entity.titleText, "Original" );
	core.receive( { opcode: 0xb42c, payload: Buffer.from( [ 1 ] ) }, 5 );
	core.receive( stallFrame( 0x33d1, LOCAL_GID ), 5 );
	const closed = flushCore( core ).find( event => event.kind === "state" && event.entity.gid === LOCAL_GID );
	assert.ok( closed?.kind === "state" );
	assert.equal( closed.entity.titleText, undefined );
	assert.equal( closed.entity.titleId, undefined );
	assert.equal( defined( closed.entity.appearanceState )[6], 0 );
	core.receive( { opcode: 0xb1a8, payload: Buffer.from( [ 1, 7 ] ) }, 6 );
	assert.equal( flushCore( core ).filter( event => event.kind === "state" ).length, 0 );
	core.dispose();
});

/*
================
stallFrame
================
*/
function stallFrame( opcode, gid, title = "", decoration = 0 ) {
	const text = Buffer.from( title, "utf16le" );
	const payload = Buffer.alloc( opcode === 0x33d1 ? 5 : 6 + text.length + (opcode === 0x30df ? 4 : 0) );
	payload.writeUInt32LE( gid );
	if ( opcode !== 0x33d1 ) {
		payload.writeUInt16LE( text.length / 2, 4 );
		text.copy( payload, 6 );
		if ( opcode === 0x30df ) payload.writeUInt32LE( decoration, 6 + text.length );
	}
	return { opcode, payload };
}

/*
================
world
================
*/
function world() {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: peer.modelRefObjId, kind: "player", tidWord: peer.modelTidWord } ],
		localPlayerEntry: {
			modelRef: peer.modelRefObjId,
			pvpState: 3,
			startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 }
		}
	} );
	const payload = Buffer.alloc( 8 );
	payload.writeUInt32LE( LOCAL_GID );
	owner.receive( { opcode: 0x32a6, payload } );
	owner.ack( defined( owner.take() ).sequence );
	return owner;
}

/*
================
peerSpawn

Extend the canonical server peer row with the native title-mode-4 tail.
================
*/
function peerSpawn( title, decoration ) {
	const raw = Buffer.from( peer.payloadHex, "hex" );
	const nameEnd = raw.indexOf( Buffer.from( peer.characterName ) ) + Buffer.byteLength( peer.characterName );
	raw[nameEnd + 6] = STALL_TITLE_MODE;
	const tail = stallFrame( 0x30df, peer.gid, title, decoration ).payload.subarray( 4 );
	return { opcode: 0x30d7, payload: Buffer.concat( [ raw.subarray( 0, -3 ), tail, raw.subarray( -3 ) ] ) };
}

test("local owner open, rename and close publish title mode without a peer appearance row", () => {
	const owner = world();
	assert.equal( defined( owner.read( LOCAL_GID ) ).appearanceState, undefined );
	owner.receive( { opcode: 0xb049, payload: Buffer.from( [ 1 ] ) } );
	owner.receive( stallFrame( 0x30df, LOCAL_GID, "王者 shop", 1234 ) );
	const opened = defined( owner.read( LOCAL_GID ) );
	assert.equal( opened.titleText, "王者 shop" );
	assert.equal( opened.titleId, 1234 );
	assert.deepEqual( opened.appearanceState, [ 1, 0, 0, 3, 0, 0, 4, 0, 0 ] );
	const published = defined( owner.take() );
	assert.ok( published.events.some( event => event.kind === "state" && event.entity === opened ) );
	owner.receive( stallFrame( 0x34b7, LOCAL_GID, "Renamed" ) );
	const renamed = defined( owner.read( LOCAL_GID ) );
	assert.equal( renamed.titleText, "Renamed" );
	assert.equal( renamed.titleId, 1234 );
	assert.equal( defined( renamed.appearanceState )[6], STALL_TITLE_MODE );
	owner.receive( { opcode: 0xb42c, payload: Buffer.from( [ 1 ] ) } );
	owner.receive( stallFrame( 0x33d1, LOCAL_GID ) );
	const closed = defined( owner.read( LOCAL_GID ) );
	assert.equal( closed.titleText, undefined );
	assert.equal( closed.titleId, undefined );
	assert.deepEqual( closed.appearanceState, [ 1, 0, 0, 3, 0, 0, 0, 0, 0 ] );
	assert.equal( opened.titleText, "王者 shop", "an offered journal batch retains its old snapshot" );
	assert.equal( defined( opened.appearanceState )[6], STALL_TITLE_MODE );
	owner.ack( published.sequence );
	const states = defined( owner.take() ).events.filter( event => event.kind === "state" );
	assert.deepEqual( states.map( event => event.entity.titleText ), [ "Renamed", undefined ] );
	owner.dispose();
});

test("peer spawn, rename, decoration update and close preserve unrelated appearance fields", () => {
	const owner = world();
	owner.receive( peerSpawn( "Peer shop", 1234 ) );
	const spawned = defined( owner.read( peer.gid ) );
	assert.equal( spawned.titleText, "Peer shop" );
	assert.equal( spawned.titleId, 1234 );
	assert.equal( defined( spawned.appearanceState )[6], STALL_TITLE_MODE );
	owner.receive( stallFrame( 0x34b7, peer.gid, "New name" ) );
	assert.equal( defined( owner.read( peer.gid ) ).titleId, 1234 );
	owner.receive( stallFrame( 0x30df, peer.gid, "Decorated", 5678 ) );
	assert.equal( defined( owner.read( peer.gid ) ).titleId, 5678 );
	owner.receive( stallFrame( 0x33d1, peer.gid ) );
	const closed = defined( owner.read( peer.gid ) );
	const appearance = [ ...defined( spawned.appearanceState ) ];
	appearance[6] = 0;
	assert.deepEqual( closed.appearanceState, appearance );
	assert.equal( closed.titleText, undefined );
	assert.equal( closed.titleId, undefined );
	assert.equal( spawned.titleText, "Peer shop" );
	assert.equal( defined( spawned.appearanceState )[6], STALL_TITLE_MODE );
	owner.dispose();
});

test("late stall updates do not resurrect a departed peer or survive its replacement spawn", () => {
	const owner = world();
	owner.receive( peerSpawn( "Old shop", 1234 ) );
	const payload = Buffer.alloc( 4 );
	payload.writeUInt32LE( peer.gid );
	owner.receive( { opcode: 0x36ab, payload } );
	for ( const opcode of [ 0x30df, 0x34b7, 0x33d1 ] ) owner.receive( stallFrame( opcode, peer.gid, "Late", 5678 ) );
	assert.equal( owner.read( peer.gid ), undefined );
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	const replacement = defined( owner.read( peer.gid ) );
	assert.equal( replacement.titleText, undefined );
	assert.equal( replacement.titleId, undefined );
	assert.equal( defined( replacement.appearanceState )[6], 0 );
	owner.dispose();
});
