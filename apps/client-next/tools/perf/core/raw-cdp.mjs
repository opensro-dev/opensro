/*
===========================================================================

raw-cdp.mjs - system Chrome over raw CDP, with the Network domain refused

Memory measurements cannot go through Playwright. Playwright enables the
DevTools Network domain on every page and worker it attaches to, and with
Network enabled the renderer keeps a copy of each response body for
inspection (partition_alloc's buffer partition, up to roughly 250 MB): a
probe measured that copy as if the client held it. Disabling Network from
a second session does not help, because Playwright's own session keeps it.

So memory runs launch Chrome themselves and speak CDP over its WebSocket.
The connection refuses every Network.* method, so no memory run can turn
inspection on by accident, and memoryDump reads Chrome's own detailed
memory-infra dump per process.

===========================================================================
*/
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// The DevTools domain whose body buffers a memory run must never enable.
export const REFUSED_DOMAIN = "Network.";
const PORT_WAIT_MS = 15000;
const PORT_POLL_MS = 100;
const DUMP_SETTLE_MS = 300;
// A detailed dump of a loaded tab takes a few seconds; past this the run
// fails instead of waiting forever for a tracingComplete that never comes.
const DUMP_DEADLINE_MS = 60000;
// Chrome's child processes can hold profile files briefly after exit.
const PROFILE_REMOVE_RETRIES = 10;
const PROFILE_REMOVE_DELAY_MS = 200;
const MIB = 1024 * 1024;

// The probe launcher's headless GPU switches (scripts/lib/probeBrowser.mjs).
const CHROME_ARGS = [ "--no-sandbox", "--enable-unsafe-swiftshader", "--use-angle=default", "--no-first-run" ];

// System Chrome's usual install paths; SRO_CHROME_PATH overrides them.
const CHROME_PATHS = {
	win32: [
		`${process.env.PROGRAMFILES ?? "C:/Program Files"}/Google/Chrome/Application/chrome.exe`,
		`${process.env["PROGRAMFILES(X86)"] ?? "C:/Program Files (x86)"}/Google/Chrome/Application/chrome.exe`,
		`${process.env.LOCALAPPDATA ?? ""}/Google/Chrome/Application/chrome.exe`
	],
	darwin: [ "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" ],
	linux: [ "/usr/bin/google-chrome", "/usr/bin/google-chrome-stable" ]
};

// The allocators a memory report shows, in order.
export const REPORTED_ALLOCATORS = [
	"partition_alloc/partitions/array_buffer",
	"partition_alloc/partitions/buffer",
	"v8",
	"malloc",
	"blink_gc",
	"gpu",
	"cc",
	"skia",
	"shared_memory"
];

/*
================
chromePath
================
*/
export function chromePath() {
	const candidates = process.env.SRO_CHROME_PATH ?
		[ process.env.SRO_CHROME_PATH ] :
		CHROME_PATHS[process.platform] ?? [];
	const found = candidates.find( path => path && existsSync( path ) );
	if ( !found ) throw Error( `no system Chrome found; set SRO_CHROME_PATH (looked in ${candidates.join( ", " )})` );
	return found;
}

/*
================
createCdpConnection

One CDP connection over socket (a WebSocket or anything with send, close,
onmessage and onclose). send( method, params, sessionId ) resolves the
result or rejects the protocol error; on( listener ) receives every event.
Once the socket closes, every pending and later send rejects, so a run
whose browser died fails instead of hanging.
================
*/
export function createCdpConnection( socket ) {
	let next = 1, closed = false;
	const waiting = new Map(), listeners = new Set();
	socket.onclose = () => {
		closed = true;
		for ( const { reject } of waiting.values() ) reject( Error( "the CDP connection closed" ) );
		waiting.clear();
	};
	socket.onmessage = event => {
		const message = JSON.parse( typeof event === "string" ? event : event.data );
		if ( message.id !== undefined && waiting.has( message.id ) ) {
			const { resolve, reject } = waiting.get( message.id );
			waiting.delete( message.id );
			if ( message.error ) reject( Error( `${message.error.message} (${message.error.code})` ) );
			else resolve( message.result ?? {} );
			return;
		}
		for ( const listener of listeners ) listener( message );
	};
	return {
		/** @param {string} method @param {object} [params] @param {string} [sessionId] */
		send( method, params = {}, sessionId ) {
			if ( method.startsWith( REFUSED_DOMAIN ) ) {
				return Promise.reject(
					Error( `${method} refused: a memory run never enables DevTools network inspection` )
				);
			}
			if ( closed ) return Promise.reject( Error( `${method}: the CDP connection is closed` ) );
			return new Promise( ( resolve, reject ) => {
				const id = next++;
				waiting.set( id, { resolve, reject } );
				socket.send( JSON.stringify( { id, method, params, ...(sessionId ? { sessionId } : {}) } ) );
			} );
		},
		on( listener ) {
			listeners.add( listener );
			return () => listeners.delete( listener );
		},
		close: () => socket.close()
	};
}

/*
================
removeProfile
================
*/
function removeProfile( profile ) {
	rmSync( profile, {
		recursive: true,
		force: true,
		maxRetries: PROFILE_REMOVE_RETRIES,
		retryDelay: PROFILE_REMOVE_DELAY_MS
	} );
}

/*
================
stopChrome

Kills Chrome and waits for it to exit, unless it already has: a crashed
or timed-out Chrome never emits another "exit".
================
*/
export async function stopChrome( chrome ) {
	if ( chrome.exitCode !== null || chrome.signalCode !== null ) return;
	const exited = new Promise( resolve => chrome.once( "exit", resolve ) );
	chrome.kill();
	await exited;
}

