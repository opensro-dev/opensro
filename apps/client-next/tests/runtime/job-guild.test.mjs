/*
===========================================================================

job-guild.test.mjs - the job guild menu, requests and answers

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";
import { defined } from "../helpers/defined.mjs";

const job = await import( "../../src/engine/foundation/gameplay/job-guild.ts" );

// 5D79E0's rows every visitor gets after the membership rows.
const RANK_ROWS = [ "npc-job-contribution", "npc-job-rank", "npc-job-previous" ];

test("an NPC's guild bits offer join, then withdraw and the alias to a member", () => {
	assert.deepEqual( job.jobGuildsOffered( 0x80000 | 0x100000 ), [ 1, 2 ] );
	assert.deepEqual( job.jobMenuRows( [ 1 ], job.noJob() ).map( row => row.id ), [
		"npc-job-join:1",
		...RANK_ROWS.map( id => id + ":1" )
	] );
	const trader = { type: 1, grade: 1, exp: 0, alias: "", contribution: 0 };
	assert.deepEqual( job.jobMenuRows( [ 1 ], trader ), [
		{ id: "npc-job-withdraw:1", symbol: "UIIT_STT_NPC_CHATTING_TRADERMENU_WITHD" },
		{ id: "npc-job-alias:1", symbol: "UIIT_STT_NPC_CHATTING_TRADERMENU_ALIASCREATE" },
		{ id: "npc-job-contribution:1", symbol: "UIIT_STT_NPC_CHATTING_TRADERMENU_DONATIONRANK" },
		{ id: "npc-job-rank:1", symbol: "UIIT_STT_NPC_CHATTING_TRADERMENU_JOBRANK" },
		{ id: "npc-job-previous:1", symbol: "UIIT_STT_NPC_CHATTING_JOBINFO_OLD" }
	] );
	assert.deepEqual( job.jobMenuRows( [ 3 ], trader ).map( row => row.id ), RANK_ROWS.map( id => id + ":3" ) );
	// Case 0x24: a hunter's own guild adds the outcome row after the alias.
	const hunter = { ...trader, type: 3 };
	assert.deepEqual( job.jobMenuRows( [ 3 ], hunter ).map( row => row.id ).slice( 0, 3 ), [
		"npc-job-withdraw:3",
		"npc-job-alias:3",
		"npc-job-outcome:3"
	] );
});

test("rank, outcome and previous-information answers follow 763F00, 75D5C0 and 75C650", () => {
	const list = defined(
		job.jobRankAnswer( {
			opcode: 0xb37e,
			payload: Uint8Array.of( 1, 2, 0, 1, 1, 2, 0, 65, 98, 3, 40, 0, 0, 0 )
		} )
	);
	assert.deepEqual( list.list, { job: 2, kind: 0, rows: [ { rank: 1, alias: "Ab", grade: 3, value: 40 } ] } );
	assert.equal( defined( job.jobRankAnswer( { opcode: 0xb37e, payload: Uint8Array.of( 2, 3, 2, 0 ) } ) ).list, null );
	assert.throws( () => job.jobRankAnswer( { opcode: 0xb37e, payload: Uint8Array.of( 1, 2, 0, 1, 1 ) } ) );
	const told = defined(
		job.jobOutcomeAnswer( { opcode: 0xb7be, payload: Uint8Array.of( 1, 0, 0xe8, 3, 0, 0 ) }, null, 9 )
	);
	assert.deepEqual( told.outcome, { npc: 9, reward: 1000, sequence: 1, collected: 0 } );
	const paid = defined(
		job.jobOutcomeAnswer( { opcode: 0xb7be, payload: Uint8Array.of( 1, 1, 0xe8, 3, 0, 0 ) }, told.outcome, 9 )
	);
	assert.equal( paid.outcome?.collected, 1 );
	assert.deepEqual( paid.notice?.arguments, [ "1,000" ] );
	assert.equal(
		job.jobPrevInfoAnswer( { opcode: 0xb5ee, payload: Uint8Array.of( 2, 0x29 ) } )?.key,
		"UIIT_MSG_JOBINFO_OLD_NOTEXIST"
	);
	assert.deepEqual( [ ...job.jobRankRequest( 17, 1, 1 ).payload ], [ 17, 0, 0, 0, 1, 1 ] );
	assert.deepEqual( [ ...job.jobOutcomeRequest( 17, 1 ).payload ], [ 17, 0, 0, 0, 1 ] );
	assert.deepEqual( [ ...job.jobPrevInfoRequest( 17 ).payload ], [ 17, 0, 0, 0 ] );
});

test("requests carry the native bodies", () => {
	assert.deepEqual( [ ...job.jobJoinRequest( 17, 1 ).payload ], [ 17, 0, 0, 0, 1 ] );
	assert.deepEqual( [ ...job.jobWithdrawRequest( 17 ).payload ], [ 17, 0, 0, 0 ] );
	assert.deepEqual( [ ...job.jobAliasRequest( 17, 1, "Ab" ).payload ], [ 17, 0, 0, 0, 1, 2, 0, 65, 98 ] );
});

test("answers move the local job and raise their notices", () => {
	const joined = defined(
		job.jobGuildAnswer( { opcode: 0xb439, payload: Uint8Array.of( 1, 1, 1, 0, 0, 0, 0 ) }, job.noJob() )
	);
	assert.deepEqual( joined.job, { type: 1, grade: 1, exp: 0, alias: "", contribution: 0 } );
	assert.equal( defined( joined.notice ).key, "UIIT_MSG_JOBGUILD_JOIN_COMPELET" );
	const named = defined(
		job.jobGuildAnswer( { opcode: 0xb620, payload: Uint8Array.of( 1, 1, 2, 0, 65, 98 ) }, joined.job )
	);
	assert.equal( named.job.alias, "Ab" );
	const refused = defined( job.jobGuildAnswer( { opcode: 0xb439, payload: Uint8Array.of( 2, 0x19 ) }, job.noJob() ) );
	assert.deepEqual( refused.job, job.noJob() );
	assert.ok( refused.notice );
	const left = defined( job.jobGuildAnswer( { opcode: 0xb661, payload: Uint8Array.of( 1 ) }, named.job ) );
	assert.deepEqual( left.job, job.noJob() );
	assert.equal( job.jobGuildAnswer( { opcode: 0xb06d, payload: Uint8Array.of( 1 ) }, job.noJob() ), null );
});

test("the dress bar names its seconds only for the given gid", () => {
	const bar = { opcode: 0x3434, payload: Uint8Array.of( 9, 0, 0, 0, 2, 2, 10 ) };
	assert.equal( job.jobDressSeconds( bar, 9 ), 10 );
	assert.equal( job.jobDressSeconds( bar, 8 ), null );
});
