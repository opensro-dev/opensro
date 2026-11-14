/*
===========================================================================

issueClaim.test.mjs - tests for scripts/github/issueClaim.mjs

The pure decisions (command parsing, grant / renew / refuse, release,
the sweep) and run() itself against a fake GitHub client that keeps an
issue's comments, assignees and labels, so each command's effect on the
issue is what is asserted.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	CLAIMED_LABEL,
	CONTRIBUTOR_LEASE_HOURS,
	MAINTAINER_LEASE_HOURS,
	MAX_CONTRIBUTOR_CLAIMS,
	decideClaim,
	decideSweep,
	decideUnclaim,
	parseCommand,
	readLease,
	run
} from "../../github/issueClaim.mjs";

const BOT = "github-actions[bot]";
const HOUR_MS = 60 * 60 * 1000;
const T0 = Date.parse( "2026-11-14T12:00:00Z" );

/*
================
fakeRepo

Open issues (the first one, number 7, is `issue`) and the client calls
run() makes, applied to them. failing: issue numbers whose comment reads
fail with a 500.
================
*/
function fakeRepo( { numbers = [ 7 ], failing = [] } = {} ) {
	const issues = new Map( numbers.map( number => [ number, {
		number,
		state: "open",
		comments: [],
		assignees: new Set(),
		labels: new Set(),
		timeline: []
	} ] ) );
	const issue = issues.get( numbers[0] );
	const at = number => issues.get( number );
	let nextId = 1;
	const rest = {
		issues: {
			listComments: "listComments",
			listEventsForTimeline: "listEventsForTimeline",
			listForRepo: "listForRepo",
			async createComment( { issue_number, body } ) {
				at( issue_number ).comments.push( { id: nextId++, user: { login: BOT, type: "Bot" }, body } );
			},
			async updateComment( { comment_id, body } ) {
				for ( const { comments } of issues.values() ) {
					const comment = comments.find( c => c.id === comment_id );
					if ( comment ) comment.body = body;
				}
			},
			async addAssignees( { issue_number, assignees } ) {
				for ( const login of assignees ) at( issue_number ).assignees.add( login );
			},
			async removeAssignees( { issue_number, assignees } ) {
				for ( const login of assignees ) at( issue_number ).assignees.delete( login );
			},
			async addLabels( { issue_number, labels } ) {
				for ( const label of labels ) at( issue_number ).labels.add( label );
			},
			async removeLabel( { issue_number, name } ) {
				if ( !at( issue_number ).labels.delete( name ) ) {
					throw Object.assign( new Error( "Label does not exist" ), { status: 404 } );
				}
			}
		}
	};
	const github = {
		rest,
		async paginate( method, params ) {
			if ( method === "listComments" ) {
				if ( failing.includes( params.issue_number ) ) {
					throw Object.assign( new Error( "Server Error" ), { status: 500 } );
				}
				return [ ...at( params.issue_number ).comments ];
			}
			if ( method === "listEventsForTimeline" ) return [ ...at( params.issue_number ).timeline ];
			if ( method === "listForRepo" ) {
				return [ ...issues.values() ].filter( i => i.labels.has( CLAIMED_LABEL ) ).map( i => ({
					number: i.number
				}) );
			}
			throw new Error( `unexpected paginate ${method}` );
		}
	};
	/*
	================
	comment

	A person's comment, then the workflow's run on it.
	================
	*/
	async function comment( login, body, association = "NONE", nowMs = T0, number = issue.number ) {
		const event = { id: nextId++, user: { login, type: "User" }, body, author_association: association };
		at( number ).comments.push( event );
		await run( {
			github,
			nowMs,
			context: {
				eventName: "issue_comment",
				repo: { owner: "opensro-dev", repo: "opensro" },
				payload: { comment: event, issue: { number, state: at( number ).state } }
			}
		} );
	}
	/*
	================
	sweep
	================
	*/
	async function sweep( nowMs, core = null ) {
		await run( {
			github,
			nowMs,
			core,
			context: { eventName: "schedule", repo: { owner: "opensro-dev", repo: "opensro" } }
		} );
	}
	const lastReply = ( number = issue.number ) =>
		at( number ).comments.filter( c => c.user.login === BOT ).at( -1 )?.body ?? "";
	return { issue, at, comment, sweep, lastReply };
}

// ============================================================================

test("only a first-line /claim or /unclaim is a command", () => {
	assert.deepEqual( parseCommand( "/claim" ), { verb: "claim", seat: "" } );
	assert.deepEqual( parseCommand( "  /claim T3822\nI'll do the socket" ), { verb: "claim", seat: "T3822" } );
	assert.deepEqual( parseCommand( "/unclaim" ), { verb: "unclaim", seat: "" } );
	for ( const body of [ "please /claim", "> /claim", "/claimed", "/claim a b", "/claim <script>", "", null ] ) {
		assert.equal( parseCommand( body ), null, String( body ) );
	}
});

