/*
===========================================================================

issueClaim.mjs - who is working on an issue, and until when

A claim is a lease kept on the issue itself, so contributors and the
maintainers' agents read and write the same record (two pull requests
for one topic came from claims kept where only one side could see
them). Commenting /claim takes the lease: the claimer is assigned, the
issue gets CLAIMED_LABEL, and the workflow's reply carries the lease in
a hidden marker. /claim again renews it, /unclaim gives it back. A lease
past its expiry is released by the sweep, unless an open pull request
by the holder that links the issue moved inside the lease window, which
extends it: active work never has to re-claim.

The maintainers' agents all comment from one account, so a lease also
names a seat (the first word after /claim). The same account with a
different seat is a different holder.

The decisions are pure functions over plain records; run() is the one
place that talks to GitHub (.github/workflows/claim.yml). Comment text
is parsed as data and never reaches a shell.

===========================================================================
*/

export const CLAIMED_LABEL = "status: claimed";
// A contributor's lease outlives a weekend; an agent seat works in hours
// and is re-seated by the queue, so a stale seat frees the issue sooner.
export const CONTRIBUTOR_LEASE_HOURS = 72;
export const MAINTAINER_LEASE_HOURS = 24;
// How many live leases one non-maintainer account may hold at once. Agents
// skip a claimed issue, so without a cap one account could claim every
// open issue and re-claim each the moment a maintainer released it: the
// pull would stop for nothing (ops #311: abuse must cost nothing). Two
// covers a contributor with one pull request in review and the next
// started. Maintainers' seats are not capped: the queue seats them.
export const MAX_CONTRIBUTOR_CLAIMS = 2;
const HOUR_MS = 60 * 60 * 1000;
const MARKER = /<!-- opensro-claim (\{[^\n]*?\}) -->/;
const SEAT_PATTERN = /^[A-Za-z0-9._-]{1,32}$/;
// GitHub's author_association values that hold write access here.
const MAINTAINER_ASSOCIATIONS = new Set( [ "OWNER", "MEMBER", "COLLABORATOR" ] );

/*
================
parseCommand

The claim command a comment starts with: { verb, seat } for "/claim",
"/claim <seat>" or "/unclaim", or null for any other comment. Only the
first line counts, so quoting a command further down does nothing.
================
*/
export function parseCommand( body ) {
	const first = String( body ?? "" ).trimStart().split( /\r?\n/, 1 )[0].trim();
	const match = /^\/(claim|unclaim)(?:\s+(\S+))?\s*$/.exec( first );
	if ( !match ) return null;
	const seat = match[2] ?? "";
	if ( seat && !SEAT_PATTERN.test( seat ) ) return null;
	return { verb: match[1], seat };
}

/*
================
isMaintainer
================
*/
export function isMaintainer( association ) {
	return MAINTAINER_ASSOCIATIONS.has( String( association ?? "" ) );
}

/*
================
leaseHours
================
*/
export function leaseHours( association ) {
	return isMaintainer( association ) ? MAINTAINER_LEASE_HOURS : CONTRIBUTOR_LEASE_HOURS;
}

/*
================
formatMarker

The hidden record a claim reply carries. released marks a reply that
ended the lease, so the latest marker alone says whether one is live.
================
*/
export function formatMarker( lease ) {
	return `<!-- opensro-claim ${JSON.stringify( lease )} -->`;
}

/*
================
readLease

The lease the latest workflow reply records, or null when there is
none or the latest one released it. Only replies the workflow itself
wrote count (botLogin): a hand-written marker is ignored.
================
*/
export function readLease( comments, botLogin ) {
	for ( let i = comments.length - 1; i >= 0; i-- ) {
		const comment = comments[i];
		if ( comment?.user?.login !== botLogin ) continue;
		const match = MARKER.exec( String( comment.body ?? "" ) );
		if ( !match ) continue;
		let record;
		try {
			record = JSON.parse( match[1] );
		} catch {
			continue;
		}
		if ( record?.released ) return null;
		if ( typeof record?.holder !== "string" || typeof record?.until !== "string" ) continue;
		return {
			holder: record.holder,
			seat: typeof record.seat === "string" ? record.seat : "",
			until: record.until,
			hours: Number( record.hours ) || CONTRIBUTOR_LEASE_HOURS,
			commentId: comment.id
		};
	}
	return null;
}

/*
================
holderName

How a reply names a holder: the login, and the seat when there is one.
================
*/
export function holderName( holder, seat ) {
	return seat ? `@${holder} (${seat})` : `@${holder}`;
}

