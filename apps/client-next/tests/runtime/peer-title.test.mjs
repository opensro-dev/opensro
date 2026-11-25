/*
===========================================================================

peer-title.test.mjs - the worn character title: frame, entity, name line

Later-native (#3502 slice 1): a peer's title id rides the port-carried
0x3FC3 frame right after its spawn row (the native client reads the id
off the spawn stream; a grown row would fail older tabs' length-strict
decoder). The frame sets the entity's title id and resolved format,
and the name line prints the name through the format.

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
const { titledDisplayName, visibleTitleFormat } = await import( "../../src/engine/foundation/ui/overhead-layout.ts" );
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const peer = JSON.parse( readFileSync(
	new URL( "../../../server/internal/game/world/simulation/testdata/peer_spawn_row_fixture.json", import.meta.url ),
	"utf8"
) ).scenarios[0];
const LOCAL_GID = 7, PEER_TITLE_OPCODE = 0x3fc3;

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
titleFrame
================
*/
function titleFrame( gid, titleId, format ) {
	const bytes = Buffer.from( format, "utf8" ), payload = Buffer.alloc( 10 + bytes.length );
	payload.writeUInt32LE( gid, 0 );
	payload.writeUInt32LE( titleId, 4 );
	payload.writeUInt16LE( bytes.length, 8 );
	bytes.copy( payload, 10 );
	return { opcode: PEER_TITLE_OPCODE, payload };
}

test("the title frame titles the spawned peer and the name line formats through it", () => {
	const owner = world();
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	owner.receive( titleFrame( peer.gid, 5, "<Snake Slayer> %s" ) );
	const entity = defined( owner.read( peer.gid ) );
	assert.equal( entity.characterTitleId, 5 );
	assert.equal( entity.characterTitleFormat, "<Snake Slayer> %s" );
	assert.equal( titledDisplayName( entity.name ?? "", entity.characterTitleFormat ), "<Snake Slayer> PeerA" );
	owner.dispose();
});

test("a title frame for an unknown gid is dropped, and a clearing frame untitles", () => {
	const owner = world();
	owner.receive( titleFrame( 424242, 5, "<Snake Slayer> %s" ) );
	assert.equal( owner.read( 424242 ), undefined );
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	owner.receive( titleFrame( peer.gid, 5, "<Snake Slayer> %s" ) );
	owner.receive( titleFrame( peer.gid, 0, "" ) );
	const entity = defined( owner.read( peer.gid ) );
	assert.equal( entity.characterTitleId, undefined );
	assert.equal( entity.characterTitleFormat, undefined );
	owner.dispose();
});

test("a malformed title frame is a protocol error", () => {
	const owner = world();
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	assert.throws(
		() => owner.receive( { opcode: PEER_TITLE_OPCODE, payload: Buffer.alloc( 9 ) } ),
		/Invalid peer title/
	);
	const trailing = titleFrame( peer.gid, 5, "<Snake Slayer> %s" );
	trailing.payload = Buffer.concat( [ trailing.payload, Buffer.from( [ 0 ] ) ] );
	assert.throws( () => owner.receive( trailing ), /Invalid peer title/ );
	owner.dispose();
});

test("a replacement spawn starts untitled", () => {
	const owner = world();
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	owner.receive( titleFrame( peer.gid, 5, "<Snake Slayer> %s" ) );
	const payload = Buffer.alloc( 4 );
	payload.writeUInt32LE( peer.gid );
	owner.receive( { opcode: 0x36ab, payload } );
	owner.receive( { opcode: 0x30d7, payload: Buffer.from( peer.payloadHex, "hex" ) } );
	const entity = defined( owner.read( peer.gid ) );
	assert.equal( entity.characterTitleId, undefined );
	assert.equal( entity.characterTitleFormat, undefined );
	owner.dispose();
});

test("the two title options gate own and other formats separately (#3502 slice 2)", () => {
	const all = defaultGameOptions(),
		mine = { gid: LOCAL_GID, characterTitleFormat: "<Snake Slayer> %s" },
		peerEntity = { gid: 4242, characterTitleFormat: "<Big Spender> %s" };
	assert.equal( visibleTitleFormat( mine, LOCAL_GID, all ), "<Snake Slayer> %s" );
	assert.equal( visibleTitleFormat( peerEntity, LOCAL_GID, all ), "<Big Spender> %s" );
	// Own title off hides only mine; other titles off hides only peers'.
	assert.equal( visibleTitleFormat( mine, LOCAL_GID, { ...all, ownTitle: false } ), undefined );
	assert.equal( visibleTitleFormat( peerEntity, LOCAL_GID, { ...all, ownTitle: false } ), "<Big Spender> %s" );
	assert.equal( visibleTitleFormat( mine, LOCAL_GID, { ...all, otherTitles: false } ), "<Snake Slayer> %s" );
	assert.equal( visibleTitleFormat( peerEntity, LOCAL_GID, { ...all, otherTitles: false } ), undefined );
	// No format stays no format, whatever the options say.
	assert.equal( visibleTitleFormat( { gid: LOCAL_GID }, LOCAL_GID, all ), undefined );
	assert.equal(
		titledDisplayName( "PeerA", visibleTitleFormat( peerEntity, LOCAL_GID, { ...all, otherTitles: false } ) ),
		"PeerA"
	);
});

test("titledDisplayName prints the plain name without a usable format", () => {
	assert.equal( titledDisplayName( "PeerA", undefined ), "PeerA" );
	assert.equal( titledDisplayName( "PeerA", "" ), "PeerA" );
	assert.equal( titledDisplayName( "PeerA", "<Big Spender> %s" ), "<Big Spender> PeerA" );
	assert.equal( titledDisplayName( "PeerA", "No placeholder" ), "PeerA" );
	assert.equal( titledDisplayName( "", "<Big Spender> %s" ), "" );
});
