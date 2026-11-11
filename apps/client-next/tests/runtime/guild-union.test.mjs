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

test("guild and union counts format current/limit as 5E2C00, 5F6FA0 and 5F72B0", () => {
	assert.deepEqual( [ 1, 2, 3, 4, 5 ].map( level => alliance.guildMemberLimit( level ) ), [ 15, 20, 25, 35, 50 ] );
	assert.equal( alliance.guildMemberCountText( 3, 3 ), "3 / 25" );
	assert.equal( alliance.guildMemberCountText( 15, 1 ), "15 / 15" );
	assert.equal( alliance.allianceGuildCountText( 2 ), "2/8" );
	assert.equal( alliance.allianceMemberCountText( 4, 3 ), "4/25" );
	assert.equal( alliance.allianceMemberCountText( 18, 5 ), "18/50" );
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

/*
================
writer

Frame builder for the guild update arms (mirrors guild-war.test.mjs).
================
*/
function writer() {
	const bytes = [];
	return {
		u8( value ) {
			bytes.push( value );
			return this;
		},
		u32( value ) {
			bytes.push( value & 255, value >>> 8 & 255, value >>> 16 & 255, value >>> 24 );
			return this;
		},
		str( value ) {
			const raw = new TextEncoder().encode( value );
			bytes.push( raw.length & 255, raw.length >>> 8, ...raw );
			return this;
		},
		frame( opcode ) {
			return { opcode, payload: Uint8Array.from( bytes ) };
		}
	};
}

/*
================
memberRow

One guildMember() row: grade 10, level 1, no war record, model 1907.
================
*/
function memberRow( w, id, name, offline ) {
	return w.u32( id ).str( name ).u8( 10 ).u8( 1 ).u32( 0 ).u32( 0xffffffff ).u32( 0 ).u32( 0 ).u32( 0 )
		.str( "" ).u32( 1907 ).u8( 0 ).u8( offline );
}

/** @type {import("../../src/engine/foundation/gameplay/social.ts").SocialState} */
const rosterState = {
	...guildState,
	guild: {
		...defined( guildState.guild, "seed guild" ),
		members: [
			{
				id: 1,
				name: "Echo",
				grade: 0,
				level: 1,
				donated: 0,
				permissions: -1,
				grant: "",
				model: 1907,
				role: 0,
				offline: 1
			},
			{
				id: 2,
				name: "Bravo",
				grade: 10,
				level: 3,
				donated: 0,
				permissions: 0,
				grant: "",
				model: 1907,
				role: 0,
				offline: 1
			},
			{
				id: 3,
				name: "Charlie",
				grade: 10,
				level: 2,
				donated: 0,
				permissions: 0,
				grant: "",
				model: 1907,
				role: 0,
				offline: 0
			}
		]
	}
};

/** @type {import("../../src/engine/foundation/gameplay/social.ts").Guild} */
const rosterGuild = defined( rosterState.guild, "roster guild" );

test("sub-op 6 mask legs follow 5E9D20: &1 is level, &2 is offline", () => {
	const leveled = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 2 ).u8( 1 ).u8( 9 ).frame( 0x3b29 )
	);
	const bravo = leveled.guild?.members.find( m => m.id === 2 );
	assert.equal( bravo?.level, 9 );
	assert.equal( bravo?.offline, 1 );
	const offlined = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 3 ).u8( 2 ).u8( 1 ).frame( 0x3b29 )
	);
	const charlie = offlined.guild?.members.find( m => m.id === 3 );
	assert.equal( charlie?.offline, 1 );
	assert.equal( charlie?.level, 2 );
});

