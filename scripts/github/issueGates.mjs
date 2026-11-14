/*
===========================================================================

issueGates.mjs - a gated issue says when it opens, and opens then

`status: gated` keeps an issue out of the work pull until something it
needs exists. On 2026-11-14 thirty-two issues carried it, and the reason
they piled up was that no gate ever said what would lift it, so nothing
lifted it: #2518 and #2519 waited a week on #2517 after it had closed.

Every gated issue now carries one line in its body:

	Ungate when: <condition> [and <condition> ...]

with these conditions, each one checkable from GitHub and the public
release record:

	#12 closed            every issue or pull request named is closed
	#12 #13 closed
	live carries #12      pull request #12 is merged and the live release
	                      (opensro.online/releases/production.json) is at
	                      or after its merge commit
	after 2026-11-15T00:00Z   the time has passed
	owner decides         a decision only the owner makes; never lifted
	                      here, listed for the owner's batched questions
	container             a backlog that holds items rather than waiting;
	                      never lifted

The sweep (.github/workflows/gates.yml) lifts the label from every issue
whose conditions all hold and comments what held. A gated issue with no
line, or a line it cannot read, gets one comment asking for it. The
decisions are pure functions; run() is the one place that talks to
GitHub. Issue text is parsed as data and never reaches a shell.

===========================================================================
*/

export const GATED_LABEL = "status: gated";
export const PRODUCTION_URL = "https://opensro.online/releases/production.json";
const LINE = /^\s*ungate when:\s*(.+?)\s*$/im;
const ASK_MARKER = "<!-- opensro-gates: ask -->";
const ISSUE_REF = /^#(\d+)$/;
const TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}Z$/;
const NOT_FOUND = 404;

/*
================
parseCondition

One condition's words as a record, or null when they match no form.
================
*/
export function parseCondition( text ) {
	const words = text.trim().split( /\s+/ );
	const lower = words.map( word => word.toLowerCase() );
	if ( lower.join( " " ) === "owner decides" ) return { kind: "owner" };
	if ( lower.join( " " ) === "container" ) return { kind: "container" };
	if ( lower[0] === "after" && words.length === 2 && TIME.test( words[1] ) ) {
		const atMs = Date.parse( words[1].replace( "Z", ":00Z" ) );
		// The shape alone admits 2026-02-30, which V8 rolls over to March 2
		// (and 25:00, which it refuses): only a time that prints back as
		// written is the time the gate means.
		const exact = !Number.isNaN( atMs ) && new Date( atMs ).toISOString().slice( 0, 16 ) + "Z" === words[1];
		return exact ? { kind: "after", atMs } : null;
	}
	if ( lower[0] === "live" && lower[1] === "carries" && words.length === 3 && ISSUE_REF.test( words[2] ) ) {
		return { kind: "live", pull: Number( words[2].slice( 1 ) ) };
	}
	if ( lower.at( -1 ) === "closed" && words.length >= 2 ) {
		const refs = words.slice( 0, -1 ).map( word => word.replace( /,$/, "" ) );
		if ( refs.every( ref => ISSUE_REF.test( ref ) ) ) {
			return { kind: "closed", issues: refs.map( ref => Number( ref.slice( 1 ) ) ) };
		}
	}
	return null;
}

/*
================
parseGate

The issue body's Ungate when line as { conditions } or { error }. A
missing line is { missing: true }.
================
*/
export function parseGate( body ) {
	const match = LINE.exec( body ?? "" );
	if ( !match ) return { missing: true };
	const parts = match[1].split( /\s+and\s+/i );
	const conditions = parts.map( parseCondition );
	const bad = parts.filter( ( _, n ) => conditions[n] === null );
	if ( bad.length ) return { error: "cannot read: " + bad.join( "; " ) };
	return { conditions };
}

