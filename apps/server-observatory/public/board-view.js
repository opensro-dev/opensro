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
	const when = age < MINUTE_S ? "just now" : ago( age ) + " ago";
	return {
		stale,
		tone: stale ? "warn" : "ok",
		text: stale ? "Stale: data from " + when : "Data from " + when,
		// The page only re-reads; new data needs the collector to run again.
		title: stale ?
			"The collector should run every " + ago( body.intervalSeconds ) + " but last ran " + when +
			". Refreshing re-reads the board; it cannot make the collector run." :
			"The collector runs every " + ago( body.intervalSeconds ) + "; this page re-reads it every 30 s."
	};
}

/*
================
checkResult

What a read found, for the line beside Refresh: newer data, the same
data, or a failure. previous is the generatedAt shown before the read
(null on the first read); at is the page clock, as a time of day.
================
*/
export function checkResult( previous, body, error, at ) {
	const clock = new Date( at ).toLocaleTimeString();
	if ( error ) return { tone: "bad", text: "Checked " + clock + ": " + error };
	if ( previous === null || body.generatedAt > previous ) {
		return { tone: "ok", text: "Checked " + clock + ": up to date" };
	}
	return { tone: "warn", text: "Checked " + clock + ": no newer data yet" };
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
seatsText

A PR's approvals per kind of reviewer, "Cursor 1/2 · Claude 0/1", each
with whether it is met. A snapshot without seats falls back to the
plain count.
================
*/
export function seatsText( pr ) {
	if ( !pr.seats?.length ) return [ { text: pr.approvals + " / " + pr.needed, met: pr.approvals >= pr.needed } ];
	return pr.seats.map( s => ({ text: s.name + " " + s.have + "/" + s.need, met: s.have >= s.need }) );
}

// What each Evidence state looks like beside its line's name.
const EVIDENCE_MARKS = Object.freeze( {
	done: { tone: "ok", mark: "✓" },
	na: { tone: "idle", mark: "n/a" },
	working: { tone: "warn", mark: "…" },
	missing: { tone: "bad", mark: "✗" }
} );

/*
================
evidenceMarks

A PR's Evidence checklist as chips, in its order, each with its words so
no state is colour alone. title carries a not-applicable reason.
================
*/
export function evidenceMarks( pr ) {
	return (pr.evidence ?? []).map( e => {
		const mark = EVIDENCE_MARKS[e.state] ?? EVIDENCE_MARKS.missing;
		return { tone: mark.tone, text: e.key + " " + mark.mark, title: e.note || e.key + ": " + e.state };
	} );
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
	live: { tone: "ok", text: "Fixed and live: reply, close" },
	fixed: { tone: "warn", text: "Fixed by the release in flight" },
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

// Thread order: fixes already live (reply now), then fixes in flight, then
// what to close, then the rest; within each, the most urgent first.
const ACTION_RANK = Object.freeze( { live: 0, fixed: 1, close: 2, reply: 3 } );
const PRIORITY_RANK = Object.freeze( { bad: 0, warn: 1, idle: 2 } );

/*
================
sortThreads

Bug-report threads in the order someone acts on them; ties keep the
snapshot's order.
================
*/
export function sortThreads( threads ) {
	const rank = t => (ACTION_RANK[t.action] ?? ACTION_RANK.reply) * 3 + PRIORITY_RANK[priorityTone( t.priority )];
	return threads.map( ( t, i ) => [ rank( t ), i, t ] ).sort( ( a, b ) => a[0] - b[0] || a[1] - b[1] ).map( r =>
		r[2]
	);
}

/*
================
scopeStage

How far one release-scope issue got, for its row: a ready PR, an open
PR and what blocks it, an agent's task and step, an epic that closes
through its children, or nobody at all (the coordinator's to assign).
prs: the snapshot's PR rows, to read the cited PR's state.
================
*/
export function scopeStage( row, prs ) {
	if ( row.pr ) {
		const pr = prs.find( p => p.number === row.pr );
		if ( pr && pr.ready ) return { label: "ready", detail: "merges once the release allows", tone: "ok" };
		return { label: "in review", detail: pr && pr.blocker ? pr.blocker : "", tone: "warn" };
	}
	if ( row.task ) {
		const t = row.task;
		const step = t.state === "queued" ?
			"not claimed yet" :
			t.state === "submitted" ?
			"submitted" :
			t.step || "claimed";
		return { label: "fixing", detail: "T" + t.id + " " + t.assignee + ", " + step, tone: "warn" };
	}
	if ( row.carried ) return { label: "in release", detail: "code merged; live read after the release", tone: "ok" };
	if ( row.epic ) return { label: "epic", detail: "closes through its child issues", tone: "idle" };
	return { label: "no owner", detail: "the coordinator assigns it next", tone: "bad" };
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
splitNeeds

The needs that wait on the owner and those that wait on the coordinator
session, each in the snapshot's order.
================
*/
export function splitNeeds( needs ) {
	return {
		owner: needs.filter( n => n.who !== "coordinator" ),
		coordinator: needs.filter( n => n.who === "coordinator" )
	};
}

/*
================
pageChanged

True when the console serves page code other than what this tab loaded:
an open tab otherwise keeps painting new data with old code, which is how
a fixed card still read "Release in flight" on 11-03. Unknown either side
is never a change.
================
*/
export function pageChanged( loaded, served ) {
	return Boolean( loaded && served && loaded !== served );
}

/*
================
summary

The plaques across the top, in the order a reader acts on them.
================
*/
export function summary( body ) {
	const { agent, fork } = splitPrs( body.prs );
	const { owner } = splitNeeds( body.needs );
	const scope = (body.scope || []).filter( r => !r.epic && !r.carried );
	const loose = scope.filter( r => !r.pr && !r.task ).length;
	return [
		{ value: owner.length, label: "need the owner", tone: owner.length ? "warn" : "" },
		{
			value: scope.filter( r => r.pr ).length + " / " + scope.length,
			label: "critical and high with a PR",
			tone: loose ? "warn" : ""
		},
		{ value: agent.filter( pr => pr.ready ).length + " / " + agent.length, label: "agent PRs ready", tone: "" },
		{ value: fork.length, label: "fork PRs open", tone: "" },
		{ value: body.threads.length, label: "bug-report threads", tone: "" },
		{ value: body.issues.open, label: "issues open", tone: "" }
	];
}
