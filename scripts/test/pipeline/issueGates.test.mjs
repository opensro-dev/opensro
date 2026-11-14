/*
===========================================================================

issueGates.test.mjs - tests for scripts/github/issueGates.mjs

The pure decisions (the Ungate when grammar, each condition, lift / ask /
wait) and run() against a fake GitHub client that keeps the issues,
their labels and comments, the pull requests and the commit graph, so
each sweep's effect on the issues is what is asserted.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { GATED_LABEL, conditionHolds, decideGate, parseCondition, parseGate, run } from "../../github/issueGates.mjs";

const NOW = Date.parse( "2026-11-14T12:00:00Z" );
const LIVE = "a".repeat( 40 );
const world = ( closed = [], carried = [] ) => ({ nowMs: NOW, closed: new Set( closed ), carried: new Set( carried ) });

/*
================
fakeGitHub

issues: { number: { body, labels, state, comments } }; pulls: { number:
{ merged, merge_commit_sha } }; carriedBy: the merge commits the live
commit contains.
================
*/
function fakeGitHub( { issues, pulls = {}, carriedBy = [] } ) {
	const calls = [];
	const github = {
		paginate: async ( method, params ) => method( params ),
		rest: {
			issues: {
				listForRepo: async ( { labels } ) =>
					Object.entries( issues ).filter( ( [, issue] ) =>
						issue.state === "open" && issue.labels.includes( labels )
					)
						.map( ( [number, issue] ) => ({ number: Number( number ), body: issue.body }) ),
				listComments: async ( { issue_number } ) => issues[issue_number].comments.map( body => ({ body }) ),
				get: async ( { issue_number } ) => ({ data: { state: issues[issue_number]?.state ?? "open" } }),
				removeLabel: async ( { issue_number, name } ) => {
					calls.push( [ "removeLabel", issue_number ] );
					issues[issue_number].labels = issues[issue_number].labels.filter( label => label !== name );
				},
				createComment: async ( { issue_number, body } ) => {
					calls.push( [ "comment", issue_number ] );
					issues[issue_number].comments.push( body );
				}
			},
			pulls: { get: async ( { pull_number } ) => ({ data: pulls[pull_number] ?? { merged: false } }) },
			repos: {
				compareCommits: async ( { base, head } ) => ({
					data: { status: head === LIVE && carriedBy.includes( base ) ? "ahead" : "behind" }
				})
			}
		}
	};
	return { github, calls };
}

const context = { repo: { owner: "opensro-dev", repo: "opensro" } };
const gated = ( body, comments = [] ) => ({ body, labels: [ GATED_LABEL ], state: "open", comments });
const liveRecord = async () => ({ client: { commit: LIVE } });

test("the grammar reads each form and refuses anything else", () => {
	assert.deepEqual( parseCondition( "#2519 closed" ), { kind: "closed", issues: [ 2519 ] } );
	assert.deepEqual( parseCondition( "#2520, #2521 #2522 closed" ), { kind: "closed", issues: [ 2520, 2521, 2522 ] } );
	assert.deepEqual( parseCondition( "live carries #3199" ), { kind: "live", pull: 3199 } );
	assert.deepEqual( parseCondition( "after 2026-11-15T00:00Z" ), {
		kind: "after",
		atMs: Date.parse( "2026-11-15T00:00:00Z" )
	} );
	assert.deepEqual( parseCondition( "Owner decides" ), { kind: "owner" } );
	assert.deepEqual( parseCondition( "container" ), { kind: "container" } );
	for ( const text of [ "soon", "#x closed", "after tomorrow", "live carries 3199", "closed" ] ) {
		assert.equal( parseCondition( text ), null, text );
	}
});

test("the body's line is found anywhere, joined with and; a bad part names itself", () => {
	const body = "## Why\ntext\n\nUngate when: #2519 closed and after 2026-11-15T00:00Z\n\nmore";
	assert.equal( parseGate( body ).conditions.length, 2 );
	assert.deepEqual( parseGate( "no line here" ), { missing: true } );
	assert.deepEqual( parseGate( "Ungate when: #1 closed and when ready" ), { error: "cannot read: when ready" } );
});

