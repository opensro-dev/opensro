/*
===========================================================================

academy-invite.test.mjs - the academy window's Invite button (BR-261125-1018-4609)

The leader of an academy invites the selected player: the window's
Invite button (CIFApprenticeShip_OnInviteBtn 5C4FD0) sends 0x76B1 with
the target (702C90), and CIFApprenticeShip_RefreshContents 5C5EF0
enables it for the leader only. The port drew the button greyed for
everyone and sent nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

const LOCAL_GID = 1, PEER_GID = 7;

/*
================
camp

An academy whose roster holds the local player (member 1) as kind
leader (0) or student (2), and one student.
================
*/
function camp( localKind ) {
	const member = ( id, kind ) => ({
		id,
		name: "M" + id,
		kind,
		level: 30,
		entryLevel: 20,
		model: 1907,
		offline: false,
		announced: true,
		honor: 0,
		location: ""
	});
	return {
		member: true,
		page: 0,
		pages: 0,
		rows: [],
		own: null,
		request: null,
		result: null,
		members: [ member( 1, localKind ), member( 2, 2 ) ],
		localMemberId: 1
	};
}

/*
================
academyWindow

A UI with the Academy window open, the local player as leader or
student, and a peer player selected. Returns the commands it sends and a
step returning the controls by id.
================
*/
function academyWindow( localKind ) {
	const commands = [], f = uiFixture( command => commands.push( command ) );
	let now = 1000, seen = null;
	const step = () => {
		for ( let i = 0; i < 20; i++ ) seen = f.ui.step( f.state, now += 50 ) ?? seen;
		return new Map( (seen?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	const local = f.state.gameplay.entities?.find( e => e.gid === f.state.gameplay.localGid );
	f.state.gameplay = {
		...f.state.gameplay,
		academy: camp( localKind ),
		target: PEER_GID
	};
	f.state.entities = [ ...(f.state.entities ?? []), {
		...(local ?? {}),
		gid: PEER_GID,
		kind: "player",
		name: "Peer",
		x: 10,
		y: 0,
		z: 10
	} ];
	step();
	f.ui.event( { kind: "activate", id: "open-window:Academy" } );
	return { f, commands, step };
}

const invites = commands => commands.map( c => c.command ).filter( c => c?.kind === "academy-invite" );

test("the leader's Invite button is live and invites the selected player (5C4FD0)", () => {
	const { f, commands, step } = academyWindow( 0 );
	try {
		const invite = step().get( "academy-invite" );
		assert.ok( invite, "the Invite button" );
		assert.notEqual( invite.disabled, true, "enabled for the leader" );
		f.ui.event( { kind: "activate", id: "academy-invite" } );
		step();
		assert.deepEqual( invites( commands ), [ { kind: "academy-invite", gid: PEER_GID } ] );
	} finally {
		f.dispose();
	}
});

test("a student's Invite button stays greyed and sends nothing (5C5EF0)", () => {
	const { f, commands, step } = academyWindow( 2 );
	try {
		const invite = step().get( "academy-invite" );
		assert.ok( invite, "the Invite button" );
		assert.equal( invite.disabled, true );
		f.ui.event( { kind: "activate", id: "academy-invite" } );
		step();
		assert.deepEqual( invites( commands ), [] );
	} finally {
		f.dispose();
	}
});

test("the worker sends 0x76B1 [u32 gid] for another player, never for oneself (702C90)", () => {
	const sent = [], g = createGameplay( frame => sent.push( frame ) );
	g.bootstrap( { academyMember: true } );
	g.seed( /** @type {any} */ ({ gid: LOCAL_GID, regionId: 1, x: 0, y: 0, z: 0, heading: 0 }) );
	g.take();
	assert.equal( g.command( { kind: "academy-invite", gid: LOCAL_GID }, 0, undefined ), null );
	assert.equal( g.command( { kind: "academy-invite", gid: 0 }, 0, undefined ), null );
	assert.equal( sent.length, 0 );
	const frame = defined( g.command( { kind: "academy-invite", gid: 0x01020304 }, 0, undefined ) );
	assert.deepEqual( sent, [ frame ] );
	assert.equal( frame.opcode, 0x76b1 );
	assert.deepEqual( [ ...frame.payload ], [ 4, 3, 2, 1 ] );
});
