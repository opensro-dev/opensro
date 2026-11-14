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

One open issue and the client calls run() makes, applied to it.
================
*/
function fakeRepo() {
	const issue = { number: 7, state: "open", comments: [], assignees: new Set(), labels: new Set(), timeline: [] };
	let nextId = 1;
	const rest = {
		issues: {
			listComments: "listComments",
			listEventsForTimeline: "listEventsForTimeline",
			listForRepo: "listForRepo",
			async createComment( { body } ) {
				issue.comments.push( { id: nextId++, user: { login: BOT, type: "Bot" }, body } );
			},
			async updateComment( { comment_id, body } ) {
				issue.comments.find( comment => comment.id === comment_id ).body = body;
			},
			async addAssignees( { assignees } ) {
				for ( const login of assignees ) issue.assignees.add( login );
			},
			async removeAssignees( { assignees } ) {
				for ( const login of assignees ) issue.assignees.delete( login );
			},
			async addLabels( { labels } ) {
				for ( const label of labels ) issue.labels.add( label );
			},
			async removeLabel( { name } ) {
				if ( !issue.labels.delete( name ) ) {
					throw Object.assign( new Error( "Label does not exist" ), { status: 404 } );
				}
			}
		}
	};
	const github = {
		rest,
		async paginate( method ) {
			if ( method === "listComments" ) return [ ...issue.comments ];
			if ( method === "listEventsForTimeline" ) return [ ...issue.timeline ];
			if ( method === "listForRepo" ) {
				return issue.labels.has( CLAIMED_LABEL ) ?
					[ { number: issue.number } ] :
					[];
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
	async function comment( login, body, association = "NONE", nowMs = T0 ) {
		const event = { id: nextId++, user: { login, type: "User" }, body, author_association: association };
		issue.comments.push( event );
		await run( {
			github,
			nowMs,
			context: {
				eventName: "issue_comment",
				repo: { owner: "opensro-dev", repo: "opensro" },
				payload: { comment: event, issue: { number: issue.number, state: issue.state } }
			}
		} );
	}
	/*
	================
	sweep
	================
	*/
	async function sweep( nowMs ) {
		await run( {
			github,
			nowMs,
			context: { eventName: "schedule", repo: { owner: "opensro-dev", repo: "opensro" } }
		} );
	}
	const lastReply = () => issue.comments.filter( c => c.user.login === BOT ).at( -1 )?.body ?? "";
	return { issue, comment, sweep, lastReply };
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
	const granted = decideClaim( { lease: null, actor: "rebraxin", seat: "", association: "NONE", nowMs: T0 } );
	assert.equal( granted.action, "grant" );
	assert.equal( Date.parse( granted.until ) - T0, CONTRIBUTOR_LEASE_HOURS * HOUR_MS );
	const lease = { holder: "rebraxin", seat: "", until: granted.until, hours: granted.hours };
	assert.equal(
		decideClaim( { lease, actor: "rebraxin", seat: "", association: "NONE", nowMs: T0 + HOUR_MS } ).action,
		"renew"
	);
	assert.equal(
		decideClaim( { lease, actor: "someone", seat: "", association: "NONE", nowMs: T0 } ).action,
		"refuse"
	);
	// A maintainer is refused too: a held claim is released first, never overridden.
	assert.equal(
		decideClaim( { lease, actor: "soyeonp92", seat: "", association: "OWNER", nowMs: T0 } ).action,
		"refuse"
	);
	// Past its expiry the lease is anyone's.
	const later = T0 + CONTRIBUTOR_LEASE_HOURS * HOUR_MS + 1;
	assert.equal(
		decideClaim( { lease, actor: "someone", seat: "", association: "NONE", nowMs: later } ).action,
		"grant"
	);
});

test("one maintainer account is several holders, one per seat", () => {
	const lease = { holder: "soyeonp92", seat: "T1", until: new Date( T0 + HOUR_MS ).toISOString(), hours: 24 };
	assert.equal(
		decideClaim( { lease, actor: "soyeonp92", seat: "T1", association: "OWNER", nowMs: T0 } ).action,
		"renew"
	);
	assert.equal(
		decideClaim( { lease, actor: "soyeonp92", seat: "T2", association: "OWNER", nowMs: T0 } ).action,
		"refuse"
	);
	const granted = decideClaim( { lease: null, actor: "soyeonp92", seat: "T2", association: "MEMBER", nowMs: T0 } );
	assert.equal( Date.parse( granted.until ) - T0, MAINTAINER_LEASE_HOURS * HOUR_MS );
});

test("a claim is released by its holder or a maintainer only", () => {
	const lease = { holder: "rebraxin", seat: "", until: new Date( T0 + HOUR_MS ).toISOString(), hours: 72 };
	assert.equal( decideUnclaim( { lease, actor: "rebraxin", association: "NONE" } ).action, "release" );
	assert.equal( decideUnclaim( { lease, actor: "soyeonp92", association: "OWNER" } ).action, "release" );
	assert.equal( decideUnclaim( { lease, actor: "someone", association: "CONTRIBUTOR" } ).action, "refuse" );
	assert.equal( decideUnclaim( { lease: null, actor: "rebraxin", association: "NONE" } ).action, "none" );
});

test("the sweep keeps a live lease, extends an active one and releases an idle one", () => {
	const lease = { holder: "rebraxin", seat: "", until: new Date( T0 ).toISOString(), hours: 72 };
	assert.equal( decideSweep( { lease, nowMs: T0 - 1, linkedPullRequests: [] } ).action, "keep" );
	assert.equal( decideSweep( { lease, nowMs: T0 + 1, linkedPullRequests: [] } ).action, "release" );
	const recent = new Date( T0 - HOUR_MS ).toISOString(), stale = new Date( T0 - 100 * HOUR_MS ).toISOString();
	const open = { state: "open", author: "rebraxin", updatedAt: recent };
	assert.equal( decideSweep( { lease, nowMs: T0 + 1, linkedPullRequests: [ open ] } ).action, "extend" );
	for (
		const pull of [ { ...open, state: "closed" }, { ...open, author: "someone" }, { ...open, updatedAt: stale } ]
	) {
		assert.equal( decideSweep( { lease, nowMs: T0 + 1, linkedPullRequests: [ pull ] } ).action, "release" );
	}
});

test("only the workflow's own latest marker is the lease", () => {
	const marker =
		'<!-- opensro-claim {"holder":"rebraxin","seat":"","until":"2026-11-17T12:00:00.000Z","hours":72} -->';
	const forged = { id: 1, user: { login: "someone" }, body: marker.replace( "rebraxin", "someone" ) };
	assert.equal( readLease( [ forged ], BOT ), null );
	const own = { id: 2, user: { login: BOT }, body: `Claimed\n\n${marker}` };
	assert.equal( readLease( [ own, forged ], BOT ).holder, "rebraxin" );
	const released = { id: 3, user: { login: BOT }, body: '<!-- opensro-claim {"released":true} -->' };
	assert.equal( readLease( [ own, released ], BOT ), null );
});

// ============================================================================

test("run: claim, refuse, release, then a takeover after expiry", async () => {
	const repo = fakeRepo();
	await repo.comment( "rebraxin", "/claim" );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "rebraxin" ] );
	assert.ok( repo.issue.labels.has( CLAIMED_LABEL ) );
	assert.match( repo.lastReply(), /^Claimed by @rebraxin until 2026-11-17T12:00:00\.000Z \(72 h\)/ );

	await repo.comment( "someone", "/claim" );
	assert.match( repo.lastReply(), /^Already claimed by @rebraxin/ );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "rebraxin" ] );

	await repo.comment( "someone", "/unclaim", "CONTRIBUTOR" );
	assert.match( repo.lastReply(), /^Only @rebraxin or a maintainer/ );

	await repo.comment( "rebraxin", "/unclaim" );
	assert.equal( repo.issue.assignees.size, 0 );
	assert.equal( repo.issue.labels.has( CLAIMED_LABEL ), false );
	assert.match( repo.lastReply(), /^Claim released by @rebraxin/ );

	await repo.comment( "rebraxin", "/claim" );
	const afterExpiry = T0 + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS;
	await repo.comment( "soyeonp92", "/claim T4001", "OWNER", afterExpiry );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "soyeonp92" ] );
	assert.match( repo.lastReply(), /^Claimed by @soyeonp92 \(T4001\)/ );
});