test("each condition holds only on its own evidence", () => {
	assert.equal( conditionHolds( { kind: "closed", issues: [ 1, 2 ] }, world( [ 1 ] ) ), false );
	assert.equal( conditionHolds( { kind: "closed", issues: [ 1, 2 ] }, world( [ 1, 2 ] ) ), true );
	assert.equal( conditionHolds( { kind: "live", pull: 9 }, world( [], [ 9 ] ) ), true );
	assert.equal( conditionHolds( { kind: "after", atMs: NOW + 1 }, world() ), false );
	assert.equal( conditionHolds( { kind: "after", atMs: NOW }, world() ), true );
	assert.equal( conditionHolds( { kind: "owner" }, world( [ 1 ] ) ), false );
	assert.equal( conditionHolds( { kind: "container" }, world( [ 1 ] ) ), false );
});

test("lift needs every condition; a missing line is asked about once", () => {
	const gate = parseGate( "Ungate when: #1 closed and after 2026-11-14T11:00Z" );
	assert.equal( decideGate( { gate, asked: false, world: world( [ 1 ] ) } ).action, "lift" );
	assert.equal( decideGate( { gate, asked: false, world: world() } ).action, "wait" );
	assert.equal( decideGate( { gate: { missing: true }, asked: false, world: world() } ).action, "ask" );
	assert.equal( decideGate( { gate: { missing: true }, asked: true, world: world() } ).action, "wait" );
});

test("the sweep lifts what holds, asks once, and leaves the rest gated", async () => {
	const issues = {
		10: { body: "", labels: [], state: "closed", comments: [] },
		20: gated( "Ungate when: #10 closed" ),
		21: gated( "Ungate when: #11 closed" ),
		22: gated( "nothing said" ),
		23: gated( "Ungate when: live carries #30" ),
		24: gated( "Ungate when: live carries #31" ),
		25: gated( "Ungate when: owner decides" ),
		11: { body: "", labels: [], state: "open", comments: [] }
	};
	const pulls = { 30: { merged: true, merge_commit_sha: "m30" }, 31: { merged: true, merge_commit_sha: "m31" } };
	const { github } = fakeGitHub( { issues, pulls, carriedBy: [ "m30" ] } );
	const summary = await run( { github, context, nowMs: NOW, fetchJson: liveRecord } );
	assert.deepEqual( summary, [
		"#20 lift (closed)",
		"#21 wait (closed)",
		"#22 ask (missing)",
		"#23 lift (live)",
		"#24 wait (live)",
		"#25 wait (owner)"
	] );
	assert.deepEqual( [ 20, 23 ].map( n => issues[n].labels ), [ [], [] ] );
	assert.ok( issues[20].comments[0].includes( "#10 closed" ) );
	assert.ok( issues[23].comments[0].includes( "the live release carries #30" ) );
	assert.deepEqual( [ 21, 24, 25 ].map( n => issues[n].labels ), [ [ GATED_LABEL ], [ GATED_LABEL ], [
		GATED_LABEL
	] ] );
	// Asked once: a second sweep adds no comment.
	await run( { github, context, nowMs: NOW, fetchJson: liveRecord } );
	assert.equal( issues[22].comments.length, 1 );
});

test("an unreadable release record never lifts a live condition", async () => {
	const issues = { 23: gated( "Ungate when: live carries #30" ) };
	const pulls = { 30: { merged: true, merge_commit_sha: "m30" } };
	const { github, calls } = fakeGitHub( { issues, pulls, carriedBy: [ "m30" ] } );
	for (
		const fetchJson of [ async () => {
			throw new Error( "HTTP 502" );
		}, async () => ({ client: { commit: "short" } }) ]
	) {
		const summary = await run( { github, context, nowMs: NOW, fetchJson } );
		assert.deepEqual( summary, [ "#23 wait (live)" ] );
	}
	assert.deepEqual( calls, [] );
});

test("an unmerged pull request is never carried", async () => {
	const issues = { 23: gated( "Ungate when: live carries #30" ) };
	const { github } = fakeGitHub( { issues, pulls: { 30: { merged: false } }, carriedBy: [ "m30" ] } );
	assert.deepEqual( await run( { github, context, nowMs: NOW, fetchJson: liveRecord } ), [ "#23 wait (live)" ] );
});
