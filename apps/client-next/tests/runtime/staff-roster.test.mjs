/*
===========================================================================

staff-roster.test.mjs - staff state and roster (port-only 0x3FCC/0x3FCD)

The decoders take the server's frames whole or not at all; a rostered
player's name takes its role's colour and tag ahead of the native "[GM]"
rule, a job alias is never tagged, and only public lines change colour.
The byte layouts are the server's encoders (apps/server/internal/game/
action/staff.go).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const {
	OP_STAFF_STATE,
	OP_STAFF_ROSTER,
	decodeStaffState,
	decodeStaffRoster,
	staffBoardName,
	staffChatColor,
	staffEntry
} = await import( "../../src/engine/foundation/gameplay/staff-roster.ts" );
const { refreshNameColor, NAME_COLOR_GM } = await import( "../../src/engine/foundation/gameplay/name-color.ts" );

/*
================
rosterFrame

The server's 0x3FCD body for these entries.
================
*/
function rosterFrame( entries ) {
	const bytes = [ entries.length & 255, entries.length >> 8 ];
	const text = value => {
		const encoded = [ ...new TextEncoder().encode( value ) ];
		bytes.push( encoded.length & 255, encoded.length >> 8, ...encoded );
	};
	const colour = value => bytes.push( value & 255, (value >>> 8) & 255, (value >>> 16) & 255, value >>> 24 );
	for ( const entry of entries ) {
		text( entry.name );
		text( entry.tag );
		colour( entry.nameColor );
		colour( entry.chatColor );
	}
	return new Uint8Array( bytes );
}

const gm = { name: "Hundred", tag: "[GM]", nameColor: 0xffffd87a, chatColor: 0xffffaec3 };
const mod = { name: "Helper", tag: "[MOD]", nameColor: 0xff7ad8ff, chatColor: 0xffc0f0ff };

test("the frames are 0x3FCC and 0x3FCD", () => {
	assert.equal( OP_STAFF_STATE, 0x3fcc );
	assert.equal( OP_STAFF_ROSTER, 0x3fcd );
});

test("the staff state is five bytes: gm then the capability bits", () => {
	assert.deepEqual( decodeStaffState( new Uint8Array( [ 1, 3, 0, 0, 0 ] ) ), { gm: true, capabilities: 3 } );
	assert.deepEqual( decodeStaffState( new Uint8Array( [ 0, 0, 0, 0, 0 ] ) ), { gm: false, capabilities: 0 } );
	for ( const bad of [ [ 1, 3, 0, 0 ], [ 2, 0, 0, 0, 0 ], [ 1, 0, 0, 0, 0, 0 ] ] ) {
		assert.throws( () => decodeStaffState( new Uint8Array( bad ) ), /staff state/ );
	}
});

test("the roster decodes whole, UTF-8 tags included, and refuses a partial one", () => {
	const star = { ...mod, tag: "[★]" };
	assert.deepEqual( decodeStaffRoster( rosterFrame( [ gm, star ] ) ), [ gm, star ] );
	assert.deepEqual( decodeStaffRoster( new Uint8Array( [ 0, 0 ] ) ), [] );
	const whole = rosterFrame( [ gm ] );
	assert.throws( () => decodeStaffRoster( whole.subarray( 0, whole.length - 1 ) ), /staff roster/ );
	assert.throws( () => decodeStaffRoster( new Uint8Array( [ ...whole, 0 ] ) ), /staff roster/ );
});

test("a rostered player's name takes its role colour ahead of the native [GM] rule", () => {
	const local = { gid: 1, kind: "local-player", name: "Local", arenaTeam: 255 };
	const context = /** @type {import("../../src/engine/foundation/gameplay/name-color.ts").NameColorContext} */ (
		/** @type {unknown} */ ({
			local,
			social: { localName: "Local", self: 1, leader: 0, members: [], guild: null },
			fortress: { worldId: 1, worlds: [], fortresses: [], wars: [], registered: [], listId: 0 },
			capeTeam: () => 1,
			staffRoster: [ gm, mod ]
		})
	);
	// Partial fixtures: the colour rule reads only these fields.
	const peer = (
		/** @type {string} */ name
	) => /** @type {import("../../src/engine/contracts/world.ts").EntityState} */ (
		/** @type {unknown} */ ({ gid: 2, kind: "player", name, arenaTeam: 255 })
	);
	assert.equal( refreshNameColor( peer( "helper" ), context ), mod.nameColor );
	assert.equal( refreshNameColor( peer( "Hundred" ), context ), gm.nameColor );
	// The native rule still holds for a name the roster does not carry.
	assert.equal( refreshNameColor( peer( "[GM]Old" ), context ), NAME_COLOR_GM );
	assert.equal( refreshNameColor( peer( "Plain" ), { ...context, staffRoster: [] } ), 0xffffffff );
	// A job alias hides who wears it: no role colour.
	assert.notEqual( refreshNameColor( { ...peer( "Helper" ), boardName: "Hunter" }, context ), mod.nameColor );
});

test("the board shows the tag before a player's name, never on an alias or a monster", () => {
	const roster = [ gm ];
	assert.equal( staffBoardName( { kind: "player", name: "Hundred" }, "Hundred", roster ), "[GM]Hundred" );
	assert.equal( staffBoardName( { kind: "local-player", name: "hundred" }, "hundred", roster ), "[GM]hundred" );
	assert.equal( staffBoardName( { kind: "player", name: "Hundred", boardName: "X" }, "X", roster ), "X" );
	assert.equal( staffBoardName( { kind: "monster", name: "Hundred" }, "Hundred", roster ), "Hundred" );
	assert.equal( staffBoardName( { kind: "player", name: "Other" }, "Other", roster ), "Other" );
	assert.equal( staffBoardName( { kind: "player", name: "Hundred" }, "Hundred", [] ), "Hundred" );
	assert.equal( staffEntry( roster, "HUNDRED" ), gm );
});

test("only a staff sender's public lines take the role's chat colour", () => {
	const pink = [ 0xff / 255, 0xae / 255, 0xc3 / 255, 1 ];
	assert.deepEqual( staffChatColor( [ gm ], 1, "Hundred" ), pink );
	assert.deepEqual( staffChatColor( [ gm ], 3, "hundred" ), pink );
	assert.deepEqual( staffChatColor( [ gm ], 6, "Hundred" ), pink, "global is public chat on this shard" );
	for ( const channel of [ 2, 4, 5, 7, 11 ] ) assert.equal( staffChatColor( [ gm ], channel, "Hundred" ), undefined );
	assert.equal( staffChatColor( [ gm ], 1, "Other" ), undefined );
	assert.equal( staffChatColor( undefined, 1, "Hundred" ), undefined );
});