/*
================
conditionHolds

Whether one condition holds in the world record:
{ nowMs, closed: Set<number>, carried: Set<number> }.
================
*/
export function conditionHolds( condition, world ) {
	switch ( condition.kind ) {
		case "closed":
			return condition.issues.every( number => world.closed.has( number ) );
		case "live":
			return world.carried.has( condition.pull );
		case "after":
			return world.nowMs >= condition.atMs;
		default:
			// owner and container are never lifted by the sweep.
			return false;
	}
}

/*
================
decideGate

What the sweep does with one gated issue: "lift" (every condition
holds), "ask" (no readable line, and not asked yet), or "wait".
================
*/
export function decideGate( { gate, asked, world } ) {
	if ( gate.missing || gate.error ) return asked ? { action: "wait" } : { action: "ask" };
	if ( gate.conditions.every( condition => conditionHolds( condition, world ) ) ) return { action: "lift" };
	return { action: "wait" };
}

/*
================
describe

A condition in the words the lift comment uses.
================
*/
function describe( condition ) {
	switch ( condition.kind ) {
		case "closed":
			return condition.issues.map( number => "#" + number ).join( ", " ) + " closed";
		case "live":
			return "the live release carries #" + condition.pull;
		case "after":
			return new Date( condition.atMs ).toISOString().slice( 0, 16 ) + "Z has passed";
		default:
			return condition.kind;
	}
}

// ============================================================================

/*
================
issueClosed
================
*/
async function issueClosed( github, owner, repo, number ) {
	const data = await found( github.rest.issues.get( { owner, repo, issue_number: number } ) );
	return data === null ? null : data.state === "closed";
}

/*
================
found

A GitHub read's data, or null when the number names nothing (404): a typo
in one issue's line is that issue's error to ask about, never a throw that
ends the sweep for every gated issue after it. Any other failure throws.
================
*/
async function found( request ) {
	try {
		return (await request).data;
	} catch ( error ) {
		if ( error?.status === NOT_FOUND ) return null;
		throw error;
	}
}

/*
================
liveCarries

Pull request merged, and the live commit is its merge commit or after it.
================
*/
async function liveCarries( github, owner, repo, pull, liveCommit ) {
	const data = await found( github.rest.pulls.get( { owner, repo, pull_number: pull } ) );
	if ( data === null ) return null; // an issue number, or none: not a pull request
	if ( !liveCommit || !data.merged || !data.merge_commit_sha ) return false;
	const { data: compare } = await github.rest.repos.compareCommits( {
		owner,
		repo,
		base: data.merge_commit_sha,
		head: liveCommit
	} );
	return compare.status === "ahead" || compare.status === "identical";
}

/*
================
readLiveCommit

The live release's game commit, or "" when the record cannot be read (a
live condition then waits; it never lifts on a guess).
================
*/
async function readLiveCommit( fetchJson ) {
	try {
		const record = await fetchJson( PRODUCTION_URL );
		const commit = record?.client?.commit ?? "";
		return /^[0-9a-f]{40}$/.test( commit ) ? commit : "";
	} catch {
		return "";
	}
}

/*
================
askText
================
*/
function askText( gate ) {
	const why = gate.error ? "Its `Ungate when:` line " + gate.error + "." : "It has no `Ungate when:` line.";
	return [
		ASK_MARKER,
		"This issue is gated, but nothing says when the gate lifts. " + why,
		"",
		"Add one line to the issue body, for example:",
		"",
		"```",
		"Ungate when: #2519 closed",
		"Ungate when: live carries #3199 and after 2026-11-16T06:00Z",
		"Ungate when: owner decides",
		"```",
		"",
		"The forms are `#N closed` (one or more), `live carries #N` (a merged pull request), `after YYYY-MM-DDTHH:MMZ`, " +
		"`owner decides` and `container`, joined with `and`. The gates sweep lifts the label once every condition holds " +
		"(scripts/github/issueGates.mjs)."
	].join( "\n" );
}

