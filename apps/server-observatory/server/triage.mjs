/*
===========================================================================

triage.mjs - the #bug-reports drafts: one read, one decision write

A collector writes the drafted replies as one JSON snapshot (triage
contract v1) to the file the console's config names (triageSnapshotFile).
This owner reads it bounded and typed, the way board.mjs reads the work
board, and overlays the decisions staff made here since.

A decision is the one write: approve or reject a thread's draft, with an
optional note, appended as one JSON line to triageDecisionsFile. The
collector reads that file back into its store; until it does, the
overlay shows the decision at once. Approving posts nothing anywhere: the
page offers the reply to copy, and whoever posts it does so by hand.

Draft text is data. It is shown as text, never as markup, and nothing in
it is read for meaning here.

===========================================================================
*/
import { appendFile, readFile, stat } from "node:fs/promises";

const SCHEMA_VERSION = 1;
const MAX_FILE_BYTES = 2 * 1024 * 1024;
const MAX_DECISIONS_BYTES = 1024 * 1024;
const MAX_THREADS = 200;
const MAX_TEXT = 300;
const MAX_DRAFT = 8000;
const MAX_NOTE = 500;
const MAX_ISSUES = 20;
const THREAD = /^\d{6,24}$/;
const PRIORITIES = new Set( [ "Critical", "High", "Medium", "Low" ] );
const ACTIONS = new Set( [ "reply", "close", "fixed", "live" ] );
const STATES = new Set( [ "pending", "approved", "rejected" ] );
const DECISIONS = new Map( [ [ "approve", "approved" ], [ "reject", "rejected" ] ] );

/*
================
text
================
*/
function text( value, limit = MAX_TEXT ) {
	return typeof value === "string" ? value.slice( 0, limit ) : "";
}

function object( value ) {
	return value !== null && typeof value === "object" && !Array.isArray( value );
}

/*
================
decisionRow

A decision as the snapshot or the decisions file carries it, or null.
================
*/
function decisionRow( d ) {
	if ( !object( d ) || !STATES.has( d.state ) ) return null;
	const at = typeof d.at === "string" && Number.isFinite( Date.parse( d.at ) ) ? d.at : "";
	return { state: d.state, by: text( d.by, 64 ), at, note: text( d.note, MAX_NOTE ) };
}

/*
================
threadRow
================
*/
function threadRow( t ) {
	if ( !object( t ) || typeof t.thread !== "string" || !THREAD.test( t.thread ) ) return null;
	return {
		thread: t.thread,
		title: text( t.title ),
		priority: PRIORITIES.has( t.priority ) ? t.priority : "",
		action: ACTIONS.has( t.action ) ? t.action : "reply",
		issues: Array.isArray( t.issues ) ?
			t.issues.slice( 0, MAX_ISSUES ).filter( n => Number.isInteger( n ) && n > 0 ) :
			[],
		draft: text( t.draft, MAX_DRAFT ),
		decision: decisionRow( t.decision ) ?? { state: "pending", by: "", at: "", note: "" }
	};
}

/*
================
triageBody

The page's view of a snapshot plus the decisions made since, or null for
a snapshot that is not contract v1. The last decision per thread wins.
================
*/
export function triageBody( snapshot, decisions ) {
	if ( !object( snapshot ) || snapshot.schemaVersion !== SCHEMA_VERSION ) return null;
	if ( typeof snapshot.generatedAt !== "string" || !Number.isFinite( Date.parse( snapshot.generatedAt ) ) ) {
		return null;
	}
	const threads = Array.isArray( snapshot.threads ) ?
		snapshot.threads.slice( 0, MAX_THREADS ).map( threadRow ).filter( Boolean ) :
		[];
	const byThread = new Map( threads.map( t => [ t.thread, t ] ) );
	for ( const d of decisions ) {
		const t = byThread.get( d.thread );
		// A decision older than the one the snapshot already holds is history.
		if ( t && d.at >= t.decision.at ) t.decision = { state: d.state, by: d.by, at: d.at, note: d.note };
	}
	return { generatedAt: snapshot.generatedAt, threads };
}

/*
================
parseDecisions

The decisions file's lines, each one a typed decision; any other line is
skipped (a torn last write, a hand edit).
================
*/
export function parseDecisions( text ) {
	const out = [];
	for ( const line of text.split( "\n" ) ) {
		if ( !line.trim() ) continue;
		let d;
		try {
			d = JSON.parse( line );
		} catch {
			continue;
		}
		const row = decisionRow( d );
		if ( row && typeof d.thread === "string" && THREAD.test( d.thread ) ) out.push( { thread: d.thread, ...row } );
	}
	return out;
}

/*
================
createTriage

snapshotFile and decisionsFile come from the console config; either
absent, the page says the drafts are off. clock is injectable for tests.
================
*/
export function createTriage( snapshotFile, decisionsFile, clock = () => new Date() ) {
	const enabled = typeof snapshotFile === "string" && snapshotFile !== "" && typeof decisionsFile === "string" &&
		decisionsFile !== "";

	async function decisions() {
		try {
			if ( (await stat( decisionsFile )).size > MAX_DECISIONS_BYTES ) return [];
			return parseDecisions( await readFile( decisionsFile, "utf8" ) );
		} catch {
			return [];
		}
	}

	async function snapshot() {
		let raw;
		try {
			if ( (await stat( snapshotFile )).size > MAX_FILE_BYTES ) {
				return { status: 502, body: { error: "The drafts snapshot is larger than the console reads" } };
			}
			raw = await readFile( snapshotFile, "utf8" );
		} catch {
			return { status: 503, body: { error: "No drafts snapshot yet" } };
		}
		let body;
		try {
			body = triageBody( JSON.parse( raw ), await decisions() );
		} catch {
			body = null;
		}
		return body ?
			{ status: 200, body } :
			{ status: 502, body: { error: "The drafts snapshot is not contract v1" } };
	}

	return {
		enabled,

		/*
		================
		request
		================
		*/
		async request() {
			if ( !enabled ) return { status: 503, body: { error: "The #bug-reports drafts are off on this console" } };
			return snapshot();
		},

		/*
		================
		decide

		Record one decision by operator. The thread must be in the current
		snapshot; the answer is the page's view with the decision applied.
		================
		*/
		async decide( operator, body ) {
			if ( !enabled ) return { status: 503, body: { error: "The #bug-reports drafts are off on this console" } };
			if ( !object( body ) || !DECISIONS.has( body.decision ) || typeof body.thread !== "string" ) {
				return { status: 400, body: { error: "Send { thread, decision: approve | reject, note? }" } };
			}
			const current = await snapshot();
			if ( current.status !== 200 ) return current;
			if ( !current.body.threads.some( t => t.thread === body.thread ) ) {
				return { status: 404, body: { error: "No draft for that thread" } };
			}
			const note = text( body.note, MAX_NOTE ).trim();
			if ( body.decision === "reject" && !note ) {
				return { status: 400, body: { error: "Say why the draft is rejected, so it can be redrafted" } };
			}
			const row = {
				thread: body.thread,
				state: DECISIONS.get( body.decision ),
				by: text( operator, 64 ),
				at: clock().toISOString(),
				note
			};
			await appendFile( decisionsFile, JSON.stringify( row ) + "\n", "utf8" );
			return snapshot();
		}
	};
}
