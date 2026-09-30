/*
===========================================================================

headless-simulation.mjs - run a release's own simulation worker in Node

The simulation worker (session, network, world) is one self-contained module
that talks to its page only through HostMessage/WorkerMessage. Node runs the
shipped bytes in a worker_threads thread; this owner plays the page's part:
it bridges postMessage/onmessage, recycles snapshot buffers, acknowledges
world batches, and keeps session state and entities. Rendering, audio and
the asset worker are absent by design: the release gate needs no GPU.

The thread gets what a browser gives a same-origin worker and nothing more:
an Origin header on same-origin requests and WebSocket upgrades, and a
cookie jar that stores cookies from credentialed responses and sends them on
credentialed requests. The jar outlives a worker, as a page reload keeps it.

The owner also keeps a timeline of the session phase changes, the requests
and the first messages on each socket, so a report shows where world entry
spends its time.

===========================================================================
*/
import { MessageChannel, Worker } from "node:worker_threads";

// HostMessage "start" version (contracts/simulation.ts PROTOCOL_VERSION).
export const SIMULATION_PROTOCOL = 3;
const POLL_MS = 50;
// Socket messages timed per connection: the entry burst, not the game tick.
const TRACED_SOCKET_MESSAGES = 12;

// The worker thread's side: installed before the release's module loads.
const BOOTSTRAP = `
import { parentPort, workerData } from "node:worker_threads";
const { origin, workerUrl, jarPort, tracedSocketMessages } = workerData;
function trace( event ) {
	parentPort.postMessage( { kind: "__trace", event, at: Date.now() } );
}
const jar = new Map( workerData.cookies );
function cookieHeader() {
	return [ ...jar ].map( ( [ name, value ] ) => name + "=" + value ).join( "; " );
}
function store( setCookies ) {
	for ( const line of setCookies ) {
		const [ pair, ...attributes ] = line.split( ";" );
		const at = pair.indexOf( "=" );
		if ( at <= 0 ) continue;
		const name = pair.slice( 0, at ).trim(), value = pair.slice( at + 1 ).trim();
		const expired = attributes.some( a => /^\\s*max-age\\s*=\\s*(-\\d+|0)\\s*$/i.test( a ) ) ||
			attributes.some( a => /^\\s*expires\\s*=/i.test( a ) && Date.parse( a.split( "=" ).slice( 1 ).join( "=" ) ) < Date.now() );
		if ( expired ) jar.delete( name );
		else jar.set( name, value );
	}
	jarPort.postMessage( [ ...jar ] );
}
const nativeFetch = globalThis.fetch;
globalThis.fetch = async ( input, init = {} ) => {
	const url = new URL( typeof input === "string" || input instanceof URL ? input : input.url, origin );
	const headers = new Headers( init.headers ?? ( typeof input === "object" && "headers" in input ? input.headers : undefined ) );
	const sameOrigin = url.origin === origin, credentialed = sameOrigin && init.credentials !== "omit";
	if ( sameOrigin ) headers.set( "Origin", origin );
	if ( credentialed && jar.size ) headers.set( "Cookie", cookieHeader() );
	const response = await nativeFetch( url, { ...init, headers } );
	trace( "fetch " + url.pathname + " " + response.status + " " + ( response.headers.get( "content-length" ) ?? "?" ) + "B" );
	if ( credentialed ) {
		const set = response.headers.getSetCookie();
		if ( set.length ) store( set );
	}
	return response;
};
const NativeWebSocket = globalThis.WebSocket;
globalThis.WebSocket = class extends NativeWebSocket {
	constructor( url, protocols ) {
		const target = new URL( url, origin.replace( /^http/, "ws" ) );
		const headers = { Origin: origin };
		if ( jar.size ) headers.Cookie = cookieHeader();
		super( target, { protocols, headers } );
		let messages = 0;
		this.addEventListener( "open", () => trace( "socket open" ) );
		this.addEventListener( "message", event => {
			if ( messages++ >= tracedSocketMessages ) return;
			const data = event.data;
			trace( "socket message " + ( data.byteLength ?? data.size ?? data.length ) + "B" );
		} );
	}
};
globalThis.postMessage = ( message, transfer ) => parentPort.postMessage( message, transfer );
await import( workerUrl );
parentPort.on( "message", data => globalThis.onmessage?.( { data } ) );
parentPort.postMessage( { kind: "__ready" } );
`;

/*
================
startHeadlessSimulation

Start one simulation worker from the release's worker file (a file: URL)
against origin, with the given cookies. Resolves once the release's module
has loaded and received "start".
================
*/
/**
 * @param {{ workerUrl: string, origin: string, bootstrapUrl: URL, cookies?: [string, string][] }} options
 */