test("member delta flips print connect, disconnect and the self notice (5F3340)", () => {
	const connected = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 2 ).u8( 2 ).u8( 0 ).frame( 0x3b29 )
	);
	assert.deepEqual(
		connected.notices?.map( n => [ n.key, n.nativeType, n.arguments ] ),
		[ [ "UIIT_MSG_GUILD_CONNECT", 0, [ "Bravo" ] ] ]
	);
	assert.equal( connected.guild?.members.find( m => m.id === 2 )?.offline, 0 );
	const noticed = {
		...rosterState,
		guild: { ...rosterGuild, subject: "Hello guild" }
	};
	const selfConnected = socialPacketOf(
		noticed,
		writer().u8( 6 ).u32( 1 ).u8( 2 ).u8( 0 ).frame( 0x3b29 )
	);
	assert.deepEqual(
		selfConnected.notices?.map( n => [ n.key, n.arguments ] ),
		[ [ "UIIT_MSG_GUILD_CONNECT_NOTICE", [ "Hello guild" ] ] ]
	);
	const silentSelf = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 1 ).u8( 2 ).u8( 0 ).frame( 0x3b29 )
	);
	assert.equal( silentSelf.notices, undefined );
	const disconnected = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 3 ).u8( 2 ).u8( 1 ).frame( 0x3b29 )
	);
	assert.deepEqual(
		disconnected.notices?.map( n => [ n.key, n.arguments ] ),
		[ [ "UIIT_MSG_GUILD_DISCONNECT", [ "Charlie" ] ] ]
	);
	const selfGone = socialPacketOf(
		{
			...rosterState,
			guild: {
				...rosterGuild,
				members: rosterGuild.members.map( m => m.id === 1 ? { ...m, offline: 0 } : m )
			}
		},
		writer().u8( 6 ).u32( 1 ).u8( 2 ).u8( 1 ).frame( 0x3b29 )
	);
	assert.equal( selfGone.notices, undefined );
	const levelOnly = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 2 ).u8( 1 ).u8( 8 ).frame( 0x3b29 )
	);
	assert.equal( levelOnly.notices, undefined );
	assert.equal( levelOnly.notice, undefined );
});

test("the 0x16 batch prints one line per flipped member", () => {
	const state = socialPacketOf(
		rosterState,
		writer().u8( 0x16 ).u8( 2 ).u8( 2 ).u32( 1 ).u8( 0 ).u32( 2 ).u8( 0 ).frame( 0x3b29 )
	);
	// Echo's own flip prints the notice-text line only when a notice
	// exists; rosterState has none, so only Bravo's connect line lands.
	assert.deepEqual(
		state.notices?.map( n => [ n.key, n.arguments ] ),
		[ [ "UIIT_MSG_GUILD_CONNECT", [ "Bravo" ] ] ]
	);
	const both = {
		...rosterState,
		guild: {
			...rosterGuild,
			members: rosterGuild.members.map( m => m.id === 1 ? { ...m, name: "Other" } : m )
		},
		localName: "Echo"
	};
	const flipped = socialPacketOf(
		both,
		writer().u8( 0x16 ).u8( 2 ).u8( 2 ).u32( 1 ).u8( 0 ).u32( 2 ).u8( 0 ).frame( 0x3b29 )
	);
	assert.deepEqual(
		flipped.notices?.map( n => n.arguments?.[0] ),
		[ "Other", "Bravo" ]
	);
});

test("a member delta for an unknown member reads its legs, skips it and degrades (#3211)", () => {
	// Mask 0x7f carries all seven legs (level, offline, grade, donated,
	// permissions, name, role); they are read by mask, not by member, so
	// the frame parses whole and only the row is skipped.
	const state = socialPacketOf(
		rosterState,
		writer().u8( 6 ).u32( 99 ).u8( 0x7f ).u8( 9 ).u8( 0 ).u8( 4 ).u32( 123 ).u32( 7 ).str( "Ghost" ).u8( 2 )
			.frame( 0x3b29 )
	);
	assert.equal( state.guild?.members.length, 3 );
	assert.equal( state.guild?.members.some( m => m.id === 99 ), false );
	assert.equal( state.notices, undefined );
	assert.equal( state.degradations?.length, 1 );
	assert.equal( state.degradations?.[0]?.code, "guild-member-delta-unknown" );
	assert.equal( state.degradations?.[0]?.frame?.opcode, 0x3b29 );
});

