/*
===========================================================================

board.mjs - the Work board's one read: a collector's snapshot file

A collector, wherever the work is visible, writes one JSON snapshot (work
board contract v1) to the file the console's config names
(boardSnapshotFile); how the file reaches this host is the deployment's
business. This owner reads it with a size limit, passes on only the
fields the contract names, each checked for its type and bounded, and
adds its age. Snapshot text is data: no field is a URL or markup, and the
page builds every link itself from the configured repository and the
numbers here. Read-only; nothing here writes.

===========================================================================
*/
import { readFile, stat } from "node:fs/promises";

const SCHEMA_VERSION = 1;
const MAX_FILE_BYTES = 1024 * 1024;
const MAX_ROWS = 200;
const MAX_TEXT = 300;
const MAX_LIST = 64;
// The collector's cadence bounds; the page calls a snapshot stale past
// twice its interval.
const MIN_INTERVAL_S = 30;
const MAX_INTERVAL_S = 86400;
const DEFAULT_INTERVAL_S = 300;
const STALE_FACTOR = 2;
const REPO = /^[A-Za-z0-9-]{1,39}\/[A-Za-z0-9._-]{1,100}$/;
const SHA = /^[0-9a-f]{7,40}$/;
const THREAD = /^\d{1,24}$/;
const STEP_STATES = new Set( [ "done", "running", "waiting", "failed" ] );
const CI_STATES = new Set( [ "green", "red", "running", "none" ] );
// live: a live release carries the fix; fixed: the release in flight will.
const THREAD_ACTIONS = new Set( [ "live", "fixed", "close", "reply" ] );
const EVIDENCE_STATES = new Set( [ "done", "na", "working", "missing" ] );
const MAX_SEATS = 4;
const MAX_EVIDENCE = 12;
const SCOPE_PRIORITIES = new Set( [ "critical", "high", "medium", "low" ] );
const TASK_STATES = new Set( [ "queued", "claimed", "submitted" ] );

/*
================
text

A bounded string, or the fallback for anything else.
================
*/
function text( value, fallback = "" ) {
	return typeof value === "string" ? value.slice( 0, MAX_TEXT ) : fallback;
}

/*
================
count

A non-negative whole number, or null.
================
*/
function count( value ) {
	return Number.isInteger( value ) && value >= 0 ? value : null;
}

/*
================
time

An ISO time as milliseconds, or null.
================
*/
function time( value ) {
	if ( typeof value !== "string" ) return null;
	const ms = Date.parse( value );
	return Number.isFinite( ms ) ? ms : null;
}

/*
================
rows

Up to limit entries of a list, each through one row reader; entries it
refuses (null) drop out.
================
*/
function rows( list, read, limit = MAX_ROWS ) {
	return Array.isArray( list ) ? list.slice( 0, limit ).map( read ).filter( row => row !== null ) : [];
}

/*
================
numbers

A list of issue or PR numbers, whole and positive.
================
*/
function numbers( list ) {
	return rows( list, n => (count( n ) ? n : null), MAX_LIST );
}

/*
================
object

Whether a value is a plain record the readers may look into.
================
*/
function object( value ) {
	return value !== null && typeof value === "object" && !Array.isArray( value );
}

/*
================
releaseRow
================
*/
function releaseRow( r ) {
	if ( !object( r ) || typeof r.name !== "string" ) return null;
	return {
		name: text( r.name ),
		freeze: typeof r.freeze === "string" && SHA.test( r.freeze ) ? r.freeze : "",
		why: text( r.why ),
		// A scoped release (critical and high, owner 11-03): the collector
		// computes its one-line summary and open issues every snapshot.
		summary: text( r.summary ),
		issues: numbers( r.issues ),
		scope: rows( r.scope, p => (SCOPE_PRIORITIES.has( p ) ? p : null), 4 ),
		// live: the host serves the freeze on every component (the collector reads it).
		live: r.live === true,
		// assembling: the list is decided, nothing is frozen; waiting is the part still open.
		assembling: r.assembling === true,
		waiting: numbers( r.waiting ),
		steps: rows( r.steps, s =>
			object( s ) && typeof s.step === "string" ?
				{ step: text( s.step ), state: STEP_STATES.has( s.state ) ? s.state : "waiting" } :
				null, MAX_LIST ),
		contains: numbers( r.contains ),
		fixes: numbers( r.fixes ),
		next: object( r.next ) ? { name: text( r.next.name ), candidates: numbers( r.next.candidates ) } : null,
		rules: rows( r.rules, rule => (typeof rule === "string" ? text( rule ) : null), MAX_LIST )
	};
}

/*
================
needRow
================
*/
function needRow( n ) {
	if ( !object( n ) || typeof n.what !== "string" ) return null;
	// who: the owner unless the collector names the coordinator (a dead or
	// late coordinator session must show on the board, not hide in a log).
	return { what: text( n.what ), detail: text( n.detail ), who: n.who === "coordinator" ? "coordinator" : "owner" };
}

/*
================
agentRow
================
*/
function agentRow( a ) {
	if ( !object( a ) || typeof a.name !== "string" ) return null;
	return {
		name: text( a.name ),
		task: text( a.task ),
		tree: text( a.tree ),
		state: text( a.state ),
		blockedOn: text( a.blockedOn ),
		at: time( a.at )
	};
}

