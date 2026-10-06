/*
===========================================================================

guild-manager.test.mjs - the guild manager's rows, wire and warehouse

The row order follows 5D9100, the requests 5DA1B0 and the warehouse road
75AE50 -> 7682F0; expectations are worked from those, not read back.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { guildManagerRows, guildLevelUpPrice } = await import( "../../src/engine/foundation/gameplay/guild-manager.ts" );
const { socialPacket, socialRequest, emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { createStorageRoom, storageMoveRequest, STORAGE_GOLD_DEPOSIT, STORAGE_MOVE_DEPOSIT } = await import(
	"../../src/engine/foundation/gameplay/storage-room.ts"
);

const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const member = ( id, name, grade ) => ({
	id,
	name,
	grade,
	level: 1,
	donated: 0,
	permissions: 0,
	grant: "",
	model: 0,
	role: 0,
	offline: 0
});
const guild = {
	id: 9,
	name: "Lanterns",
	level: 2,
	gp: 0,
	subject: "",
	contents: "",
	members: [ member( 1, "Master", 0 ), member( 2, "Member", 10 ) ]
};

/*
================
packet

The social owner's next state for a frame it must claim.
================
*/
/** @returns {any} */
const packet = ( state, opcode, payload ) => {
	const next = socialPacket( state, { opcode, payload } );
	if ( !next ) throw Error( "unclaimed social frame" );
	return next;
};

/*
================
rows
================
*/
const rows = ( g, name ) => guildManagerRows( 0x4000, g, name ).map( r => r.row );

test("the guild set follows 5D9100's order for each standing", () => {
	assert.deepEqual( rows( null, "Stranger" ), [ "create" ] );
	assert.deepEqual( rows( guild, "Master" ), [
		"level-up",
		"dissolve",
		"master-leave",
		"compensation",
		"release",
		"warehouse"
	] );
	assert.deepEqual( rows( guild, "Member" ), [ "secede", "release", "warehouse" ] );
	const voting = { ...guild, votes: [ { id: 7, kind: 0, remainingMs: 60000 } ] };
	assert.deepEqual( rows( voting, "Member" ), [ "secede", "vote", "warehouse" ] );
	assert.deepEqual( guildManagerRows( 0x1, guild, "Master" ), [] );
});

test("the level-up window prices the next level and stops at five", () => {
	assert.deepEqual( guildLevelUpPrice( 1 ), { gp: 5400, gold: 3000000 } );
	assert.deepEqual( guildLevelUpPrice( 4 ), { gp: 378000, gold: 21000000 } );
	assert.equal( guildLevelUpPrice( 5 ), undefined );
});

test("the manager's requests carry the NPC first", () => {
	const state = { ...emptySocial( "Master" ), guild };
	assert.deepEqual( socialRequest( state, { kind: "guild-level-up", gid: 300 } ), {
		opcode: 0x73f0,
		payload: Uint8Array.of( ...u32( 300 ) )
	} );
	assert.deepEqual( socialRequest( state, { kind: "guild-master-leave", gid: 300, id: 2 } ), {
		opcode: 0x77d4,
		payload: Uint8Array.of( ...u32( 300 ), ...u32( 2 ) )
	} );
	assert.deepEqual( socialRequest( state, { kind: "guild-vote", gid: 300, vote: 7, option: 1 } ), {
		opcode: 0x7330,
		payload: Uint8Array.of( ...u32( 300 ), ...u32( 7 ), 1 )
	} );
	assert.equal( socialRequest( state, { kind: "guild-compensation-claim", gid: 300 } ).opcode, 0x73f7 );
});

test("votes open, close and name the outcome", () => {
	/** @type {any} */
	let state = { ...emptySocial( "Member" ), guild };
	state = packet( state, 0x3a6c, Uint8Array.of( 1, ...u32( 7 ), 0, ...u32( 600000 ) ) );
	assert.deepEqual( state.guild.votes, [ { id: 7, kind: 0, remainingMs: 600000 } ] );
	state = packet( state, 0x3a6c, Uint8Array.of( 3, ...u32( 7 ), 1, ...u32( 2 ) ) );
	assert.deepEqual( state.guild.votes, [] );
	assert.equal( state.notice.key, "UIIT_MSG_MRELEASE_BEELETED" );
	assert.deepEqual( state.notice.arguments, [ "Member", "Lanterns" ] );
	state = packet( state, 0xb6dc, Uint8Array.of( 2, 0x33 ) );
	assert.equal( state.notice.key, "UIIT_MSG_MRELEASEERR_NOTVOTETIME" );
});

test("the compensation quote waits for the claim box", () => {
	/** @type {import("../../src/engine/foundation/gameplay/social.ts").SocialState} */
	let state = { ...emptySocial( "Master" ), guild };
	state = packet( state, 0xb140, Uint8Array.of( 1, ...u32( 70000 ) ) );
	assert.equal( state.compensation, 70000 );
	state = packet( state, 0xb3f7, Uint8Array.of( 1 ) );
	assert.equal( state.compensation, undefined );
});