/*
================
sweepIssue

One gated issue: read what its conditions name (cached per number across
the sweep), decide, act, and return its summary line. A number that names
nothing makes the line unreadable, so the issue is asked about once.
================
*/
async function sweepIssue( sweep, issue ) {
	const { github, owner, repo } = sweep;
	let gate = parseGate( issue.body );
	const world = { nowMs: sweep.nowMs, closed: new Set(), carried: new Set() };
	const unknown = [];
	for ( const condition of gate.conditions ?? [] ) {
		for ( const number of condition.kind === "closed" ? condition.issues : [] ) {
			if ( !sweep.closed.has( number ) ) {
				sweep.closed.set( number, await issueClosed( github, owner, repo, number ) );
			}
			const closed = sweep.closed.get( number );
			if ( closed === null ) unknown.push( `#${number} is not an issue or pull request` );
			else if ( closed ) world.closed.add( number );
		}
		if ( condition.kind === "live" ) {
			const pull = condition.pull;
			if ( !sweep.carried.has( pull ) ) {
				sweep.carried.set( pull, await liveCarries( github, owner, repo, pull, sweep.liveCommit ) );
			}
			const carried = sweep.carried.get( pull );
			if ( carried === null ) unknown.push( `#${pull} is not a pull request` );
			else if ( carried ) world.carried.add( pull );
		}
	}
	if ( unknown.length ) gate = { error: "names what is not there: " + unknown.join( "; " ) };
	let asked = false;
	if ( gate.missing || gate.error ) {
		const comments = await github.paginate( github.rest.issues.listComments, {
			owner,
			repo,
			issue_number: issue.number,
			per_page: 100
		} );
		asked = comments.some( comment => (comment.body ?? "").includes( ASK_MARKER ) );
	}
	const { action } = decideGate( { gate, asked, world } );
	if ( action === "lift" ) {
		await github.rest.issues.removeLabel( { owner, repo, issue_number: issue.number, name: GATED_LABEL } );
		await github.rest.issues.createComment( {
			owner,
			repo,
			issue_number: issue.number,
			body: "Ungated by the gates sweep: " + gate.conditions.map( describe ).join( " and " ) + ". Ready to pull."
		} );
	} else if ( action === "ask" ) {
		await github.rest.issues.createComment( { owner, repo, issue_number: issue.number, body: askText( gate ) } );
	}
	const kinds = (gate.conditions ?? []).map( condition => condition.kind ).join( "+" ) ||
		(gate.error ? "unreadable" : "missing");
	return `#${issue.number} ${action} (${kinds})`;
}

/*
================
run

The sweep: every open gated issue is read, decided and acted on. The
GitHub reads are cached per number within one run.
================
*/
export async function run( { github, context, nowMs = Date.now(), fetchJson = defaultFetchJson, core = null } ) {
	const { owner, repo } = context.repo;
	const issues = await github.paginate( github.rest.issues.listForRepo, {
		owner,
		repo,
		state: "open",
		labels: GATED_LABEL,
		per_page: 100
	} );
	const sweep = {
		github,
		owner,
		repo,
		nowMs,
		liveCommit: await readLiveCommit( fetchJson ),
		closed: new Map(),
		carried: new Map()
	};
	const summary = [];
	for ( const issue of issues ) {
		if ( issue.pull_request ) continue;
		try {
			summary.push( await sweepIssue( sweep, issue ) );
		} catch ( error ) {
			// A GitHub failure on one issue (rate limit, 5xx) leaves that one
			// gated until the next sweep; the others are still decided.
			summary.push( `#${issue.number} wait (error: ${error?.status ?? error?.message})` );
			core?.warning( `gates: #${issue.number}: ${error?.message}` );
		}
	}
	if ( core ) {
		core.summary.addHeading( "Gated issues" ).addList( summary.length ? summary : [ "none" ] );
		await core.summary.write();
	}
	return summary;
}

/*
================
defaultFetchJson
================
*/
async function defaultFetchJson( url ) {
	const response = await fetch( url, { signal: AbortSignal.timeout( 15000 ) } );
	if ( !response.ok ) throw new Error( "HTTP " + response.status );
	return response.json();
}
