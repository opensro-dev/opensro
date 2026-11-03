/*
===========================================================================

board-view.js - what the Work board shows for each snapshot field

Pure functions from the work board contract v1 (board.mjs) to the
page's words and tones: ages, the snapshot's freshness, each PR's
blocker, each agent's state, and the GitHub links, which are built here
from the configured repository and a number, never taken from the
snapshot. No DOM here, so node tests read exactly what the page renders.

===========================================================================
*/

const GITHUB = "https://github.com/";
const MINUTE_S = 60;
const HOUR_S = 3600;
const DAY_S = 86400;

/*
================
ago

A duration in seconds as the page says it: "just now", "4 min", "3 h",
"2 d".
================
*/
export function ago( seconds ) {
	if ( seconds < MINUTE_S ) return "just now";
	if ( seconds < HOUR_S ) return Math.floor( seconds / MINUTE_S ) + " min";
	if ( seconds < DAY_S ) return Math.floor( seconds / HOUR_S ) + " h";
	return Math.floor( seconds / DAY_S ) + " d";
}

/*
================
since

How long ago a contract time (milliseconds, or null) was, at now.
================
*/
export function since( at, now ) {
	if ( at === null || at === undefined ) return "";
	return ago( Math.max( 0, Math.round( (now - at) / 1000 ) ) );
}

/*
================
freshness

The snapshot's age at the page's clock, and whether it is stale. The
server's ageSeconds was true when it answered; the page adds the time
since, so the age keeps moving between polls.
================
*/
export function freshness( body, answeredAt, now ) {
	const age = body.ageSeconds + Math.max( 0, Math.round( (now - answeredAt) / 1000 ) );
	const stale = age > body.intervalSeconds * 2;
	return {
		stale,
		tone: stale ? "warn" : "ok",
		text: (stale ? "Stale: collected " : "Collected ") + (age < MINUTE_S ? "just now" : ago( age ) + " ago")
	};
}

/*
================
pullURL
================
*/
export function pullURL( repo, number ) {
	return GITHUB + repo + "/pull/" + Number( number );
}

/*
================
issueURL
================
*/
export function issueURL( repo, number ) {
	return GITHUB + repo + "/issues/" + Number( number );
}

/*
================
ciTone
================
*/
export function ciTone( ci ) {
	return ci === "green" ? "ok" : ci === "red" ? "bad" : ci === "running" ? "warn" : "idle";
}

/*
================
prTone

A PR's blocker tone: ready is jade, red CI or requested changes red,
anything else still in progress amber.
================
*/
export function prTone( pr ) {
	if ( pr.ready ) return "ok";
	if ( pr.ci === "red" || /changes requested/i.test( pr.blocker ) ) return "bad";
	return "warn";
}

/*
================
agentTone

An agent's state tone: idle grey, a hand-off waiting on someone amber,
working jade.
================
*/
export function agentTone( state ) {
	const s = state.toLowerCase();
	if ( s === "" || s.includes( "idle" ) ) return "idle";
	if ( s.includes( "uncommitted" ) || s.includes( "waiting" ) || s.includes( "blocked" ) ) return "warn";
	return "ok";
}

/*
================
stepTone
================
*/
export function stepTone( state ) {
	return state === "done" ? "ok" : state === "running" ? "warn" : state === "failed" ? "bad" : "idle";
}

// What each bug-report thread action says.
const THREAD_ACTIONS = Object.freeze( {
	fixed: { tone: "ok", text: "Fixed by this release" },
	close: { tone: "ok", text: "Close thread" },
	reply: { tone: "idle", text: "Reply only" }
} );

/*
================
threadAction
================
*/
export function threadAction( action ) {
	return THREAD_ACTIONS[action] ?? THREAD_ACTIONS.reply;
}

/*
================
priorityTone
================
*/
export function priorityTone( priority ) {
	const p = priority.toLowerCase();
	if ( p.startsWith( "critical" ) || p.startsWith( "high" ) ) return "bad";
	if ( p.startsWith( "medium" ) ) return "warn";
	return "idle";
}

// Thread order: what this release fixes, then what to close, then the rest;
// within each, the most urgent first.
const ACTION_RANK = Object.freeze( { fixed: 0, close: 1, reply: 2 } );
const PRIORITY_RANK = Object.freeze( { bad: 0, warn: 1, idle: 2 } );

/*
================
sortThreads

Bug-report threads in the order someone acts on them; ties keep the
snapshot's order.
================
*/
export function sortThreads( threads ) {
	const rank = t => (ACTION_RANK[t.action] ?? 2) * 3 + PRIORITY_RANK[priorityTone( t.priority )];
	return threads.map( ( t, i ) => [ rank( t ), i, t ] ).sort( ( a, b ) => a[0] - b[0] || a[1] - b[1] ).map( r =>
		r[2]
	);
}

/*
================
splitPrs

Agent PRs (from the repository's own branches) and fork PRs, each by
number.
================
*/
export function splitPrs( prs ) {
	const sorted = [ ...prs ].sort( ( a, b ) => a.number - b.number );
	return { agent: sorted.filter( pr => !pr.fork ), fork: sorted.filter( pr => pr.fork ) };
}

/*
================
summary

The plaques across the top, in the order a reader acts on them.
================
*/
export function summary( body ) {
	const { agent, fork } = splitPrs( body.prs );
	return [
		{ value: body.needs.length, label: "need the owner", tone: body.needs.length ? "warn" : "" },
		{ value: agent.filter( pr => pr.ready ).length + " / " + agent.length, label: "agent PRs ready", tone: "" },
		{ value: fork.length, label: "fork PRs open", tone: "" },
		{ value: body.threads.length, label: "bug-report threads", tone: "" },
		{ value: body.issues.open, label: "issues open", tone: "" }
	];
}
