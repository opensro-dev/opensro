/*
===========================================================================

locked-run.mjs - run one command only while holding the shared run lock

Several agents share one machine. Benchmarks and full checks must not
overlap, or every number they produce is wrong. This wrapper acquires the
lock, then spawns: the child never starts unless the lock was created by
this process (exclusive create, 'wx'). It writes its own START and END
lines to the shared journal, and releases the lock only while the lock
still holds this run's token, on every exit path.

	node scripts/coordination/locked-run.mjs --owner NAME --purpose "server gate" --minutes 10 -- go test ./...
	node scripts/coordination/locked-run.mjs --owner NAME --purpose "capture" --minutes 12 --wait --estimate 300 --shell -- run.cmd

The coordination directory comes from SRO_COORDINATION_DIR and holds
benchmark.lock, benchmark.queue and the journal (append_only.txt, or
SRO_COORDINATION_JOURNAL). There is no default: a private lock nobody else
reads would look like coordination and protect nothing.

Exit codes: the child's code; 75 when the lock is held or the wait timed
out (nothing ran); 78 when the preflight failed (nothing ran); 64 for bad
usage. The lock line is "OWNER HH:MM:SS purpose expires HH:MM token=<hex>
pid=<n>".

With --wait the run joins a FIFO queue instead of failing on a held lock:
jobs estimated at SHORT_JOB_SECONDS or less go ahead of long ones that have
not started, a long ticket that waited STARVATION_MS joins the short class,
and each class runs in arrival order. A ticket whose waiter died is dropped
on read. Without --wait a run fails at once, and also when anyone is
queued, so it cannot jump the queue.

--preflight "<command>" runs a cheap readiness check through the shell
before the run queues; a failure costs no lock time. Keep it cheap
(directories, files, free ports), never a module-graph warm-up or a game
launch, which would load the machine while someone else measures.

What it does not do: stop the child at expiry or clean up the child's own
descendants. Expiry is a promise to other readers. A stale lock (expired
and its pid gone) is moved aside only with --break-stale.

===========================================================================
*/

