/*
===========================================================================

party-chat-command.test.mjs - the party's chat commands (6AD990)

"/Invite to party name", "/Banish from party name" and "/Leave the party"
parse by their localized words and order what 6AD990 would.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { partyChatCommand, partyChatOrder } = await import(
	"../../src/engine/foundation/gameplay/party-chat-command.ts"
);
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );

/** @type {Record<string, string>} */
const TEXT = {
	UIIT_STT_CHAT_COMMAND_PARTY_INVITE: "/Invite to party",
	UIIT_STT_CHAT_COMMAND_PARTY_EXPEL: "/Banish from party",
	UIIT_STT_CHAT_COMMAND_PARTY_EXIT: "/Leave the party"
};
/** @param {string} symbol */
const copy = symbol => TEXT[symbol] ?? symbol;

/**
 * @param {object} [fields]
 * @returns {any}
 */
const social = ( fields = {} ) => ({
	self: 10,
	leader: 10,
	options: 0,
	members: [ { id: 10, name: "Me" }, { id: 11, name: "Friend" } ],
	...fields
});

test("the English multi-word commands parse, with the rest of the line as the name", () => {
	assert.deepEqual( partyChatCommand( "/Invite to party Friend", copy ), { kind: "invite", name: "Friend" } );
	assert.deepEqual( partyChatCommand( " /invite TO party  Two Words ", copy ), {
		kind: "invite",
		name: "Two Words"
	} );
	assert.deepEqual( partyChatCommand( "/Banish from party Friend", copy ), { kind: "banish", name: "Friend" } );
	assert.deepEqual( partyChatCommand( "/Leave the party", copy ), { kind: "leave" } );
});

test("other lines are not party commands", () => {
	for (
		const line of [
			"Invite to party Friend",
			"/Invite to party",
			"/Invite to partyFriend",
			"/Leave the party now",
			"hello"
		]
	) {
		assert.equal( partyChatCommand( line, copy ), null, line );
	}
});

test("leave orders 0x704F only inside a party", () => {
	assert.deepEqual( partyChatOrder( { kind: "leave" }, social(), 0 ), { kind: "party-leave" } );
	assert.equal( partyChatOrder( { kind: "leave" }, social( { leader: 0, members: [] } ), 0 ), null );
});

test("banish needs the leader and another member of that name", () => {
	assert.deepEqual( partyChatOrder( { kind: "banish", name: "Friend" }, social(), 0 ), {
		kind: "party-kick",
		id: 11
	} );
	assert.equal( partyChatOrder( { kind: "banish", name: "Friend" }, social( { leader: 11 } ), 0 ), null );
	assert.equal( partyChatOrder( { kind: "banish", name: "Stranger" }, social(), 0 ), null );
	assert.equal( partyChatOrder( { kind: "banish", name: "Me" }, social(), 0 ), null );
});

test("invite is open outside a party, to the leader, or to anyone when the party allows it", () => {
	const invite = /** @type {const} */ ({ kind: "invite", name: "Other" });
	assert.deepEqual( partyChatOrder( invite, social( { leader: 0, members: [] } ), 3 ), {
		kind: "party-invite-named",
		name: "Other",
		options: 3
	} );
	assert.deepEqual( partyChatOrder( invite, social( { options: 1 } ), 3 ), {
		kind: "party-invite-named",
		name: "Other",
		options: 1
	} );
	assert.equal( partyChatOrder( invite, social( { leader: 11 } ), 0 ), null );
	assert.equal( partyChatOrder( invite, social( { leader: 11, options: 4 } ), 0 )?.kind, "party-invite-named" );
});

test("the world core sends nothing for a named player who is not in view", () => {
	const sent = [], core = createWorldCore( frame => sent.push( frame ) );
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1907, startProfile: { regionId: 257, x: 0, y: 0, z: 0, angle: 0 } }
	} );
	core.receive( { opcode: 0x32a6, payload: Uint8Array.of( 1, 0, 0, 0, 0, 0, 0, 0 ) }, 0 );
	sent.length = 0;
	core.command( { kind: "party-invite-named", name: "Nobody", options: 0 }, 0 );
	assert.deepEqual( sent, [] );
	core.dispose();
});