test("a claim is granted, renewed by its holder and refused to anyone else", () => {
	const granted = decideClaim( { lease: null, actor: "contributor", seat: "", association: "NONE", nowMs: T0 } );
	assert.equal( granted.action, "grant" );
	assert.equal( Date.parse( granted.until ) - T0, CONTRIBUTOR_LEASE_HOURS * HOUR_MS );
	const lease = { holder: "contributor", seat: "", until: granted.until, hours: granted.hours };
	assert.equal(
		decideClaim( { lease, actor: "contributor", seat: "", association: "NONE", nowMs: T0 + HOUR_MS } ).action,
		"renew"
	);
	assert.equal(
		decideClaim( { lease, actor: "other", seat: "", association: "NONE", nowMs: T0 } ).action,
		"refuse"
	);
	// A maintainer is refused too: a held claim is released first, never overridden.
	assert.equal(
		decideClaim( { lease, actor: "maintainer", seat: "", association: "OWNER", nowMs: T0 } ).action,
		"refuse"
	);
	// Past its expiry the lease is anyone's.
	const later = T0 + CONTRIBUTOR_LEASE_HOURS * HOUR_MS + 1;
	assert.equal(
		decideClaim( { lease, actor: "other", seat: "", association: "NONE", nowMs: later } ).action,
		"grant"
	);
});

test("one maintainer account is several holders, one per seat", () => {
	const lease = { holder: "maintainer", seat: "T1", until: new Date( T0 + HOUR_MS ).toISOString(), hours: 24 };
	assert.equal(
		decideClaim( { lease, actor: "maintainer", seat: "T1", association: "OWNER", nowMs: T0 } ).action,
		"renew"
	);
	assert.equal(
		decideClaim( { lease, actor: "maintainer", seat: "T2", association: "OWNER", nowMs: T0 } ).action,
		"refuse"
	);
	const granted = decideClaim( { lease: null, actor: "maintainer", seat: "T2", association: "MEMBER", nowMs: T0 } );
	assert.equal( Date.parse( granted.until ) - T0, MAINTAINER_LEASE_HOURS * HOUR_MS );
});

test("a claim is released by its holder or a maintainer only", () => {
	const lease = { holder: "contributor", seat: "", until: new Date( T0 + HOUR_MS ).toISOString(), hours: 72 };
	assert.equal( decideUnclaim( { lease, actor: "contributor", association: "NONE" } ).action, "release" );
	assert.equal( decideUnclaim( { lease, actor: "maintainer", association: "OWNER" } ).action, "release" );
	assert.equal( decideUnclaim( { lease, actor: "other", association: "CONTRIBUTOR" } ).action, "refuse" );
	assert.equal( decideUnclaim( { lease: null, actor: "contributor", association: "NONE" } ).action, "none" );
});

test("the sweep keeps a live lease, extends an active one and releases an idle one", () => {
	const lease = { holder: "contributor", seat: "", until: new Date( T0 ).toISOString(), hours: 72 };
	assert.equal( decideSweep( { lease, nowMs: T0 - 1, linkedPullRequests: [] } ).action, "keep" );
	assert.equal( decideSweep( { lease, nowMs: T0 + 1, linkedPullRequests: [] } ).action, "release" );
	const recent = new Date( T0 - HOUR_MS ).toISOString(), stale = new Date( T0 - 100 * HOUR_MS ).toISOString();
	const open = { state: "open", author: "contributor", updatedAt: recent };
	assert.equal( decideSweep( { lease, nowMs: T0 + 1, linkedPullRequests: [ open ] } ).action, "extend" );
	for (
		const pull of [ { ...open, state: "closed" }, { ...open, author: "other" }, { ...open, updatedAt: stale } ]
	) {
		assert.equal( decideSweep( { lease, nowMs: T0 + 1, linkedPullRequests: [ pull ] } ).action, "release" );
	}
});

test("only the workflow's own latest marker is the lease", () => {
	const marker =
		'<!-- opensro-claim {"holder":"contributor","seat":"","until":"2026-11-17T12:00:00.000Z","hours":72} -->';
	const forged = { id: 1, user: { login: "other" }, body: marker.replace( "contributor", "other" ) };
	assert.equal( readLease( [ forged ], BOT ), null );
	const own = { id: 2, user: { login: BOT }, body: `Claimed\n\n${marker}` };
	assert.equal( readLease( [ own, forged ], BOT ).holder, "contributor" );
	const released = { id: 3, user: { login: BOT }, body: '<!-- opensro-claim {"released":true} -->' };
	assert.equal( readLease( [ own, released ], BOT ), null );
});

// ============================================================================