test("a 0x16 batch skips an unknown member and still applies the known row", () => {
	const state = socialPacketOf(
		rosterState,
		writer().u8( 0x16 ).u8( 2 ).u8( 0x7f )
			.u32( 99 ).u8( 9 ).u8( 0 ).u8( 4 ).u32( 123 ).u32( 7 ).str( "Ghost" ).u8( 2 )
			.u32( 2 ).u8( 5 ).u8( 0 ).u8( 6 ).u32( 50 ).u32( 1 ).str( "BravoTwo" ).u8( 1 )
			.frame( 0x3b29 )
	);
	const bravo = state.guild?.members.find( m => m.id === 2 );
	assert.equal( bravo?.name, "BravoTwo" );
	assert.equal( bravo?.level, 5 );
	assert.equal( state.degradations?.length, 1 );
	assert.equal( state.degradations?.[0]?.code, "guild-member-delta-unknown" );
});

test("an unknown mask bit stays fatal, for known and unknown members", () => {
	for ( const id of [ 2, 99 ] ) {
		assert.throws(
			() => social.socialPacket( rosterState, writer().u8( 6 ).u32( id ).u8( 0x80 ).frame( 0x3b29 ) ),
			/Unsupported guild member delta/
		);
	}
});

test("member removal prints by kind (762040 slot 3)", () => {
	const left = socialPacketOf( rosterState, writer().u8( 3 ).u32( 1 ).u8( 1 ).frame( 0x3b29 ) );
	assert.equal( left.notice?.key, "UIIT_MSG_GUILD_EXIT_RESULT" );
	assert.deepEqual( left.notice?.arguments, [ "Alpha" ] );
	assert.equal( left.guild, null );
	const otherLeft = socialPacketOf( rosterState, writer().u8( 3 ).u32( 2 ).u8( 1 ).frame( 0x3b29 ) );
	assert.equal( otherLeft.notice, undefined );
	assert.equal( otherLeft.guild?.members.length, 2 );
	const kicked = socialPacketOf( rosterState, writer().u8( 3 ).u32( 2 ).u8( 2 ).frame( 0x3b29 ) );
	assert.equal( kicked.notice?.key, "UIIT_MSG_GUILD_EXPULSION_RESULT" );
	assert.deepEqual( kicked.notice?.arguments, [ "Bravo" ] );
	const selfKicked = socialPacketOf( rosterState, writer().u8( 3 ).u32( 1 ).u8( 2 ).frame( 0x3b29 ) );
	assert.equal( selfKicked.notice?.key, "UIIT_MSG_GUILD_EXPULSION_RESULT" );
	assert.deepEqual( selfKicked.notice?.arguments, [ "Echo" ] );
	assert.equal( selfKicked.guild, null );
	const odd = socialPacketOf( rosterState, writer().u8( 3 ).u32( 2 ).u8( 3 ).frame( 0x3b29 ) );
	assert.equal( odd.notice, undefined );
	assert.equal( odd.guild?.members.length, 2 );
});

test("guild break prints on both channels (762040)", () => {
	const state = socialPacketOf( rosterState, writer().u8( 1 ).frame( 0x3b29 ) );
	assert.equal( state.notice?.key, "UIIT_MSG_GUILD_BREAK_RESULT" );
	assert.equal( state.notice?.nativeType, 0 );
	assert.equal( state.notice?.notificationBanner, true );
	assert.equal( state.notice?.bannerOnly, undefined );
	assert.deepEqual( state.notice?.arguments, [ "Alpha" ] );
	assert.equal( state.guild, null );
});

test("a member append prints the join line only when online (5F3340)", () => {
	const joined = socialPacketOf(
		guildState,
		memberRow( writer().u8( 2 ), 5, "Newbie", 0 ).frame( 0x3b29 )
	);
	assert.equal( joined.notice?.key, "UIIT_MSG_GUILD_JOIN_RESULT" );
	assert.deepEqual( joined.notice?.arguments, [ "Newbie" ] );
	assert.equal( joined.guild?.members.length, 2 );
	const offline = socialPacketOf(
		guildState,
		memberRow( writer().u8( 2 ), 5, "Newbie", 1 ).frame( 0x3b29 )
	);
	assert.equal( offline.notice, undefined );
	// The local player's own row never prints: native reaches the join
	// line only for the member the join event just cached (76239A ->
	// 5F3340 name compare), and the self row arrives with the roster.
	const self = socialPacketOf(
		guildState,
		memberRow( writer().u8( 2 ), 6, "Echo", 0 ).frame( 0x3b29 )
	);
	assert.equal( self.notice, undefined );
	assert.equal( self.guild?.members.length, 2 );
});