/*
================
decideClaim

What a /claim does. grant: no live lease (none, or an expired one).
renew: the holder claims again. refuse: someone else holds it. limit:
a non-maintainer already holds MAX_CONTRIBUTOR_CLAIMS live leases on
other issues (held), so a new one is refused; a renewal never is.
================
*/
export function decideClaim( { lease, actor, seat, association, nowMs, held = 0 } ) {
	const hours = leaseHours( association );
	const until = new Date( nowMs + hours * HOUR_MS ).toISOString();
	if ( !lease || Date.parse( lease.until ) <= nowMs ) {
		if ( !isMaintainer( association ) && held >= MAX_CONTRIBUTOR_CLAIMS ) return { action: "limit" };
		return { action: "grant", until, hours };
	}
	if ( lease.holder === actor && lease.seat === seat ) return { action: "renew", until, hours };
	return { action: "refuse", lease };
}

/*
================
decideUnclaim

What an /unclaim does: release for the holder (any seat of the holder's
account, so a maintainer clears its own stale seat) or a maintainer,
refuse for anyone else, none when nothing is held.
================
*/
export function decideUnclaim( { lease, actor, association } ) {
	if ( !lease ) return { action: "none" };
	if ( lease.holder === actor || isMaintainer( association ) ) return { action: "release" };
	return { action: "refuse", lease };
}

/*
================
decideSweep

What the sweep does with one lease. keep: still inside its expiry.
extend: expired, but an open pull request by the holder that links the
issue was updated inside the lease window. release: expired and idle.
================
*/
export function decideSweep( { lease, nowMs, linkedPullRequests } ) {
	if ( Date.parse( lease.until ) > nowMs ) return { action: "keep" };
	const windowStart = nowMs - lease.hours * HOUR_MS;
	const active = linkedPullRequests.some( pull =>
		pull.state === "open" && pull.author === lease.holder && Date.parse( pull.updatedAt ) > windowStart
	);
	if ( active ) {
		return { action: "extend", until: new Date( nowMs + lease.hours * HOUR_MS ).toISOString() };
	}
	return { action: "release" };
}

// ============================================================================

/*
================
leaseReply

The reply text for a granted or renewed lease, marker included.
================
*/
function leaseReply( { holder, seat, until, hours, renewed } ) {
	const marker = formatMarker( { holder, seat, until, hours } );
	return [
		`${renewed ? "Claim renewed" : "Claimed"} by ${holderName( holder, seat )} until ${until} (${hours} h).`,
		"",
		"Pushing to a pull request that links this issue keeps the claim alive. Comment `/claim` to renew it by hand, or `/unclaim` to give it back.",
		"",
		marker
	].join( "\n" );
}

/*
================
listComments
================
*/
async function listComments( github, owner, repo, number ) {
	return github.paginate( github.rest.issues.listComments, { owner, repo, issue_number: number, per_page: 100 } );
}

/*
================
claimedIssues
================
*/
async function claimedIssues( github, owner, repo ) {
	const issues = await github.paginate( github.rest.issues.listForRepo, {
		owner,
		repo,
		state: "open",
		labels: CLAIMED_LABEL,
		per_page: 100
	} );
	return issues.filter( issue => !issue.pull_request );
}

/*
================
liveLeasesHeld

How many live leases actor holds on open issues other than except,
read from the workflow's own lease markers. Not from the assignees:
GitHub silently ignores an assignee it will not accept, so a count of
assignments would let such an account claim without limit.
================
*/
async function liveLeasesHeld( { github, owner, repo, actor, except, botLogin, nowMs } ) {
	let held = 0;
	for ( const issue of await claimedIssues( github, owner, repo ) ) {
		if ( issue.number === except ) continue;
		const lease = readLease( await listComments( github, owner, repo, issue.number ), botLogin );
		if ( lease && lease.holder === actor && Date.parse( lease.until ) > nowMs ) held++;
	}
	return held;
}

/*
================
linkedPullRequests

Open or closed pull requests that reference the issue, from its
timeline's cross-reference events, as { state, author, updatedAt }.
================
*/
async function linkedPullRequests( github, owner, repo, number ) {
	const events = await github.paginate( github.rest.issues.listEventsForTimeline, {
		owner,
		repo,
		issue_number: number,
		per_page: 100
	} );
	return events
		.filter( event => event.event === "cross-referenced" && event.source?.issue?.pull_request )
		.map( event => ({
			state: event.source.issue.state,
			author: event.source.issue.user?.login ?? "",
			updatedAt: event.source.issue.updated_at
		}) );
}

/*
================
release

Ends a lease: unassigns the holder, drops the label and records it.
================
*/
async function release( { github, owner, repo, number, lease, text } ) {
	await github.rest.issues.removeAssignees( { owner, repo, issue_number: number, assignees: [ lease.holder ] } );
	await github.rest.issues.removeLabel( { owner, repo, issue_number: number, name: CLAIMED_LABEL } ).catch(
		error => {
			if ( error?.status !== 404 ) throw error;
		}
	);
	await github.rest.issues.createComment( {
		owner,
		repo,
		issue_number: number,
		body: `${text}\n\n${formatMarker( { released: true } )}`
	} );
}

