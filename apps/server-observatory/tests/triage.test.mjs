/*
===========================================================================
triage.test.mjs - the #bug-reports drafts: typed read, one decision write
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createTriage, parseDecisions, triageBody } from "../server/triage.mjs";

const SNAPSHOT = {
	schemaVersion: 1,
	generatedAt: "2026-11-03T05:00:00Z",
	threads: [
		{
			thread: "1560133986854903840",
			title: "alchemy NPC",
			priority: "Low",
			action: "close",
			issues: [ 1850 ],
			draft: "Reply <b>here</b>"
		},
		{
			thread: "1560211639045718088",
			title: "Most Wanted",
			priority: "Bogus",
			action: "odd",
			issues: [ "x", 1849 ],
			draft: "Second"
		}
	]
};

async function withFiles( fn ) {
	const dir = await mkdtemp( join( tmpdir(), "triage-" ) );
	try {
		const snap = join( dir, "triage.json" );
		const decisions = join( dir, "decisions.jsonl" );
		await writeFile( snap, JSON.stringify( SNAPSHOT ) );
		await fn( snap, decisions );
	} finally {
		await rm( dir, { recursive: true, force: true } );
	}
}

test("a snapshot reads typed: unknown enums fall back, draft text stays text", () => {
	const body = triageBody( SNAPSHOT, [] );
	assert.equal( body.threads.length, 2 );
	assert.equal( body.threads[0].draft, "Reply <b>here</b>" );
	assert.deepEqual( body.threads[0].decision, { state: "pending", by: "", at: "", note: "" } );
	assert.equal( body.threads[1].priority, "" );
	assert.equal( body.threads[1].action, "reply" );
	assert.deepEqual( body.threads[1].issues, [ 1849 ] );
	assert.equal( triageBody( { ...SNAPSHOT, schemaVersion: 2 }, [] ), null );
});

test("a thread carries its public reply, whether posting closes it, and when it was posted", () => {
	const threads = [
		{
			...SNAPSHOT.threads[0],
			reply: "Fixed and live.",
			closes: true,
			posted: { at: "2026-11-03T06:00:00Z", closed: true }
		},
		{ ...SNAPSHOT.threads[1], closes: "yes", posted: { at: "not a time" } }
	];
	const [done, fresh] = triageBody( { ...SNAPSHOT, threads }, [] ).threads;
	assert.deepEqual( [ done.reply, done.closes, done.posted ], [ "Fixed and live.", true, {
		at: "2026-11-03T06:00:00Z",
		closed: true
	} ] );
	assert.deepEqual( [ fresh.reply, fresh.closes, fresh.posted ], [ "", false, null ] );
});

test("decision lines: torn or foreign lines are skipped, the last decision wins", () => {
	const lines = [
		JSON.stringify( {
			thread: "1560133986854903840",
			state: "rejected",
			by: "a",
			at: "2026-11-03T05:01:00Z",
			note: "no"
		} ),
		'{"thread": "15601',
		JSON.stringify( { thread: "not-a-thread", state: "approved", at: "2026-11-03T05:02:00Z" } ),
		JSON.stringify( {
			thread: "1560133986854903840",
			state: "approved",
			by: "b",
			at: "2026-11-03T05:03:00Z",
			note: ""
		} )
	].join( "\n" );
	const decisions = parseDecisions( lines );
	assert.equal( decisions.length, 2 );
	assert.equal( triageBody( SNAPSHOT, decisions ).threads[0].decision.state, "approved" );
});

test("decide appends one line; a rejection needs a reason; unknown threads are refused", async () => {
	await withFiles( async ( snap, decisionsFile ) => {
		const triage = createTriage( snap, decisionsFile, () => new Date( "2026-11-03T05:05:00Z" ) );
		assert.equal(
			(await triage.decide( "staff", { thread: "1560133986854903840", decision: "reject" } )).status,
			400
		);
		assert.equal(
			(await triage.decide( "staff", { thread: "1999999999999999999", decision: "approve" } )).status,
			404
		);
		assert.equal(
			(await triage.decide( "staff", { thread: "1560133986854903840", decision: "post" } )).status,
			400
		);
		const ok = await triage.decide( "staff", { thread: "1560133986854903840", decision: "approve" } );
		assert.equal( ok.status, 200 );
		assert.equal( ok.body.threads[0].decision.state, "approved" );
		const written = (await readFile( decisionsFile, "utf8" )).trim().split( "\n" );
		assert.equal( written.length, 1 );
		assert.deepEqual( JSON.parse( written[0] ), {
			thread: "1560133986854903840",
			state: "approved",
			by: "staff",
			at: "2026-11-03T05:05:00.000Z",
			note: ""
		} );
	} );
});

test("without both files configured the drafts are off", async () => {
	const triage = createTriage( "", "" );
	assert.equal( triage.enabled, false );
	assert.equal( (await triage.request()).status, 503 );
});
