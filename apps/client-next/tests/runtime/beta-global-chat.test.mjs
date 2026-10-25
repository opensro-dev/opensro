/*
===========================================================================

beta-global-chat.test.mjs - public echo, replay and character-session history

Exercise native receipt ordering through the chat owner and scene reentry
through the gameplay owner. Neither entity visibility nor travel owns history.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test, mock } from "node:test";
import assert from "node:assert/strict";
const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { chatLineColor } = await import( "../../src/engine/foundation/ui/chat-presentation.ts" );

/*
================
globalFrame
================
*/
function globalFrame( name, text ) {
	const sender = Buffer.from( name, "utf8" );
	const message = Buffer.from( text, "utf16le" );
	const payload = Buffer.alloc( 5 + sender.length + message.length );
	payload[0] = 6;
	payload.writeUInt16LE( sender.length, 1 );
	sender.copy( payload, 3 );
	payload.writeUInt16LE( text.length, 3 + sender.length );
	message.copy( payload, 5 + sender.length );
	return { opcode: 0x3667, payload };
}

test("global chat renders the authoritative name without an entity lookup", () => {
	mock.method( Date, "now", () => 1000 );
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	assert.equal( chat.receive( globalFrame( "FarAway", "hello from Jangan" ), 1 ), true );
	assert.deepEqual( chat.state().lines, [ {
		channel: 6,
		name: "FarAway",
		gid: undefined,
		text: "hello from Jangan",
		outgoing: false,
		sequence: 1,
		sentAt: 1000
	} ] );
	mock.restoreAll();
	chat.request( 1, "hello back", "", 0 );
	chat.receive( globalFrame( "Local", "hello back" ), 1 );
	assert.equal( chat.state().pending, true, "public echo must not release the native receipt gate" );
	const ownLine = chat.state().lines.at( -1 );
	assert.ok( ownLine );
	assert.equal( ownLine.channel, 6 );
	assert.deepEqual( chatLineColor( ownLine.channel ), chatLineColor( 6 ) );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().pending, false );
	assert.equal( chat.state().lines.length, 2 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 2, "an own-name broadcast must not duplicate the receipt" );
	assert.throws(
		() => chat.request( 6, "forged global", "", 1 ),
		/Invalid chat message/,
		"global channel remains server-owned"
	);
});

test("a suited player's global line renders once, under the job alias", () => {
	const chat = createChat( () => {} );
	// Entered wearing the trader suit (slot 8, a job item): the server
	// stamps the alias, and the entry alias board names the player by it.
	chat.bootstrap( {
		character: { name: "Local" },
		localPlayerEntry: { jobAlias: "TraderJo" },
		equipItems: [ { slot: 8, refObjId: 999 } ],
		refItemSnapshot: [ { refObjId: 999, typeFlags: 0xbac } ]
	} );
	chat.request( 1, "buy low sell high", "", 0 );
	chat.receive( globalFrame( "TraderJo", "buy low sell high" ), 1 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 1, "the suited line rendered twice" );
	assert.equal( chat.state().lines[0]?.name, "TraderJo", "the alias the server used must own the line" );
	assert.equal( chat.state().lines[0]?.channel, 6 );
	assert.equal( chat.state().pending, false );
});

test("public replay includes the user's earlier messages and retains repeated text", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( { character: { name: "Local" } } );
	for ( let i = 0; i < 2; i++ ) chat.receive( globalFrame( "Local", "same words" ), 1 );
	assert.equal( chat.state().lines.length, 2 );
	assert.ok( chat.state().lines.every( line => line.channel === 6 && !line.outgoing ) );
});

test("world reentry preserves chat while character changes and session reset clear it", () => {
	const game = createGameplay( () => {} );
	const entry = { character: { name: "Local" } };
	game.bootstrap( entry );
	game.receive( globalFrame( "FarAway", "before teleport" ), 0 );
	const before = game.take()?.chat?.lines;
	assert.equal( before?.length, 1 );
	game.resetWorld();
	game.bootstrap( entry );
	assert.deepEqual( game.take()?.chat?.lines, before );
	game.bootstrap( { character: { name: "Other" } } );
	assert.equal( game.take()?.chat?.lines.length, 0 );
	game.receive( globalFrame( "FarAway", "other session" ), 0 );
	game.reset();
	game.bootstrap( { character: { name: "Other" } } );
	assert.equal( game.take()?.chat?.lines.length, 0 );
	game.dispose();
});