test("run: the sweep extends a claim with a moving pull request and releases an idle one", async () => {
	const repo = fakeRepo();
	await repo.comment( "rebraxin", "/claim" );
	const expired = T0 + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS;
	repo.issue.timeline.push( {
		event: "cross-referenced",
		source: {
			issue: {
				pull_request: {},
				state: "open",
				user: { login: "rebraxin" },
				updated_at: new Date( expired - HOUR_MS ).toISOString()
			}
		}
	} );
	await repo.sweep( expired );
	assert.deepEqual( [ ...repo.issue.assignees ], [ "rebraxin" ] );
	assert.match( repo.lastReply(), /^Claim renewed by @rebraxin/ );

	repo.issue.timeline.length = 0;
	await repo.sweep( expired + (CONTRIBUTOR_LEASE_HOURS + 1) * HOUR_MS );
	assert.equal( repo.issue.assignees.size, 0 );
	assert.equal( repo.issue.labels.has( CLAIMED_LABEL ), false );
	assert.match( repo.lastReply(), /expired with no activity and is released/ );
});

test("run: a comment that is no command changes nothing", async () => {
	const repo = fakeRepo();
	await repo.comment( "rebraxin", "I might /claim this later" );
	assert.equal( repo.issue.assignees.size, 0 );
	assert.equal( repo.lastReply(), "" );
});
