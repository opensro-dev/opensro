/*
===========================================================================
board.test.mjs - the work board's snapshot read stays bounded, typed and inert
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { boardBody, createBoard } from "../server/board.mjs";

const NOW = Date.parse( "2026-11-03T03:30:00Z" );
const SNAPSHOT = {
	schemaVersion: 1,
	generatedAt: "2026-11-03T03:28:00Z",
	intervalSeconds: 120,
	release: {
		name: "Hotfix",
		freeze: "01cf43d06",
		why: "resume loop",
		steps: [ { step: "Freeze", state: "done" }, { step: "Stage", state: "running" } ],
		contains: [ 2363, 2369 ],
		fixes: [ 2146 ],
		next: { name: "Next", candidates: [ 2420 ] },
		rules: [ "A release is a fixed PR list." ]
	},
	needs: [ { what: "Release go", detail: "approve the publish" } ],
	agents: [ { name: "opensro1", task: "idle", tree: "", state: "idle", blockedOn: "", at: "2026-11-03T03:06:32Z" } ],
	prs: [
		{
			number: 2433,
			title: "feat(console)",
			author: "a",
			fork: false,
			approvals: 1,
			needed: 2,
			ci: "green",
			blocker: "needs 1 more approval",
			ready: false,
			createdAt: "2026-11-03T03:25:00Z"
		}
	],
	threads: [ { thread: "1434567890123456789", title: "icons", priority: "High", action: "fixed", issues: [ 2363 ] } ],
	issues: { open: 86, withoutPr: 40, labels: [ { name: "bug", count: 30 } ] },
	merged: [ { number: 2424, title: "client", mergedAt: "2026-11-03T02:00:00Z" } ]
};

/*
================
withFile

A temporary snapshot file holding text, removed after the body runs.
================
*/
async function withFile( text, body ) {
	const dir = await mkdtemp( join( tmpdir(), "board-" ) );
	const file = join( dir, "board.json" );
	try {
		await writeFile( file, text );
		return await body( file );
	} finally {
		await rm( dir, { recursive: true, force: true } );
	}
}

test("a good snapshot passes with its age and the repository", async () => {
	await withFile( JSON.stringify( SNAPSHOT ), async file => {
		const result = await createBoard( file, "opensro-dev/opensro", () => NOW ).request();
		assert.equal( result.status, 200 );
		assert.equal( result.body.repo, "opensro-dev/opensro" );
		assert.equal( result.body.ageSeconds, 120 );
		assert.equal( result.body.stale, false );
		assert.deepEqual( result.body.release.contains, [ 2363, 2369 ] );
		assert.equal( result.body.prs[0].ci, "green" );
		assert.equal( result.body.agents[0].at, Date.parse( "2026-11-03T03:06:32Z" ) );
	} );
});

test("a snapshot older than twice its interval is stale", () => {
	const old = boardBody( { ...SNAPSHOT, generatedAt: "2026-11-03T03:25:00Z" }, NOW );
	assert.equal( old.ageSeconds, 300 );
	assert.equal( old.stale, true );
	const bounded = boardBody( { ...SNAPSHOT, intervalSeconds: 1 }, NOW );
	assert.equal( bounded.intervalSeconds, 30, "an interval below the floor is raised to it" );
	assert.equal( boardBody( { ...SNAPSHOT, intervalSeconds: "x" }, NOW ).intervalSeconds, 300 );
});

test("the board is off without a file or a valid repository", async () => {
	assert.equal( createBoard( "", "opensro-dev/opensro" ).enabled, false );
	assert.equal( createBoard( "/x.json", "https://evil.test/a" ).enabled, false );
	assert.equal( createBoard( "/x.json", "../../etc" ).enabled, false );
	const off = await createBoard( undefined, undefined ).request();
	assert.equal( off.status, 503 );
	const missing = await createBoard( join( tmpdir(), "board-none", "no.json" ), "a/b" ).request();
	assert.equal( missing.status, 503, "no snapshot yet is not an empty board" );
});

test("a snapshot that is not v1, not JSON or too large is refused", async () => {
	await withFile( "{not json", async file => {
		assert.equal( (await createBoard( file, "a/b", () => NOW ).request()).status, 502 );
	} );
	await withFile( JSON.stringify( { ...SNAPSHOT, schemaVersion: 2 } ), async file => {
		assert.equal( (await createBoard( file, "a/b", () => NOW ).request()).status, 502 );
	} );
	await withFile( JSON.stringify( { ...SNAPSHOT, generatedAt: "yesterday" } ), async file => {
		assert.equal( (await createBoard( file, "a/b", () => NOW ).request()).status, 502 );
	} );
	await withFile( " ".repeat( 1024 * 1024 + 1 ), async file => {
		const result = await createBoard( file, "a/b", () => NOW ).request();
		assert.equal( result.status, 502 );
		assert.match( result.body.error, /larger than/ );
	} );
});

test("only the contract's fields pass, each of its type", () => {
	const body = boardBody( {
		...SNAPSHOT,
		extra: "dropped",
		release: { ...SNAPSHOT.release, freeze: "javascript:alert(1)", contains: [ 1, "2", -3, 4.5, 2363 ] },
		prs: [
			{ ...SNAPSHOT.prs[0], url: "https://evil.test", title: "<img src=x onerror=alert(1)>", ci: "purple" },
			{ ...SNAPSHOT.prs[0], number: "2433" },
			{ ...SNAPSHOT.prs[0], number: 0 }
		],
		threads: [ { ...SNAPSHOT.threads[0], thread: "../etc" }, { ...SNAPSHOT.threads[0], action: "post" } ],
		agents: [ { ...SNAPSHOT.agents[0], task: "x".repeat( 5000 ) } ]
	}, NOW );
	assert.equal( body.extra, undefined );
	assert.equal( body.release.freeze, "", "a freeze that is not a sha is blank" );
	assert.deepEqual( body.release.contains, [ 1, 2363 ] );
	assert.equal( body.prs.length, 1, "a PR without a whole positive number is dropped" );
	assert.equal( body.prs[0].url, undefined, "the snapshot never supplies a link" );
	assert.equal( body.prs[0].title, "<img src=x onerror=alert(1)>", "text stays text; the page never parses it" );
	assert.equal( body.prs[0].ci, "none" );
	assert.equal( body.threads.length, 1 );
	assert.equal( body.threads[0].action, "reply" );
	assert.equal( body.agents[0].task.length, 300 );
	assert.deepEqual( boardBody( { ...SNAPSHOT, issues: "lots", release: "x" }, NOW ).issues, {
		open: 0,
		withoutPr: 0,
		labels: []
	} );
	assert.equal( boardBody( { ...SNAPSHOT, release: "x" }, NOW ).release, null );
});
