/*
===========================================================================

avatar-socket.test.mjs - which socket and storage slot an avatar item takes

The four v1.150 avatar sockets by TID4, and the Devil's Spirit's fifth
socket (port-only, SRO_NASRUN, #3456) with storage slot 4 to itself.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const socket = await import( "../../src/engine/foundation/gameplay/avatar-socket.ts" );

/*
================
typeFlags

The packed type word the item rows carry.
================
*/
function typeFlags( tid1, tid2, tid3, tid4 ) {
	return tid1 << 2 | tid2 << 5 | tid3 << 7 | tid4 << 11;
}

const HAT = typeFlags( 3, 1, 13, 1 );
const DRESS = typeFlags( 3, 1, 13, 2 );
const ATTACHMENT = typeFlags( 3, 1, 13, 3 );
const FLAG = typeFlags( 3, 1, 13, 4 );
const SPIRIT = typeFlags( 3, 1, 14, 1 );
const SWORD = typeFlags( 3, 1, 6, 2 );

test("the four v1.150 avatars socket by TID4", () => {
	assert.deepEqual( [ HAT, DRESS, ATTACHMENT, FLAG ].map( socket.avatarSocket ), [ 1, 2, 3, 4 ] );
	for ( const item of [ HAT, DRESS, ATTACHMENT, FLAG ] ) assert.equal( socket.isAvatarItem( item ), true );
});

test("a Spirit takes the fifth socket, not the hat's, though it shares the hat's TID4", () => {
	assert.equal( socket.isAvatarItem( SPIRIT ), true );
	assert.equal( socket.avatarSocket( SPIRIT ), socket.SPIRIT_SOCKET );
	assert.notEqual( socket.avatarSocket( SPIRIT ), socket.avatarSocket( HAT ) );
});

test("anything else has no socket", () => {
	assert.equal( socket.isAvatarItem( SWORD ), false );
	assert.equal( socket.avatarSocket( SWORD ), 0 );
	assert.equal( socket.avatarSocket( typeFlags( 3, 1, 13, 5 ) ), 0 );
});

test("storage slot 4 holds only the Spirit, and the Spirit only slot 4", () => {
	for ( let slot = 0; slot < socket.AVATAR_STORAGE_CAPACITY; slot++ ) {
		assert.equal( socket.avatarSlotFits( HAT, slot ), slot < socket.AVATAR_STORAGE_SLOTS, `hat ${slot}` );
		assert.equal( socket.avatarSlotFits( SPIRIT, slot ), slot === socket.SPIRIT_SLOT, `spirit ${slot}` );
	}
	assert.equal( socket.avatarSlotFits( HAT, -1 ), false );
	assert.equal( socket.avatarSlotFits( SPIRIT, 4.5 ), false );
});

test("only a never-used Spirit is dormant: its use wakes it before it can be worn (#4364)", () => {
	// A Spirit's data word is its expiry in unix seconds once woken.
	const woken = 1764000000;
	assert.equal( socket.isDormantSpirit( SPIRIT, 0 ), true );
	assert.equal( socket.isDormantSpirit( SPIRIT, woken ), false );
	// A lapsed Spirit keeps its word: it is not dormant and takes no second use.
	assert.equal( socket.isDormantSpirit( SPIRIT, 1 ), false );
	// No v1.150 avatar is ever dormant, whatever its durability field holds.
	for ( const item of [ HAT, DRESS, ATTACHMENT, FLAG, SWORD ] ) {
		assert.equal( socket.isDormantSpirit( item, 0 ), false );
	}
});
