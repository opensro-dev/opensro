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

test("an NPC's guild bits offer join, then withdraw and the alias to a member", () => {
	assert.deepEqual( job.jobGuildsOffered( 0x80000 | 0x100000 ), [ 1, 2 ] );
	assert.deepEqual( job.jobMenuRows( [ 1 ], job.noJob() ).map( row => row.id ), [ "npc-job-join:1" ] );
	const trader = { type: 1, grade: 1, exp: 0, alias: "" };
	assert.deepEqual( job.jobMenuRows( [ 1 ], trader ), [
		{ id: "npc-job-withdraw:1", symbol: "UIIT_STT_NPC_CHATTING_TRADERMENU_WITHD" },
		{ id: "npc-job-alias:1", symbol: "UIIT_STT_NPC_CHATTING_TRADERMENU_ALIASCREATE" }
	] );
	assert.deepEqual( job.jobMenuRows( [ 3 ], trader ), [] );
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
	assert.deepEqual( joined.job, { type: 1, grade: 1, exp: 0, alias: "" } );
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