/*
================
handleComment

One /claim or /unclaim comment on an issue (pull request comments and
the workflow's own replies are ignored).
================
*/
async function handleComment( { github, context, botLogin, nowMs } ) {
	const { comment, issue } = context.payload;
	if ( !comment || !issue || issue.pull_request || comment.user?.type === "Bot" ) return;
	const command = parseCommand( comment.body );
	if ( !command ) return;
	const { owner, repo } = context.repo;
	const number = issue.number, actor = comment.user.login, association = comment.author_association;
	const lease = readLease( await listComments( github, owner, repo, number ), botLogin );
	if ( command.verb === "claim" ) {
		if ( issue.state !== "open" ) return;
		const fresh = !lease || Date.parse( lease.until ) <= nowMs;
		const held = fresh && !isMaintainer( association ) ?
			await liveLeasesHeld( { github, owner, repo, actor, except: number, botLogin, nowMs } ) :
			0;
		const decision = decideClaim( { lease, actor, seat: command.seat, association, nowMs, held } );
		if ( decision.action === "limit" ) {
			await github.rest.issues.createComment( {
				owner,
				repo,
				issue_number: number,
				body: `@${actor} already holds ${MAX_CONTRIBUTOR_CLAIMS} claims, the most one account holds at once. ` +
					"Finish or `/unclaim` one of them first."
			} );
			return;
		}
		if ( decision.action === "refuse" ) {
			await github.rest.issues.createComment( {
				owner,
				repo,
				issue_number: number,
				body: `Already claimed by ${
					holderName( decision.lease.holder, decision.lease.seat )
				} until ${decision.lease.until}. Ask them here, or pick another issue.`
			} );
			return;
		}
		// Taking over an expired lease: the old holder is no longer on it.
		if ( lease && lease.holder !== actor ) {
			await github.rest.issues.removeAssignees( {
				owner,
				repo,
				issue_number: number,
				assignees: [ lease.holder ]
			} );
		}
		await github.rest.issues.addAssignees( { owner, repo, issue_number: number, assignees: [ actor ] } );
		await github.rest.issues.addLabels( { owner, repo, issue_number: number, labels: [ CLAIMED_LABEL ] } );
		await github.rest.issues.createComment( {
			owner,
			repo,
			issue_number: number,
			body: leaseReply( {
				holder: actor,
				seat: command.seat,
				until: decision.until,
				hours: decision.hours,
				renewed: decision.action === "renew"
			} )
		} );
		return;
	}
	const decision = decideUnclaim( { lease, actor, association } );
	if ( decision.action === "refuse" ) {
		await github.rest.issues.createComment( {
			owner,
			repo,
			issue_number: number,
			body: `Only ${
				holderName( decision.lease.holder, decision.lease.seat )
			} or a maintainer can release this claim.`
		} );
		return;
	}
	if ( decision.action === "release" ) {
		await release( { github, owner, repo, number, lease, text: `Claim released by @${actor}.` } );
	}
}

/*
================
sweep

Every open issue carrying the label: extend the active, release the
idle, and drop the label from an issue whose lease is already gone. A
GitHub failure on one issue (rate limit, 5xx, a deleted account) leaves
that one for the next sweep; the others are still decided.
================
*/
async function sweep( { github, context, botLogin, nowMs, core } ) {
	const { owner, repo } = context.repo;
	for ( const issue of await claimedIssues( github, owner, repo ) ) {
		try {
			await sweepIssue( { github, owner, repo, number: issue.number, botLogin, nowMs } );
		} catch ( error ) {
			core?.warning( `claim sweep: #${issue.number}: ${error?.message}` );
		}
	}
}

/*
================
sweepIssue
================
*/
async function sweepIssue( { github, owner, repo, number, botLogin, nowMs } ) {
	const lease = readLease( await listComments( github, owner, repo, number ), botLogin );
	if ( !lease ) {
		await github.rest.issues.removeLabel( { owner, repo, issue_number: number, name: CLAIMED_LABEL } );
		return;
	}
	const decision = decideSweep( {
		lease,
		nowMs,
		linkedPullRequests: await linkedPullRequests( github, owner, repo, number )
	} );
	if ( decision.action === "extend" ) {
		await github.rest.issues.updateComment( {
			owner,
			repo,
			comment_id: lease.commentId,
			body: leaseReply( { ...lease, until: decision.until, renewed: true } )
		} );
	} else if ( decision.action === "release" ) {
		await release( {
			github,
			owner,
			repo,
			number,
			lease,
			text: `Claim by ${
				holderName( lease.holder, lease.seat )
			} expired with no activity and is released. Comment \`/claim\` to take it.`
		} );
	}
}

/*
================
run

The workflow's entry: a comment event handles that comment; a schedule
or a manual dispatch sweeps every claimed issue.
================
*/
export async function run( { github, context, nowMs = Date.now(), core = null } ) {
	const botLogin = "github-actions[bot]";
	if ( context.eventName === "issue_comment" ) {
		await handleComment( { github, context, botLogin, nowMs } );
		return;
	}
	await sweep( { github, context, botLogin, nowMs, core } );
}
