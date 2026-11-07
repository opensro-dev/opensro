/*
===========================================================================

chat-item-links-worker.test.mjs - the worker sends and receives item links

Port-only, not native (SRO_CHAT_ITEM_LINKS). The chat owner decodes the
link tail on player lines and replayed history, drops a malformed tail
without failing the session, sends only the links it can read, and keeps
its own line single when the server's echo trimmed a link. The Global
Chatting use carries the same block.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { createChat } = await import( "../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { CHAT_LINK_MARKER } = await import( "../../src/engine/foundation/gameplay/chat-item-links.ts" );

const bytes = hex => Uint8Array.from( hex.split( " " ).map( b => parseInt( b, 16 ) ) );
const M = CHAT_LINK_MARKER;
// Only the fields the chat owner passes through; typed loosely for the stubs.
const POTION =
	/** @type {any} */ ({ slot: 13, refObjId: 0x01020304, typeFlags: 0, quantity: 3, plus: 0, name: "Potion" });
// One linked body: the notice tests' ETC potion.
const TAIL = bytes( "01 06 00 04 03 02 01 03 00" );

/*
================
wide

u16 count + UTF-16LE text, as the 0x3667 body carries it.
================
*/
function wide( text ) {
	const p = new Uint8Array( 2 + text.length * 2 ), v = new DataView( p.buffer );
	v.setUint16( 0, text.length, true );
	for ( let i = 0; i < text.length; i++ ) v.setUint16( 2 + i * 2, text.charCodeAt( i ), true );
	return p;
}

/*
================
named

A name-authored 0x3667 body, then any tail.
================
*/
function named( channel, name, text, tail = new Uint8Array( 0 ) ) {
	const head = Uint8Array.of( channel, name.length, 0, ...[ ...name ].map( c => c.charCodeAt( 0 ) ) );
	return Uint8Array.from( [ ...head, ...wide( text ), ...tail ] );
}

/*
================
owner

A chat owner whose resolvers answer the potion.
================
*/
/**
 * @param {any[]} [sent]
 * @param {(ref: any) => any} [linked]
 */
function owner( sent = [], linked = () => POTION ) {
	const chat = createChat( frame => sent.push( frame ), () => POTION, linked );
	chat.bootstrap( { character: { name: "Local" }, chatItemLinks: 1 } );
	return chat;
}

test("a player line decodes its item link", () => {
	const chat = owner();
	const all = Uint8Array.from( [ 1, 2, 0, 0, 0, ...wide( "look " + M ), ...TAIL ] );
	assert.equal( chat.receive( { opcode: 0x3667, payload: all }, 1 ), true );
	assert.deepEqual( defined( chat.state().lines.at( -1 ) ).links, [ POTION ] );
	chat.receive( { opcode: 0x3667, payload: named( 4, "Berk", M, TAIL ) }, 1 );
	assert.deepEqual( defined( chat.state().lines.at( -1 ) ).links, [ POTION ] );
});

test("a malformed player tail is a plain line", () => {
	const chat = owner();
	chat.receive( { opcode: 0x3667, payload: named( 4, "Berk", "x", bytes( "01 06 00 04" ) ) }, 1 );
	const line = defined( chat.state().lines.at( -1 ) );
	assert.equal( line.text, "x" );
	assert.equal( line.links, undefined );
});

test("a stall line with a tail still throws as today", () => {
	const chat = owner();
	assert.throws( () => chat.receive( { opcode: 0x3667, payload: named( 9, "Berk", "x", TAIL ) }, 1 ), /trailing/ );
});

test("history keeps the links", () => {
	const chat = owner();
	const line = named( 6, "Berk", "wts " + M, TAIL );
	const p = new Uint8Array( 2 + 10 + line.length ), v = new DataView( p.buffer );
	p[0] = 2;
	p[1] = 1;
	v.setBigUint64( 2, 1_790_000_000_000n, true );
	v.setUint16( 10, line.length, true );
	p.set( line, 12 );
	chat.receive( { opcode: 16, payload: p }, 1 );
	const got = defined( chat.state().lines.at( -1 ) );
	assert.equal( got.history, true );
	assert.deepEqual( got.links, [ POTION ] );
});

test("a sent line carries its refs and echoes its items", () => {
	const sent = [];
	const chat = owner( sent );
	chat.request( 1, "x" + M, "", 0, [ { source: 0, slot: 13 } ] );
	assert.deepEqual( sent[0].payload, Uint8Array.from( [ 1, 255, ...wide( "x" + M ), ...bytes( "01 00 0D 00" ) ] ) );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	const own = defined( chat.state().lines.at( -1 ) );
	assert.equal( own.outgoing, true );
	assert.deepEqual( own.links, [ POTION ] );
});

test("an unreadable ref sends today's frame", () => {
	const sent = [];
	const chat = owner( sent, () => undefined );
	chat.request( 1, "x" + M, "", 0, [ { source: 0, slot: 13 } ] );
	assert.deepEqual( sent[0].payload, Uint8Array.from( [ 1, 255, ...wide( "x" ) ] ) );
});

test("the switch off leaves the line and its markers native", () => {
	const sent = [];
	const chat = createChat( frame => sent.push( frame ), () => POTION, () => POTION );
	chat.bootstrap( { character: { name: "Local" } } );
	chat.request( 1, "x" + M, "", 0, [ { source: 0, slot: 13 } ] );
	assert.deepEqual( sent[0].payload, Uint8Array.from( [ 1, 255, ...wide( "x" + M ) ] ) );
});

test("the beta echo matches a trimmed line", () => {
	const chat = owner();
	chat.request( 1, "x" + M, "", 0, [ { source: 0, slot: 13 } ] );
	chat.receive( { opcode: 0x3667, payload: named( 6, "Local", "x" ) }, 1 );
	chat.receive( { opcode: 0xb367, payload: Uint8Array.of( 1, 1, 255 ) }, 1 );
	const lines = chat.state().lines;
	assert.equal( lines.length, 1 );
	assert.equal( lines[0].outgoing, true );
});

test("the Global Chatting use appends its refs", () => {
	const GLOBAL_CHATTING = 3 << 2 | 3 << 5 | 3 << 7 | 5 << 11;
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( {
		chatItemLinks: 1,
		refItemSnapshot: [ { refObjId: 3851, typeFlags: GLOBAL_CHATTING } ],
		equipItems: [ { refObjId: 3851, slot: 21, body: [ 0x0b, 0x0f, 0, 0, 3, 0 ] } ]
	} );
	gameplay.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Author"
	} );
	gameplay.command(
		{ kind: "item-use", slot: 21, message: "wts " + M, links: [ { source: 0, slot: 21 } ] },
		1,
		undefined
	);
	assert.deepEqual( sent.at( -1 ).payload.slice( -4 ), bytes( "01 00 15 00" ) );
});
