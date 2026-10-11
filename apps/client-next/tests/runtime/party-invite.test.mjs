/*
===========================================================================

party-invite.test.mjs - who a party invitation goes to, and the proposer's line

6FD830 (0x70D5) and 6FDA50 (0x751A) invite a player, a ridden horse's rider
for the horse, and nothing else; both print UIIT_STT_PARTY_PROPOSAL_PENDING
with the invitee's name before sending.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { partyInviteTarget, partyProposalPendingNotice } = await import(
	"../../src/engine/foundation/gameplay/party-invite.ts"
);
const { createWorldCore } = await import( "../../src/engine/runtime/simulation/worker/session/world/core.ts" );

const LOCAL = 1;
/** @param {object} fields */
const row = fields => ({ refObjId: 1907, regionId: 0x62aa, x: 0, y: 0, z: 0, heading: 0, ...fields });
const rows = [
	row( { gid: 1, kind: "local-player", name: "Me" } ),
	row( { gid: 2, kind: "player", name: "Friend" } ),
	row( { gid: 3, kind: "cos", name: "Horse" } ),
	row( { gid: 4, kind: "player", name: "Rider", mountedOn: 3 } ),
	row( { gid: 5, kind: "cos", name: "Empty horse" } ),
	row( { gid: 6, kind: "monster", name: "Wolf" } ),
	row( { gid: 7, kind: "npc", name: "Guard" } )
];
const lookup = {
	entity: gid => rows.find( entity => entity.gid === gid ),
	rider: gid => rows.find( entity => entity.mountedOn === gid )
};

test("a player is invited as selected", () => {
	assert.equal( partyInviteTarget( 2, LOCAL, lookup )?.gid, 2 );
});

test("a ridden horse invites its rider", () => {
	assert.equal( partyInviteTarget( 3, LOCAL, lookup )?.gid, 4 );
});

test("the local player, monsters, NPCs, empty horses and unknown gids invite nobody", () => {
	for ( const gid of [ 1, 5, 6, 7, 99 ] ) assert.equal( partyInviteTarget( gid, LOCAL, lookup ), null, `gid ${gid}` );
});

test("the proposer's line names the invitee on the party channel", () => {
	assert.deepEqual( partyProposalPendingNotice( "Friend" ), {
		key: "UIIT_STT_PARTY_PROPOSAL_PENDING",
		value: 0,
		arguments: [ "Friend" ],
		nativeType: 4
	} );
});

test("the world core sends no invitation for a target that is not a player", () => {
	const sent = [], core = createWorldCore( frame => sent.push( frame ) );
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [],
		localPlayerEntry: { modelRef: 1907, startProfile: { regionId: 257, x: 0, y: 0, z: 0, angle: 0 } }
	} );
	core.receive( { opcode: 0x32a6, payload: Uint8Array.of( LOCAL, 0, 0, 0, 0, 0, 0, 0 ) }, 0 );
	sent.length = 0;
	core.command( { kind: "party-invite", gid: 99, options: 0 }, 0 );
	core.command( { kind: "party-invite", gid: LOCAL, options: 0 }, 0 );
	assert.deepEqual( sent.filter( frame => frame.opcode === 0x70d5 || frame.opcode === 0x751a ), [] );
	core.dispose();
});