export async function startHeadlessSimulation( { workerUrl, origin, cookies = [], bootstrapUrl } ) {
	const startedAt = Date.now();
	const jarChannel = new MessageChannel();
	let jar = cookies;
	jarChannel.port1.on( "message", value => {
		jar = value;
	} );
	jarChannel.port1.unref();
	const worker = new Worker( bootstrapUrl, {
		workerData: {
			origin,
			workerUrl,
			cookies,
			jarPort: jarChannel.port2,
			tracedSocketMessages: TRACED_SOCKET_MESSAGES
		},
		transferList: [ jarChannel.port2 ]
	} );
	/** @type {{ session: any, sessions: any[], timeline: { event: string, atMs: number }[], failures: string[], entities: Map<number, any>, gameplay: any, travelRevision: number, readyRequested: boolean, batches: number, events: number, exited: boolean }} */
	const state = {
		session: null,
		sessions: [],
		// Phase changes, requests and socket messages, in ms since the start.
		timeline: [],
		failures: [],
		entities: new Map(),
		gameplay: null,
		travelRevision: 0,
		readyRequested: false,
		batches: 0,
		events: 0,
		exited: false
	};
	/** @type {(value?: unknown) => void} */
	let ready = () => {};
	const started = new Promise( ( resolve, reject ) => {
		ready = resolve;
		worker.once( "error", reject );
	} );
	worker.on( "error", error => state.failures.push( String( error ) ) );
	worker.on( "exit", () => {
		state.exited = true;
	} );
	worker.on( "message", message => {
		if ( message.kind === "__ready" ) {
			ready();
			return;
		}
		if ( message.kind === "__trace" ) {
			state.timeline.push( { event: message.event, atMs: message.at - startedAt } );
			return;
		}
		if ( message.kind === "snapshot" ) {
			worker.postMessage( { kind: "recycle", buffer: message.buffer }, [ message.buffer ] );
			return;
		}
		if ( message.kind === "session" ) {
			if ( message.state.phase !== state.session?.phase ) {
				state.timeline.push( { event: "session " + message.state.phase, atMs: Date.now() - startedAt } );
			}
			state.session = message.state;
			state.sessions.push( message.state );
			return;
		}
		if ( message.kind === "failure" ) {
			state.failures.push( message.message );
			return;
		}
		if ( message.kind === "world" ) {
			state.batches++;
			for ( const event of message.batch.events ) {
				state.events++;
				if ( event.kind === "gameplay" ) state.gameplay = event.state;
				// The page reports readiness for the travel it presented; a newer
				// travel needs a new report (world.ts ignores a stale revision).
				if ( event.kind === "travel" && event.travel.revision !== state.travelRevision ) {
					state.travelRevision = event.travel.revision;
					if ( state.readyRequested ) {
						worker.postMessage( {
							kind: "session",
							command: { kind: "world-ready", travelRevision: state.travelRevision }
						} );
					}
				}
				if ( event.kind === "spawn" || event.kind === "state" ) {
					state.entities.set( event.entity.gid, event.entity );
				} else if ( event.kind === "despawn" ) state.entities.delete( event.gid );
				else if ( event.kind === "reset" ) state.entities.clear();
			}
			worker.postMessage( { kind: "world-ack", sequence: message.batch.sequence } );
		}
	} );
	await started;
	worker.postMessage( { kind: "start", version: SIMULATION_PROTOCOL } );
	return {
		state,
		session( command ) {
			worker.postMessage( { kind: "session", command } );
		},
		ready() {
			state.readyRequested = true;
			worker.postMessage( {
				kind: "session",
				command: { kind: "world-ready", travelRevision: state.travelRevision }
			} );
		},
		entityNamed( name ) {
			for ( const entity of state.entities.values() ) if ( entity.name === name ) return entity;
			return null;
		},
		cookies: () => jar,
		async waitFor( predicate, timeoutMs, label ) {
			const deadline = Date.now() + timeoutMs;
			while ( Date.now() < deadline ) {
				if ( state.failures.length ) {
					throw Error( `${label}: simulation failed: ${state.failures.join( "; " )}` );
				}
				if ( state.session?.phase === "failed" ) {
					throw Error(
						`${label}: session failed: ${state.session.error ?? state.session.code ?? "unknown"}`
					);
				}
				const value = predicate( state );
				if ( value ) return value;
				await new Promise( resolve => setTimeout( resolve, POLL_MS ) );
			}
			throw Error( `${label}: timed out after ${timeoutMs} ms (phase ${state.session?.phase ?? "none"})` );
		},
		async stop() {
			worker.postMessage( { kind: "stop" } );
			await worker.terminate();
		}
	};
}

/*
================
bootstrapSource

The worker-thread preamble, written beside the release's worker file.
================
*/
export function bootstrapSource() {
	return BOOTSTRAP;
}

/*
================
simulationWorkerPath

The simulation worker's URL path in a built application script: the page
constructs it as new Worker(new URL(path, ...), { ..., name: "sro-simulation" }).
================
*/
export function simulationWorkerPath( script ) {
	const pattern = /new Worker\(\s*new URL\(\s*"([^"]+\.js)"[^)]*\)\s*,\s*\{[^}]*name\s*:\s*"sro-simulation"[^}]*\}/g;
	const found = [ ...script.matchAll( pattern ) ].map( match => match[1] );
	if ( found.length !== 1 ) throw Error( `Expected one simulation worker in the application, found ${found.length}` );
	return found[0];
}