test("sub-op 5 &0x10 banners the notice update; &4 still levels (5E4710)", () => {
	const updated = socialPacketOf(
		guildState,
		writer().u8( 5 ).u8( 16 ).str( "Subj" ).str( "Body" ).frame( 0x3b29 )
	);
	assert.equal( updated.notice?.key, "UIIT_MSG_GUILD_COMMON_KNOW_REMIND_UPDATE" );
	assert.equal( updated.notice?.notificationBanner, true );
	assert.equal( updated.notice?.bannerOnly, true );
	assert.equal( updated.guild?.subject, "Subj" );
	assert.equal( updated.guild?.contents, "Body" );
	const leveled = socialPacketOf(
		guildState,
		writer().u8( 5 ).u8( 4 ).u8( 3 ).frame( 0x3b29 )
	);
	assert.equal( leveled.notice?.key, "UIIT_MSG_GUILD_LEVEL_UP_RESULT" );
	assert.deepEqual( leveled.notice?.arguments, [ "3" ] );
	assert.equal( leveled.guild?.level, 3 );
});

test("ally join and removal modes print their lines (5FAFF0, 762040 slot 8)", () => {
	const joined = socialPacketOf(
		guildState,
		writer().u8( 0x0d ).u32( 9 ).str( "Delta" ).u8( 1 ).str( "Boss" ).u32( 1907 ).u8( 0 ).frame( 0x3b29 )
	);
	assert.equal( joined.notice?.key, "UIIT_MSG_GUILD_ALLY_JOIN_RESULT" );
	assert.deepEqual( joined.notice?.arguments, [ "Delta" ] );
	const allied = {
		...guildState,
		alliances: [
			{ id: 7, name: "Alpha", level: 2, master: "Echo", model: 1907, flags: 1 },
			{ id: 8, name: "Bravo", level: 3, master: "Fox", model: 1907, flags: 4 },
			{ id: 9, name: "Charlie", level: 1, master: "Golf", model: 1907, flags: 0 }
		],
		allianceMaster: 7
	};
	const exited = socialPacketOf( allied, writer().u8( 0x12 ).u8( 1 ).u32( 8 ).frame( 0x3b29 ) );
	assert.equal( exited.notice?.key, "UIIT_MSG_GUILD_ALLY_EXIT_RESULT" );
	assert.deepEqual( exited.notice?.arguments, [ "Bravo" ] );
	assert.deepEqual( exited.alliances?.map( a => a.name ), [ "Alpha", "Charlie" ] );
	const expelled = socialPacketOf( allied, writer().u8( 0x12 ).u8( 2 ).u32( 9 ).frame( 0x3b29 ) );
	assert.equal( expelled.notice?.key, "UIIT_MSG_GUILD_RESPECT_ALLY_EXPEL_RESULT" );
	assert.deepEqual( expelled.notice?.arguments, [ "Charlie" ] );
	const broken = socialPacketOf( allied, writer().u8( 0x12 ).u8( 3 ).frame( 0x3b29 ) );
	assert.equal( broken.notice?.key, "UIIT_MSG_QUESTION_GUILD_RESPECT_ALLY_BREAKUP" );
	assert.deepEqual( broken.alliances, [] );
});

test("create and notice-edit acks print (75F9B0, 75C950)", () => {
	const created = socialPacketOf(
		guildState,
		memberRow(
			writer().u8( 1 ).u32( 7 ).str( "Alpha" ).u8( 2 ).u32( 0 ).str( "" ).str( "" ).u32( 0 ).u8( 0 ).u8( 1 ),
			1,
			"Echo",
			0
		).u8( 0 ).frame( 0xb663 )
	);
	assert.equal( created.notice?.key, "UIIT_MSG_GUILD_CREATE_RESULT" );
	assert.deepEqual( created.notice?.arguments, [ "Alpha" ] );
	assert.equal( created.guild?.name, "Alpha" );
	const reminded = socialPacketOf( guildState, { opcode: 0xb77a, payload: Uint8Array.of( 1 ) } );
	assert.equal( reminded.notice?.key, "UIIT_MSG_GUILD_COMMON_KNOW_REMIND_UPDATE" );
	assert.equal( reminded.notice?.bannerOnly, true );
});