import { spawn, execFileSync } from "node:child_process";
import { appendFileSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { join } from "node:path";

const EXIT_USAGE = 64, EXIT_HELD = 75, EXIT_PREFLIGHT = 78;
const MAX_MINUTES = 120;
// Jobs at or under this estimate go ahead of long jobs still waiting.
const SHORT_JOB_SECONDS = 60;
const QUEUE_POLL_MS = 1000, MAX_WAIT_MINUTES = 90;
// A long ticket that has waited this long joins the short class, so a
// steady stream of short jobs cannot starve it.
const STARVATION_MS = 10 * 60000;
const PREFLIGHT_TIMEOUT_MS = 30000;
// The journal's time zone label; lines read "HH:MM:SS ZONE [OWNER] ...".
const ZONE = process.env.SRO_COORDINATION_ZONE ?? "LOCAL";

const DIRECTORY = process.env.SRO_COORDINATION_DIR?.trim() ?? "";
const LOCK = DIRECTORY ? join( DIRECTORY, "benchmark.lock" ) : "";
const QUEUE = DIRECTORY ? join( DIRECTORY, "benchmark.queue" ) : "";
const JOURNAL = process.env.SRO_COORDINATION_JOURNAL?.trim() || (DIRECTORY ? join( DIRECTORY, "append_only.txt" ) : "");

/*
================
clock

Local machine time, as the journal and lock lines use it.
================
*/
function clock( date = new Date() ) {
	return date.toTimeString().slice( 0, 8 );
}

/*
================
parseArgs
================
*/
function parseArgs( argv ) {
	const split = argv.indexOf( "--" );
	if ( split < 0 || split === argv.length - 1 ) return null;
	const options = {
		owner: "",
		purpose: "",
		minutes: 0,
		shell: false,
		breakStale: false,
		wait: false,
		estimate: 0,
		preflight: ""
	};
	const flags = argv.slice( 0, split );
	for ( let i = 0; i < flags.length; i++ ) {
		const flag = flags[i];
		if ( flag === "--shell" ) options.shell = true;
		else if ( flag === "--break-stale" ) options.breakStale = true;
		else if ( flag === "--owner" ) options.owner = flags[++i] ?? "";
		else if ( flag === "--purpose" ) options.purpose = flags[++i] ?? "";
		else if ( flag === "--minutes" ) options.minutes = Number( flags[++i] );
		else if ( flag === "--wait" ) options.wait = true;
		else if ( flag === "--estimate" ) options.estimate = Number( flags[++i] );
		else if ( flag === "--preflight" ) options.preflight = flags[++i] ?? "";
		else return null;
	}
	const valid = /^[A-Za-z0-9_-]{1,32}$/.test( options.owner ) && options.purpose.trim() &&
		!/[\r\n]/.test( options.purpose ) && Number.isFinite( options.minutes ) &&
		options.minutes > 0 && options.minutes <= MAX_MINUTES;
	if ( !valid || !Number.isFinite( options.estimate ) || options.estimate < 0 ) return null;
	if ( !options.estimate ) options.estimate = options.minutes * 60;
	return { ...options, command: argv[split + 1], args: argv.slice( split + 2 ) };
}

/*
================
journal
================
*/
function journal( owner, text ) {
	appendFileSync( JOURNAL, `\n${clock()} ${ZONE} [${owner}] ${text}\n` );
}

/*
================
alive

Whether a pid still names a running process (signal 0 only tests).
================
*/
function alive( pid ) {
	if ( !Number.isInteger( pid ) || pid <= 0 ) return false;
	try {
		process.kill( pid, 0 );
		return true;
	} catch ( error ) {
		return error.code === "EPERM";
	}
}

/*
================
describeHolder

The current lock line, whether its expiry has passed and whether its pid runs.
================
*/
function describeHolder() {
	let line;
	try {
		line = readFileSync( LOCK, "utf8" ).trim();
	} catch {
		return null;
	}
	const expires = /expires (\d{2}):(\d{2})/.exec( line );
	const pid = Number( /pid=(\d+)/.exec( line )?.[1] );
	let expired = false;
	if ( expires ) {
		const at = new Date();
		at.setHours( Number( expires[1] ), Number( expires[2] ), 0, 0 );
		// A lock taken before midnight that expires after it: the expiry
		// clock reads earlier than the start clock on the same line.
		const start = /^\S+ (\d{2}):(\d{2}):\d{2} /.exec( line );
		const startMinutes = start ? Number( start[1] ) * 60 + Number( start[2] ) : null;
		const expiryMinutes = Number( expires[1] ) * 60 + Number( expires[2] );
		const nowMinutes = new Date().getHours() * 60 + new Date().getMinutes();
		if ( startMinutes !== null && expiryMinutes < startMinutes && nowMinutes >= startMinutes ) {
			at.setDate( at.getDate() + 1 );
		}
		expired = Date.now() > at.getTime();
	}
	return { line, expired, pid: Number.isFinite( pid ) ? pid : null, running: alive( pid ) };
}

/*
================
gitIdentity

Best-effort HEAD and dirty flag of the working directory, for the START line.
================
*/
function gitIdentity() {
	try {
		const head = execFileSync( "git", [ "rev-parse", "--short", "HEAD" ], { encoding: "utf8" } ).trim();
		const dirty = execFileSync( "git", [ "status", "--porcelain", "--untracked-files=no" ], { encoding: "utf8" } )
			.trim();
		return `${head}${dirty ? "+dirty" : ""}`;
	} catch {
		return "no-git";
	}
}

/*
================
readQueue

Live tickets in run order: short jobs first, then arrival order. Tickets of
dead waiters are dropped (and the file rewritten) so a crash cannot block.
================
*/
function readQueue() {
	let lines = [];
	try {
		lines = readFileSync( QUEUE, "utf8" ).split( "\n" ).filter( Boolean );
	} catch {
		return [];
	}
	const tickets = lines.map( line => {
		const [token, owner, pid, estimate, at] = line.split( " " );
		return { line, token, owner, pid: Number( pid ), estimate: Number( estimate ), at: Number( at ) };
	} ).filter( ticket => ticket.token && Number.isFinite( ticket.at ) );
	const live = tickets.filter( ticket => alive( ticket.pid ) );
	if ( live.length !== tickets.length ) writeQueue( live );
	const now = Date.now();
	const long = ticket => ticket.estimate > SHORT_JOB_SECONDS && now - ticket.at < STARVATION_MS;
	return live.sort( ( a, b ) => Number( long( a ) ) - Number( long( b ) ) || a.at - b.at );
}

/*
================
writeQueue

Replace the queue through a rename so a reader never sees a partial file.
================
*/
function writeQueue( tickets ) {
	const temporary = QUEUE + "." + process.pid + ".tmp";
	writeFileSync( temporary, tickets.map( ticket => ticket.line + "\n" ).join( "" ) );
	renameSync( temporary, QUEUE );
}

/*
================
leaveQueue
================
*/
function leaveQueue( token ) {
	const tickets = readQueue();
	if ( tickets.some( ticket => ticket.token === token ) ) {
		writeQueue( tickets.filter( ticket => ticket.token !== token ) );
	}
}

/*
================
waitTurn

Join the queue, then try the lock only while this ticket is first. Another
rewriter can drop a line in a race, so the ticket is re-added if missing.
================
*/
async function waitTurn( options, token, stamp ) {
	const ticket = `${token} ${options.owner} ${process.pid} ${options.estimate} ${Date.now()} ${options.purpose}`;
	appendFileSync( QUEUE, ticket + "\n" );
	const leave = () => leaveQueue( token );
	process.on( "exit", leave );
	const deadline = Date.now() + MAX_WAIT_MINUTES * 60000;
	let announced = false;
	while ( Date.now() < deadline ) {
		const tickets = readQueue();
		if ( !tickets.some( entry => entry.token === token ) ) appendFileSync( QUEUE, ticket + "\n" );
		else if ( tickets[0].token === token ) {
			try {
				writeFileSync( LOCK, stamp() + "\n", { flag: "wx" } );
				leave();
				process.off( "exit", leave );
				return true;
			} catch ( error ) {
				if ( error.code !== "EEXIST" ) throw error;
			}
		}
		if ( !announced ) {
			console.error( `[locked-run] queued (${tickets.length} ticket(s)); waiting for the lock` );
			announced = true;
		}
		await new Promise( resolve => setTimeout( resolve, QUEUE_POLL_MS ) );
	}
	leave();
	return false;
}

/*
================
acquire

Exclusive create. A stale lock (expired and its pid gone) is moved aside
only with --break-stale; a live or unexpired one is never touched.
================
*/
function acquire( options, line ) {
	try {
		writeFileSync( LOCK, line + "\n", { flag: "wx" } );
		return true;
	} catch ( error ) {
		if ( error.code !== "EEXIST" ) throw error;
	}
	const holder = describeHolder();
	console.error( `[locked-run] lock held: ${holder?.line ?? "(unreadable)"}` );
	if ( !holder ) return false;
	const stale = holder.expired && holder.pid !== null && !holder.running;
	console.error( `[locked-run] expired=${holder.expired} pid=${holder.pid ?? "none"} running=${holder.running}` );
	if ( !stale || !options.breakStale ) {
		if ( stale ) console.error( "[locked-run] stale lock: rerun with --break-stale to move it aside" );
		return false;
	}
	const aside = LOCK.replace( /\.lock$/, "" ) + `-stale-${Date.now()}.lock`;
	renameSync( LOCK, aside );
	journal( options.owner, `moved a stale lock aside to ${aside}: ${holder.line}` );
	try {
		writeFileSync( LOCK, line + "\n", { flag: "wx" } );
		return true;
	} catch ( error ) {
		if ( error.code === "EEXIST" ) return false;
		throw error;
	}
}

/*
================
release

Only the run whose token is still in the lock may release it; the file is
kept as released-*.lock so the history stays inspectable.
================
*/
function release( owner, line ) {
	let current;
	try {
		current = readFileSync( LOCK, "utf8" ).trim();
	} catch {
		return "lock already gone";
	}
	if ( current !== line ) return "lock now names another holder; left in place";
	const stamp = new Date().toISOString().replace( /[-:T]/g, "" ).slice( 0, 14 );
	renameSync( LOCK, LOCK.replace( /benchmark\.lock$/, `released-${owner.toLowerCase()}-${stamp}.lock` ) );
	return "released";
}

/*
================
main
================
*/
async function main() {
	const options = parseArgs( process.argv.slice( 2 ) );
	if ( !options || !DIRECTORY ) {
		console.error(
			"usage: SRO_COORDINATION_DIR=<shared dir> node locked-run.mjs --owner NAME --purpose TEXT --minutes N " +
				"[--shell] [--break-stale] [--wait --estimate S] [--preflight CMD] -- command [args...]"
		);
		process.exit( EXIT_USAGE );
	}
	// Readiness runs before queueing, so a broken setup never spends anyone's
	// lock time.
	if ( options.preflight ) {
		try {
			execFileSync( options.preflight, { stdio: "inherit", shell: true, timeout: PREFLIGHT_TIMEOUT_MS } );
		} catch {
			console.error( "[locked-run] preflight failed, not queued: " + options.preflight );
			process.exit( EXIT_PREFLIGHT );
		}
	}
	const token = randomBytes( 6 ).toString( "hex" );
	// Times are taken when the lock is actually acquired: a queued wait must
	// not shorten the claimed expiry or count towards the run's duration.
	let started = new Date(), expires = "", line = "";
	const stamp = () => {
		started = new Date();
		expires = clock( new Date( started.getTime() + options.minutes * 60000 ) ).slice( 0, 5 );
		line = `${options.owner} ${
			clock( started )
		} ${options.purpose} expires ${expires} token=${token} pid=${process.pid}`;
		return line;
	};
	if ( options.wait ) {
		if ( !await waitTurn( options, token, stamp ) ) process.exit( EXIT_HELD );
	} else {
		const ahead = readQueue();
		if ( ahead.length ) {
			console.error(
				`[locked-run] ${ahead.length} run(s) queued (first: ${ahead[0].owner}); use --wait to join`
			);
			process.exit( EXIT_HELD );
		}
		if ( !acquire( options, stamp() ) ) process.exit( EXIT_HELD );
	}
	journal(
		options.owner,
		`START ${options.purpose} (locked-run token ${token}, lock until ${expires}, cwd ${process.cwd()} @ ${gitIdentity()})`
	);
	const finish = ( code, how ) => {
		const seconds = Math.round( (Date.now() - started.getTime()) / 1000 );
		const released = release( options.owner, line );
		journal(
			options.owner,
			`END ${options.purpose} (locked-run token ${token}, ${how}, ${seconds}s of estimate ${options.estimate}s, ${released})`
		);
		process.exit( code );
	};
	// spawn can throw synchronously (a bad cwd or command shape) after the
	// lock is taken; release it through the same END path, never leave it.
	let child;
	try {
		child = spawn( options.command, options.args, { stdio: "inherit", shell: options.shell } );
	} catch ( error ) {
		finish( 1, `failed to start: ${error.message}` );
		return;
	}
	const forward = signal => child.kill( signal );
	process.on( "SIGINT", forward );
	process.on( "SIGTERM", forward );
	child.on( "error", error => finish( 1, `failed to start: ${error.message}` ) );
	child.on( "exit", ( code, signal ) => finish( code ?? 1, signal ? `signal ${signal}` : `exit ${code}` ) );
}

main();