test("run: claim, refuse, release, then a takeover after expiry", async () => {
	const repo = fakeRepo();
	await repo.comment( "contributor", "/claim" );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "contributor" ] );
	assert.ok( repo.issue.labels.has( CLAIMED_LABEL ) );
	assert.match( repo.lastReply(), /^Claimed by @contributor until 2026-11-17T12:00:00\.000Z \(72 h\)/ );

	await repo.comment( "other", "/claim" );
	assert.match( repo.lastReply(), /^Already claimed by @contributor/ );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "contributor" ] );

	await repo.comment( "other", "/unclaim", "CONTRIBUTOR" );
	assert.match( repo.lastReply(), /^Only @contributor or a maintainer/ );

	await repo.comment( "contributor", "/unclaim" );
	assert.equal( repo.issue.assignees.size, 0 );
	assert.equal( repo.issue.labels.has( CLAIMED_LABEL ), false );
	assert.match( repo.lastReply(), /^Claim released by @contributor/ );

	await repo.comment( "contributor", "/claim" );
	const afterExpiry = T0 + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS;
	await repo.comment( "maintainer", "/claim T4001", "OWNER", afterExpiry );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "maintainer" ] );
	assert.match( repo.lastReply(), /^Claimed by @maintainer \(T4001\)/ );
});

test("run: the sweep extends a claim with a moving pull request and releases an idle one", async () => {
	const repo = fakeRepo();
	await repo.comment( "contributor", "/claim" );
	const expired = T0 + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS;
	repo.issue.timeline.push( {
		event: "cross-referenced",
		source: {
			issue: {
				pull_request: {},
				state: "open",
				user: { login: "contributor" },
				updated_at: new Date( expired - HOUR_MS ).toISOString()
			}
		}
	} );
	await repo.sweep( expired );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "contributor" ] );
	assert.match( repo.lastReply(), /^Claim renewed by @contributor/ );

	repo.issue.timeline.length = 0;
	await repo.sweep( expired + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS );
	assert.equal( repo.issue.assignees.size, 0 );
	assert.equal( repo.issue.labels.has( CLAIMED_LABEL ), false );
	assert.match( repo.lastReply(), /expired with no activity and is released/ );
});

test("run: a comment that is no command changes nothing", async () => {
	const repo = fakeRepo();
	await repo.comment( "contributor", "I might /claim this later" );
	assert.equal( repo.issue.assignees.size, 0 );
	assert.equal( repo.lastReply(), "" );
});

test("run: one account holds at most the cap of live claims; a maintainer seat is not capped", async () => {
	const repo = fakeRepo( { numbers: [ 1, 2, 3, 4 ] } );
	for ( const number of [ 1, 2 ] ) await repo.comment( "contributor", "/claim", "NONE", T0, number );
	assert.equal( MAX_CONTRIBUTOR_CLAIMS, 2 );
	await repo.comment( "contributor", "/claim", "NONE", T0, 3 );
	assert.match( repo.lastReply( 3 ), /already holds 2 claims/ );
	assert.equal( repo.at( 3 ).labels.has( CLAIMED_LABEL ), false );
	assert.equal( repo.at( 3 ).assignees.size, 0 );
	// A renewal of a held claim is never refused by the cap.
	await repo.comment( "contributor", "/claim", "NONE", T0 + HOUR_MS, 2 );
	assert.match( repo.lastReply( 2 ), /^Claim renewed by @contributor/ );
	// A released or expired claim no longer counts.
	await repo.comment( "contributor", "/unclaim", "NONE", T0 + HOUR_MS, 1 );
	await repo.comment( "contributor", "/claim", "NONE", T0 + HOUR_MS, 3 );
	assert.match( repo.lastReply( 3 ), /^Claimed by @contributor/ );
	const later = T0 + (CONTRIBUTOR_LEASE_HOURS + 2) * HOUR_MS;
	await repo.comment( "contributor", "/claim", "NONE", later, 4 );
	assert.match( repo.lastReply( 4 ), /^Claimed by @contributor/ );
	// A maintainer's seats are not capped: three at once.
	for ( const number of [ 1, 2, 3 ] ) {
		await repo.comment( "maintainer", "/claim T" + number, "OWNER", later, number );
		assert.ok( repo.lastReply( number ).startsWith( `Claimed by @maintainer (T${number})` ), String( number ) );
	}
});

test("run: a GitHub failure on one claimed issue leaves the sweep deciding the others", async () => {
	const repo = fakeRepo( { numbers: [ 1, 2 ], failing: [] } );
	for ( const number of [ 1, 2 ] ) await repo.comment( "contributor", "/claim", "NONE", T0, number );
	const failing = fakeRepo( { numbers: [ 1, 2 ], failing: [ 1 ] } );
	for ( const number of [ 1, 2 ] ) {
		failing.at( number ).comments.push( ...repo.at( number ).comments );
		for ( const label of repo.at( number ).labels ) failing.at( number ).labels.add( label );
		for ( const login of repo.at( number ).assignees ) failing.at( number ).assignees.add( login );
	}
	const warnings = [];
	await failing.sweep( T0 + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS, { warning: line => warnings.push( line ) } );
	assert.equal( failing.at( 1 ).labels.has( CLAIMED_LABEL ), true );
	assert.equal( failing.at( 2 ).labels.has( CLAIMED_LABEL ), false );
	assert.match( failing.lastReply( 2 ), /expired with no activity/ );
	assert.deepEqual( warnings, [ "claim sweep: #1: Server Error" ] );
});