/*
================
prRow
================
*/
function prRow( p ) {
	if ( !object( p ) || !count( p.number ) || typeof p.title !== "string" ) return null;
	return {
		number: p.number,
		title: text( p.title ),
		author: text( p.author ),
		fork: p.fork === true,
		approvals: count( p.approvals ) ?? 0,
		needed: count( p.needed ) ?? 0,
		seats: rows( p.seats, seatRow, MAX_SEATS ),
		evidence: rows( p.evidence, evidenceRow, MAX_EVIDENCE ),
		ci: CI_STATES.has( p.ci ) ? p.ci : "none",
		blocker: text( p.blocker ),
		ready: p.ready === true,
		createdAt: time( p.createdAt )
	};
}

/*
================
seatRow

One kind of reviewer a PR needs (Cursor, Claude): approvals at the head
against the number the merge rule asks for.
================
*/
function seatRow( s ) {
	if ( !object( s ) || typeof s.name !== "string" || count( s.have ) === null || count( s.need ) === null ) {
		return null;
	}
	return { name: text( s.name ), have: s.have, need: s.need };
}

/*
================
evidenceRow

One kind of a PR's evidence: done, not applicable (with its reason),
working (a task is held; the note names it) or missing.
================
*/
function evidenceRow( e ) {
	if ( !object( e ) || typeof e.key !== "string" || !EVIDENCE_STATES.has( e.state ) ) return null;
	return { key: text( e.key ), state: e.state, note: text( e.note ) };
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
		priority: text( t.priority ),
		action: THREAD_ACTIONS.has( t.action ) ? t.action : "reply",
		issues: numbers( t.issues )
	};
}

/*
================
issuesRow
================
*/
function issuesRow( i ) {
	if ( !object( i ) ) return { open: 0, withoutPr: 0, labels: [] };
	return {
		open: count( i.open ) ?? 0,
		withoutPr: count( i.withoutPr ) ?? 0,
		labels: rows( i.labels, l =>
			object( l ) && typeof l.name === "string" && count( l.count ) !== null ?
				{ name: text( l.name ), count: l.count } :
				null, MAX_LIST )
	};
}

/*
================
scopeRow

One critical or high issue of the release scope (owner 11-03: all of
them ship in the next release): the agent task that holds it, if any,
and the open PR that cites it, if any.
================
*/
function scopeRow( r ) {
	if ( !object( r ) || !count( r.number ) || !SCOPE_PRIORITIES.has( r.priority ) ) return null;
	const t = r.task;
	const task = object( t ) && count( t.id ) && TASK_STATES.has( t.state ) ?
		{ id: t.id, assignee: text( t.assignee ), state: t.state, step: text( t.step ) } :
		null;
	return {
		number: r.number,
		title: text( r.title ),
		priority: r.priority,
		epic: r.epic === true,
		carried: r.carried === true,
		pr: count( r.pr ) || null,
		task
	};
}

/*
================
mergedRow
================
*/
function mergedRow( m ) {
	if ( !object( m ) || !count( m.number ) ) return null;
	return { number: m.number, title: text( m.title ), mergedAt: time( m.mergedAt ) };
}

/*
================
boardBody

The snapshot as the page may see it, or null when it is not v1 or has
no generation time. now is milliseconds.
================
*/
export function boardBody( body, now ) {
	if ( !object( body ) || body.schemaVersion !== SCHEMA_VERSION ) return null;
	const generatedAt = time( body.generatedAt );
	if ( generatedAt === null ) return null;
	const interval = count( body.intervalSeconds ) ?? DEFAULT_INTERVAL_S;
	const intervalSeconds = Math.min( MAX_INTERVAL_S, Math.max( MIN_INTERVAL_S, interval ) );
	const ageSeconds = Math.max( 0, Math.round( (now - generatedAt) / 1000 ) );
	return {
		schemaVersion: SCHEMA_VERSION,
		generatedAt,
		intervalSeconds,
		ageSeconds,
		stale: ageSeconds > intervalSeconds * STALE_FACTOR,
		release: releaseRow( body.release ),
		needs: rows( body.needs, needRow, MAX_LIST ),
		agents: rows( body.agents, agentRow, MAX_LIST ),
		prs: rows( body.prs, prRow ),
		threads: rows( body.threads, threadRow ),
		scope: rows( body.scope, scopeRow ),
		issues: issuesRow( body.issues ),
		merged: rows( body.merged, mergedRow, MAX_LIST )
	};
}

/*
================
createBoard

The board reader for one snapshot file and the repository its numbers
belong to. Either missing: the board is off and says so. clock is for
tests.
================
*/
export function createBoard( file, repo, clock = Date.now ) {
	const enabled = typeof file === "string" && file !== "" && typeof repo === "string" && REPO.test( repo );
	return {
		enabled,
		async request() {
			if ( !enabled ) return { status: 503, body: { error: "The work board is not configured on this console" } };
			let raw;
			try {
				if ( (await stat( file )).size > MAX_FILE_BYTES ) {
					return {
						status: 502,
						body: { error: "The board snapshot is larger than " + MAX_FILE_BYTES + " bytes" }
					};
				}
				raw = await readFile( file, "utf8" );
			} catch {
				return { status: 503, body: { error: "No board snapshot yet; the collector has not written one" } };
			}
			let body;
			try {
				body = boardBody( JSON.parse( raw ), clock() );
			} catch {
				body = null;
			}
			if ( !body ) return { status: 502, body: { error: "The board snapshot is not a v1 work board" } };
			return { status: 200, body: { ...body, repo } };
		}
	};
}