test("the guild warehouse claims, lists and releases its room", () => {
	const sent = [], room = createStorageRoom( frame => sent.push( frame ) );
	room.openGuild( 300 );
	assert.deepEqual( sent.pop(), { opcode: 0x7338, payload: Uint8Array.of( ...u32( 300 ), ...u32( 0x4000 ) ) } );
	room.receive( { opcode: 0xb338, payload: Uint8Array.of( 1, ...u32( 0x4000 ) ) }, new Map() );
	assert.deepEqual( sent.pop(), { opcode: 0x7515, payload: Uint8Array.of( ...u32( 300 ) ) } );
	room.receive( { opcode: 0xb515, payload: Uint8Array.of( 1 ) }, new Map() );
	assert.deepEqual( sent.pop(), { opcode: 0x733d, payload: Uint8Array.of( ...u32( 300 ) ) } );
	room.receive( { opcode: 0x34a9, payload: Uint8Array.of( 0x10, 0x27, 0, 0, 0, 0, 0, 0 ) }, new Map() );
	room.receive( { opcode: 0x3363, payload: Uint8Array.of( 30, 0 ) }, new Map() );
	room.receive( { opcode: 0xb33d, payload: Uint8Array.of( 1 ) }, new Map() );
	assert.deepEqual( room.state(), { npc: 300, guild: true, phase: "open", capacity: 30, gold: "10000", items: [] } );
	assert.equal(
		storageMoveRequest( 300, { type: STORAGE_GOLD_DEPOSIT, source: 0, destination: 0, quantity: 0, gold: 5 }, true )
			.payload[0],
		0x20
	);
	assert.equal(
		storageMoveRequest(
			300,
			{ type: STORAGE_MOVE_DEPOSIT, source: 13, destination: 0, quantity: 0, gold: 0 },
			true
		).payload[0],
		0x1e
	);
	room.close();
	assert.deepEqual( sent.pop(), { opcode: 0x7428, payload: Uint8Array.of( ...u32( 300 ) ) } );
});

test("a warehouse in another member's hands releases the room", () => {
	const room = createStorageRoom( () => {} );
	room.openGuild( 300 );
	room.receive( { opcode: 0xb338, payload: Uint8Array.of( 1, ...u32( 0x4000 ) ) }, new Map() );
	assert.equal(
		room.receive( { opcode: 0xb515, payload: Uint8Array.of( 2, 0x48, 3, 0, 65, 66, 67 ) }, new Map() ),
		false
	);
	assert.equal( room.state(), null );
	const state = packet( { ...emptySocial( "Master" ), guild }, 0xb515, Uint8Array.of( 2, 0x48, 3, 0, 65, 66, 67 ) );
	assert.equal( state.notice.key, "UIIT_MSG_GUILD_WAREHOUSE_USE" );
	assert.deepEqual( state.notice.arguments, [ "ABC" ] );
});

/*
================
Soldier attribute wire and visible-menu lifetime
================
*/
test("soldier attribute deltas preserve flags and refresh only an already visible NPC", async () => {
	const { createGuildManagerHud } = await import( "../../src/engine/runtime/ui/hud/guild-manager-hud.ts" );
	const { guildSoldierRows, guildSoldierRefusal, guildSoldierPrompt } = await import(
		"../../src/engine/foundation/gameplay/guild-manager.ts"
	);
	/** @type {import("../../src/engine/foundation/gameplay/social.ts").SocialState} */
	let state = { ...emptySocial( "Master" ), guild };
	const hud = createGuildManagerHud();
	hud.observeSoldiers( 4001, 0 );
	assert.equal( hud.soldiers(), false );
	const request = socialRequest( state, { kind: "guild-soldier-attribute", gid: 4001, attribute: 8 } );
	assert.equal( request.opcode, 0x7322 );
	assert.deepEqual( [ ...request.payload ], [ ...u32( 4001 ), 8 ] );
	state = packet( state, 0xb322, new Uint8Array( [ 1, 8 ] ) );
	assert.equal( state.guild?.flags, 8 );
	hud.observeSoldiers( 4001, state.soldierAttributeSequence ?? 0 );
	assert.equal( hud.soldiers(), true );
	state = packet( state, 0x3b29, new Uint8Array( [ 5, 64, 2 ] ) );
	assert.equal( state.guild?.flags, 10 );
	assert.equal( state.soldierAttributeSequence, 2 );
	assert.equal( guildSoldierRefusal( 10, 2 ), "UIIT_MSG_GUILD_SOLDIER_ABILITY_SELECT_ERROR" );
	assert.equal( guildSoldierRefusal( 10, 4 ), "UIIT_MSG_GUILD_SOLDIER_ABILITY_OVER" );
	assert.equal( guildSoldierRefusal( 10, 0 ), undefined );
	state = packet( state, 0xb322, new Uint8Array( [ 1, 0 ] ) );
	assert.equal( state.guild?.flags, 0 );
	hud.observeSoldiers( undefined, state.soldierAttributeSequence ?? 0 );
	hud.observeSoldiers( 4001, state.soldierAttributeSequence ?? 0 );
	assert.equal( hud.soldiers(), false );
	assert.equal( guildSoldierRefusal( 0, 0 ), "UIIT_MSG_GUILD_SOLDIER_ABILITY_INITIALIZE_ERROR" );
	assert.deepEqual(
		guildSoldierRows().map( row => row.id ),
		[ 1, 2, 4, 8, 0 ].map( bit => "npc-guild-soldier:" + bit )
	);
	assert.equal( guildSoldierPrompt( 0, key => key ), "UIIT_MSG_GUILD_SOLDIER_ABILITY_ZERO" );
});