test("live equipment rows move the chat dedupe name between the name and the alias", () => {
	const chat = createChat( () => {} );
	chat.bootstrap( {
		character: { name: "Local" },
		localPlayerEntry: { jobAlias: "TraderJo" },
		equipItems: [],
		refItemSnapshot: [ { refObjId: 999, typeFlags: 0xbac } ]
	} );
	chat.request( 1, "before the suit", "", 0 );
	chat.receive( globalFrame( "Local", "before the suit" ), 1 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 1 );
	chat.equipment( [ { slot: 8, typeFlags: 0xbac } ] );
	chat.request( 1, "suited now", "", 0 );
	chat.receive( globalFrame( "TraderJo", "suited now" ), 1 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 2, "the suited echo rendered twice" );
	assert.equal( chat.state().lines[1]?.name, "TraderJo" );
	chat.equipment( [] );
	chat.request( 1, "suit off again", "", 0 );
	chat.receive( globalFrame( "Local", "suit off again" ), 1 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 3, "the unsuited echo rendered twice" );
	assert.equal( chat.state().lines[2]?.name, "Local" );
});

/*
================
suitBody

The trader suit's entry body (refObjId 999, the 0xbac type word): the
equipment band's plus, variance, durability and empty magic options.
================
*/
function suitBody() {
	const body = Buffer.alloc( 18 );
	body.writeUInt32LE( 999, 0 );
	body.writeUInt32LE( 10, 13 );
	return [ ...body ];
}

test("own-line dedupe follows a job suit equipped and removed mid-session", () => {
	const entry = suited => ({
		simulationProtocolVersion: 1,
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		character: { name: "Local" },
		localPlayerEntry: { jobAlias: "TraderJo" },
		refItemSnapshot: [ { refObjId: 999, typeFlags: 0xbac } ],
		equipItems: [ { slot: suited ? 8 : 13, refObjId: 999, body: suitBody() } ]
	});
	const player = {
		gid: 1,
		refObjId: 1,
		kind: "player",
		name: "Local",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	};
	/*
	================
	speak

	Send one all-channel line and answer it the way the server does: the
	authoritative echo under the name the sender's current suit stamps,
	then the receipt.
	================
	*/
	function speak( game, now, text, stamped ) {
		game.command( { kind: "chat", channel: 1, text }, now, undefined );
		game.receive( globalFrame( stamped, text ), now );
		game.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, now );
		return game.take()?.chat?.lines.filter( line => line.text === text ) ?? [];
	}
	// Enter with the suit in the bag, then equip it: the stamp changes
	// from the character name to the job alias at send time.
	const game = createGameplay( () => {} );
	let now = 0;
	game.bootstrap( entry( false ) );
	game.seed( player );
	assert.equal( speak( game, ++now, "first", "Local" ).length, 1 );
	game.command( { kind: "inventory-move", source: 13, destination: 8, quantity: 0 }, ++now, undefined );
	game.receive( { opcode: 0xb06d, payload: Uint8Array.of( 1, 0, 13, 8, 1, 0, 0 ) }, ++now );
	assert.ok( game.take()?.inventory.some( row => row.slot === 8 ), "the suit is worn" );
	const suited = speak( game, ++now, "suited", "TraderJo" );
	assert.equal( suited.length, 1, "the suited line rendered twice" );
	assert.equal( suited[0]?.name, "TraderJo" );
	game.dispose();
	// Enter wearing the suit, then remove it: the mirror image.
	const dressed = createGameplay( () => {} );
	dressed.bootstrap( entry( true ) );
	dressed.seed( player );
	assert.equal( speak( dressed, ++now, "dressed", "TraderJo" ).length, 1 );
	dressed.command( { kind: "inventory-move", source: 8, destination: 13, quantity: 0 }, ++now, undefined );
	dressed.receive( { opcode: 0xb06d, payload: Uint8Array.of( 1, 0, 8, 13, 1, 0, 0 ) }, ++now );
	assert.ok( !dressed.take()?.inventory.some( row => row.slot === 8 ), "the suit is off" );
	const bare = speak( dressed, ++now, "bare", "Local" );
	assert.equal( bare.length, 1, "the unsuited line rendered twice" );
	assert.equal( bare[0]?.name, "Local" );
	dressed.dispose();
});

test("travel retains a pending receipt without resending or duplicating the message", () => {
	const sent = [];
	const chat = createChat( frame => sent.push( frame ) );
	const entry = { character: { name: "Local" } };
	chat.bootstrap( entry );
	chat.request( 1, "travel now", "", 0 );
	chat.receive( globalFrame( "Local", "travel now" ), 1 );
	chat.bootstrap( entry );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	assert.equal( chat.state().lines.length, 1 );
	assert.equal( chat.state().pending, false );
	assert.equal( sent.length, 1 );
});
