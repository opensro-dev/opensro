/*
===========================================================================

guild-union.test.mjs - the union proposal, requests and union tab rules

0x3393 kind 6 (7644E0 case 6) and its {01 01} / {02 00} answers (6971B0
case 0x1A), the 0x7379 / 0x7795 / 0x7680 / 0x744E requests, the category
0x10 result notices, and CIFAllianceGuild_RefreshButtons (5F6880).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const social = await import( "../../src/engine/foundation/gameplay/social.ts" );
const alliance = await import( "../../src/engine/foundation/ui/alliance-guild.ts" );
const { createUnionHud } = await import( "../../src/engine/runtime/ui/hud/union-hud.ts" );

/**
 * @param {import("../../src/engine/foundation/gameplay/social.ts").SocialState} state
 * @param {{ opcode: number, payload: Uint8Array }} frame
 */
const socialPacketOf = ( state, frame ) => defined( social.socialPacket( state, frame ), "social frame" );

const master = {
	id: 1,
	name: "Echo",
	grade: 0,
	level: 1,
	donated: 0,
	permissions: -1,
	grant: "",
	model: 1907,
	role: 0,
	offline: 0
};
/** @type {import("../../src/engine/foundation/gameplay/social.ts").SocialState} */
const guildState = {
	...social.emptySocial( "Echo" ),
	guild: { id: 7, name: "Alpha", level: 2, gp: 0, subject: "", contents: "", crest: 0, members: [ master ] }
};

test("a union proposal waits for the master's answer", () => {
	const prompt = { opcode: 0x3393, payload: Uint8Array.of( 6, 0x40, 0x0d, 0x03, 0 ) };
	const state = socialPacketOf( guildState, prompt );
	assert.deepEqual( state.invitation, { type: 6, gid: 0x30d40 } );
	assert.deepEqual( [ ...social.socialRequest( state, { kind: "social-consent", accept: true } ).payload ], [
		1,
		1
	] );
	assert.deepEqual( [ ...social.socialRequest( state, { kind: "social-consent", accept: false } ).payload ], [
		2,
		0
	] );
});

test("union requests carry the native bodies", () => {
	const invite = social.socialRequest( guildState, { kind: "guild-union-invite", gid: 0x30d41 } );
	assert.equal( invite.opcode, 0x7379 );
	assert.deepEqual( [ ...invite.payload ], [ 0x41, 0x0d, 0x03, 0 ] );
	const leave = social.socialRequest( guildState, { kind: "guild-union-leave" } );
	assert.equal( leave.opcode, 0x7795 );
	assert.equal( leave.payload.length, 0 );
	const kick = social.socialRequest( guildState, { kind: "guild-union-kick", id: 9 } );
	assert.equal( kick.opcode, 0x7680 );
	assert.deepEqual( [ ...kick.payload ], [ 9, 0, 0, 0 ] );
	const grant = social.socialRequest( guildState, {
		kind: "guild-permissions",
		grants: [ { id: 0x0102, permissions: 4 } ]
	} );
	assert.equal( grant.opcode, 0x744e );
	assert.deepEqual( [ ...grant.payload ], [ 1, 2, 1, 0, 0, 4, 0, 0, 0 ] );
});

test("a refused union request raises its category 0x10 notice", () => {
	const state = socialPacketOf( guildState, { opcode: 0xb379, payload: Uint8Array.of( 2, 0x29 ) } );
	assert.equal( state.notice?.key, "UIIT_MSG_GUILDERR_ALLIANCE_FULL" );
	const accepted = socialPacketOf( guildState, { opcode: 0xb795, payload: Uint8Array.of( 1 ) } );
	assert.equal( accepted.notice, undefined );
});

test("the union tab arms its commands as 5F6880", () => {
	assert.deepEqual( alliance.allianceButtons( guildState ), { invite: true, exit: false, expel: false } );
	const rows = [
		{ id: 7, name: "Alpha", level: 2, master: "Echo", model: 1907, flags: 1 },
		{ id: 8, name: "Bravo", level: 3, master: "Fox", model: 1907, flags: 4 }
	];
	const leading = { ...guildState, alliances: rows, allianceMaster: 7 };
	assert.deepEqual( alliance.allianceButtons( leading ), { invite: true, exit: true, expel: true } );
	const follower = { ...leading, allianceMaster: 8 };
	assert.deepEqual( alliance.allianceButtons( follower ), { invite: false, exit: true, expel: false } );
	const member = { ...leading, localName: "Golf" };
	assert.deepEqual( alliance.allianceButtons( member ), { invite: false, exit: false, expel: false } );
	assert.equal( alliance.allianceLeader( follower )?.name, "Bravo" );
});

test("the union tab keeps one question and its sort order", () => {
	const hud = createUnionHud();
	hud.ask( { kind: "exit", guild: 0, name: "" } );
	hud.ask( { kind: "expel", guild: 8, name: "Bravo" } );
	assert.equal( hud.question()?.kind, "exit" );
	assert.equal( hud.answer()?.kind, "exit" );
	assert.equal( hud.question(), null );
	const rows = [ { name: "B", level: 1 }, { name: "A", level: 3 } ];
	assert.deepEqual( hud.order( rows ).map( r => r.name ), [ "A", "B" ] );
	hud.sortBy( "level" );
	assert.deepEqual( hud.order( rows ).map( r => r.name ), [ "B", "A" ] );
	hud.sortBy( "level" );
	assert.deepEqual( hud.order( rows ).map( r => r.name ), [ "A", "B" ] );
});

const { createGrantPowerHud } = await import( "../../src/engine/runtime/ui/hud/grant-power-hud.ts" );

test("the rights panel drafts changes and caps union chat at twelve", () => {
	const hud = createGrantPowerHud();
	const members = Array.from( { length: 14 }, ( _, i ) => ({
		id: i + 1,
		name: "m" + i,
		grade: i === 0 ? 0 : 10,
		permissions: i <= 12 && i > 0 ? 4 : 0
	}) );
	hud.open( members );
	assert.equal( hud.visible().length, 5 );
	assert.ok( hud.visible().every( r => r.row.grade !== 0 ) );
	hud.toggle( 14, 4 );
	assert.deepEqual( hud.grants(), [], "a thirteenth union chat holder" );
	hud.toggle( 2, 4 );
	hud.toggle( 14, 4 );
	hud.toggle( 14, 1 );
	assert.deepEqual( hud.grants(), [ { id: 2, permissions: 0 }, { id: 14, permissions: 5 } ] );
	hud.close();
	assert.equal( hud.isOpen(), false );
});