/*
================
launchRawChrome

System Chrome with an isolated profile and an ephemeral debugging port,
connected over its browser WebSocket. close() ends Chrome and removes the
profile. A launch that fails on the way does the same before it throws.
================
*/
export async function launchRawChrome( { headed = false, extraArgs = [] } = {} ) {
	const profile = mkdtempSync( join( tmpdir(), "sro-memory-" ) );
	const chrome = spawn( chromePath(), [
		...(headed ? [] : [ "--headless=new" ]),
		`--user-data-dir=${profile}`,
		"--remote-debugging-port=0",
		...CHROME_ARGS,
		...extraArgs,
		"about:blank"
	], { stdio: "ignore" } );
	const abandon = async error => {
		await stopChrome( chrome );
		removeProfile( profile );
		throw error;
	};
	const portFile = join( profile, "DevToolsActivePort" );
	const deadline = Date.now() + PORT_WAIT_MS;
	while ( !existsSync( portFile ) || !readFileSync( portFile, "utf8" ).includes( "\n" ) ) {
		if ( Date.now() > deadline ) return abandon( Error( "Chrome did not open its debugging port" ) );
		await new Promise( resolve => setTimeout( resolve, PORT_POLL_MS ) );
	}
	const [port, path] = readFileSync( portFile, "utf8" ).split( "\n" );
	const socket = new WebSocket( `ws://127.0.0.1:${port}${path}` );
	try {
		await new Promise( ( resolve, reject ) => {
			socket.onopen = resolve;
			socket.onerror = () => reject( Error( "Chrome's debugging WebSocket did not open" ) );
		} );
	} catch ( error ) {
		return abandon( error );
	}
	const cdp = createCdpConnection( socket );
	return {
		cdp,
		async close() {
			cdp.close();
			await stopChrome( chrome );
			removeProfile( profile );
		}
	};
}

/*
================
attachPage

The browser's first page target, attached flat: its sessionId addresses
page-level methods through the browser connection.
================
*/
export async function attachPage( cdp ) {
	const { targetInfos } = await cdp.send( "Target.getTargets" );
	const page = targetInfos.find( target => target.type === "page" );
	if ( !page ) throw Error( "Chrome has no page target" );
	const { sessionId } = await cdp.send( "Target.attachToTarget", { targetId: page.targetId, flatten: true } );
	return sessionId;
}

/*
================
parseMemoryDump

Chrome's detailed memory-infra events to one row per process: its private
footprint and the reported allocators' sizes, in MiB.
================
*/
export function parseMemoryDump( events, processTypes = new Map() ) {
	const hex = value => typeof value === "string" ? parseInt( value, 16 ) : Number( value ?? 0 );
	const rows = new Map();
	for ( const event of events ) {
		const dumps = event.ph === "v" ? event.args?.dumps : undefined;
		if ( !dumps ) continue;
		const row = rows.get( event.pid ) ??
			{ pid: event.pid, type: processTypes.get( event.pid ) ?? "?", footprint: 0, allocators: {} };
		const footprint = hex( dumps.process_totals?.private_footprint_bytes );
		if ( footprint ) row.footprint = footprint / MIB;
		for ( const name of REPORTED_ALLOCATORS ) {
			const size = hex( dumps.allocators?.[name]?.attrs?.size?.value );
			if ( size ) row.allocators[name] = size / MIB;
		}
		rows.set( event.pid, row );
	}
	return [ ...rows.values() ].filter( row => row.footprint > 0 || Object.keys( row.allocators ).length );
}

/*
================
memoryDump

One deterministic detailed dump of every Chrome process. Rejects when
Chrome has not finished the trace within deadlineMs.
================
*/
export async function memoryDump( cdp, deadlineMs = DUMP_DEADLINE_MS ) {
	const events = [];
	let complete, timer;
	const done = new Promise( resolve => complete = resolve );
	const late = new Promise( ( _, reject ) => {
		timer = setTimeout(
			() => reject( Error( `Chrome did not finish the memory dump within ${deadlineMs} ms` ) ),
			deadlineMs
		);
	} );
	const stop = cdp.on( message => {
		if ( message.method === "Tracing.dataCollected" ) events.push( ...message.params.value );
		if ( message.method === "Tracing.tracingComplete" ) complete();
	} );
	/*
	================
	trace
	================
	*/
	const trace = async () => {
		await cdp.send( "Tracing.start", {
			transferMode: "ReportEvents",
			traceConfig: {
				includedCategories: [ "disabled-by-default-memory-infra" ],
				excludedCategories: [ "*" ],
				memoryDumpConfig: { triggers: [] }
			}
		} );
		await new Promise( resolve => setTimeout( resolve, DUMP_SETTLE_MS ) );
		const dump = await cdp.send( "Tracing.requestMemoryDump", { deterministic: true, levelOfDetail: "detailed" } );
		if ( !dump.success ) throw Error( "Chrome refused the memory dump" );
		await new Promise( resolve => setTimeout( resolve, DUMP_SETTLE_MS ) );
		await cdp.send( "Tracing.end" );
		await done;
	};
	try {
		await Promise.race( [ trace(), late ] );
	} finally {
		clearTimeout( timer );
		stop();
	}
	const { processInfo } = await cdp.send( "SystemInfo.getProcessInfo" );
	return parseMemoryDump( events, new Map( processInfo.map( process => [ process.id, process.type ] ) ) );
}
