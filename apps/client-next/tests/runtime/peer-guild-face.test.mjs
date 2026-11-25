/*
===========================================================================

peer-guild-face.test.mjs - a watched player's guild nameplate, live

#4413 (BUG-097): when a player the local client already sees creates,
joins or loses a guild, the server re-sends the face (0x365F, client
75FE90) or clears it (0x37E6, client 75C890). The entity's guild fields
follow, so the overhead guild row changes without a respawn.

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
const peer = JSON.parse( readFileSync(
	new URL( "../../../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", import.meta.url ),
	"utf8"
) ).scenarios[0];
const LOCAL_GID = 7, APPEARANCE_OPCODE = 0x365f, CLEARED_OPCODE = 0x37e6;

/*
================
world
================
*/
function world() {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 3,
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
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	return owner;
}

/*
================
ascii
================
*/
function ascii( text ) {
	const out = Buffer.alloc( 2 + text.length );
	out.writeUInt16LE( text.length, 0 );
	out.write( text, 2, "latin1" );
	return out;
}

/*
================
appearanceFrame

The server's EncodePeerGuildAppearance layout.
================
*/
function appearanceFrame( gid, guildId, name, grant, crests, role ) {
	const head = Buffer.alloc( 8 ), tail = Buffer.alloc( 13 );
	head.writeUInt32LE( gid, 0 );
	head.writeUInt32LE( guildId, 4 );
	crests.forEach( ( c, i ) => tail.writeUInt32LE( c, i * 4 ) );
	tail.writeUInt8( role, 12 );
	return { opcode: APPEARANCE_OPCODE, payload: Buffer.concat( [ head, ascii( name ), ascii( grant ), tail ] ) };
}

test("a face frame binds the watched peer's guild, and a clear frame removes it", () => {
	const owner = world();
	owner.receive( appearanceFrame( peer.gid, 42, "FaceBanner", "Scout", [ 3, 9, 4 ], 1 ) );
	let entity = defined( owner.read( peer.gid ) );
	assert.equal( entity.guildName, "FaceBanner" );
	assert.equal( entity.guildId, 42 );
	assert.equal( entity.guildGrantName, "Scout" );
	assert.deepEqual( entity.guildCrests, [ 3, 9, 4 ] );
	assert.equal( entity.guildWarTeam, 1 );

	const clear = Buffer.alloc( 4 );
	clear.writeUInt32LE( peer.gid );
	owner.receive( { opcode: CLEARED_OPCODE, payload: clear } );
	entity = defined( owner.read( peer.gid ) );
	assert.equal( entity.guildName, "" );
	assert.equal( entity.guildId, undefined );
	assert.equal( entity.guildGrantName, undefined );
	assert.equal( entity.guildCrests, undefined );
	owner.dispose();
});

test("an empty-name face frame binds nothing, as 75FE90's empty arm", () => {
	const owner = world();
	owner.receive( appearanceFrame( peer.gid, 42, "FaceBanner", "Scout", [ 3, 9, 4 ], 1 ) );
	const empty = Buffer.alloc( 10 );
	empty.writeUInt32LE( peer.gid, 0 );
	empty.writeUInt32LE( 42, 4 );
	owner.receive( { opcode: APPEARANCE_OPCODE, payload: empty } );
	const entity = defined( owner.read( peer.gid ) );
	assert.equal( entity.guildName, "FaceBanner" );
	assert.equal( entity.guildId, 42 );
	owner.dispose();
});

test("a face frame for an unseen gid changes nothing", () => {
	const owner = world();
	const before = defined( owner.read( peer.gid ) );
	owner.receive( appearanceFrame( peer.gid + 1, 42, "FaceBanner", "", [ 0, 0, 0 ], 0 ) );
	assert.equal( owner.read( peer.gid + 1 ), undefined );
	assert.equal( defined( owner.read( peer.gid ) ).guildName, before.guildName );
	owner.dispose();
});
