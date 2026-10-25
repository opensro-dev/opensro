/*
===========================================================================

dev-lag.mjs - the development client behind an artificial network delay

Movement and prediction only show their character over a real link: on
localhost the round trip is about a millisecond, so a mode that hides
latency and one that does not look the same. This starts the usual Vite
development server with every server it reaches - the Agent (/api) and
each shard's GameWorld transport - behind a TCP proxy that holds each
chunk for half the requested round trip in each direction. Nothing in the
client or the servers changes: the proxies sit between this edge and its
upstreams, and Vite reads a copy of the shard catalog that points at them.

	pnpm dev:lag              300 ms round trip on port 5180
	pnpm dev:lag -- --rtt 150 --jitter 20 --port 5190

A second instance on another port (--port) shares the same servers, so an
agent's browser test can run beside a person playing on 5180.

===========================================================================
*/
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

const CLIENT_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../.." );
const REPO_ROOT = path.resolve( CLIENT_ROOT, "../.." );
const DEFAULT_RTT_MS = 300;
const DEFAULT_PORT = 5180;
// Proxy ports are taken from here upward, one per upstream.
const PROXY_PORT_BASE = 18700;

/*
================
options

--rtt, --jitter and --port from the command line.
================
*/
function options( argv ) {
	const read = name => {
		const at = argv.indexOf( "--" + name );
		return at >= 0 ? Number( argv[at + 1] ) : undefined;
	};
	return {
		rttMs: read( "rtt" ) ?? DEFAULT_RTT_MS,
		jitterMs: read( "jitter" ) ?? 0,
		port: read( "port" ) ?? DEFAULT_PORT
	};
}

/*
================
delayedPipe

Copies from into to, each chunk released delayMs (plus up to jitterMs)
after it arrived and never before the chunk ahead of it, so the stream
keeps its order. The source's end, close or error is passed on only once
every chunk it sent has been released: a server that answers and closes
at once must not lose the answer still held for the delay (a browser's
keep-alive request then saw "socket hang up").
================
*/
function delayedPipe( from, to, delayMs, jitterMs ) {
	let releaseAt = 0, pending = 0, closing = false;
	const finish = () => {
		if ( closing && pending === 0 && !to.destroyed ) to.end();
	};
	from.on( "data", chunk => {
		const now = Date.now();
		releaseAt = Math.max( releaseAt, now + delayMs + Math.random() * jitterMs );
		pending++;
		setTimeout( () => {
			pending--;
			if ( !to.destroyed ) to.write( chunk );
			finish();
		}, releaseAt - now );
	} );
	const close = () => {
		closing = true;
		finish();
	};
	from.on( "end", close );
	from.on( "close", close );
	from.on( "error", close );
}

/*
================
delayProxy

A loopback TCP proxy on listenPort to target that adds delayMs each way.
Resolves once it listens.
================
*/
export function delayProxy( listenPort, target, delayMs, jitterMs ) {
	const url = new URL( target );
	return new Promise( ( resolve, reject ) => {
		const server = net.createServer( client => {
			const upstream = net.connect( Number( url.port ), url.hostname );
			client.setNoDelay( true );
			upstream.setNoDelay( true );
			delayedPipe( client, upstream, delayMs, jitterMs );
			delayedPipe( upstream, client, delayMs, jitterMs );
			// Each side ends the other through delayedPipe once its held
			// chunks are out; nothing is destroyed early.
			client.on( "error", () => {} );
			upstream.on( "error", () => {} );
		} );
		server.on( "error", reject );
		server.listen( listenPort, "127.0.0.1", () => resolve( server ) );
	} );
}

/*
================
main
================
*/
async function main() {
	const { rttMs, jitterMs, port } = options( process.argv.slice( 2 ) );
	const delayMs = rttMs / 2;
	const agentTarget = process.env.SRO_AGENT_PROXY_TARGET || "http://127.0.0.1:8787";
	const catalogFile = process.env.SRO_SHARD_CATALOG ||
		path.join( REPO_ROOT, "apps/server/config/shards.json" );
	const catalog = JSON.parse( readFileSync( catalogFile, "utf8" ) );
	// Each instance takes its own block of proxy ports, so two instances
	// (different --port) never collide.
	let next = PROXY_PORT_BASE + (port % 100) * 10;
	const agentPort = next++;
	await delayProxy( agentPort, agentTarget, delayMs, jitterMs );
	for ( const shard of catalog.shards ) {
		if ( !shard.publicTransportUrl?.startsWith( "/" ) ) continue;
		const shardPort = next++;
		await delayProxy( shardPort, shard.transportUrl, delayMs, jitterMs );
		shard.transportUrl = "http://127.0.0.1:" + shardPort;
	}
	// The catalog copy is scratch state: ignored temp/, rewritten on each run.
	const scratch = path.join( REPO_ROOT, "temp", "dev-lag" );
	mkdirSync( scratch, { recursive: true } );
	const lagCatalog = path.join( scratch, `shards-${port}.json` );
	writeFileSync( lagCatalog, JSON.stringify( catalog, null, "\t" ) );
	console.log(
		`dev-lag: +${rttMs} ms round trip (jitter ${jitterMs} ms) on http://localhost:${port}/ ` +
			`(Agent via :${agentPort}, ${next - agentPort - 1} shard transport(s))`
	);
	const vite = spawn(
		process.execPath,
		[
			path.join( path.dirname( createRequire( import.meta.url ).resolve( "vite/package.json" ) ), "bin/vite.js" ),
			"--host",
			"127.0.0.1",
			"--port",
			String( port ),
			"--strictPort"
		],
		{
			cwd: CLIENT_ROOT,
			stdio: "inherit",
			env: {
				...process.env,
				SRO_AGENT_PROXY_TARGET: "http://127.0.0.1:" + agentPort,
				SRO_SHARD_CATALOG: lagCatalog
			}
		}
	);
	vite.on( "exit", code => process.exit( code ?? 0 ) );
}

// Imported by its test for delayProxy alone; run, it starts the server.
if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main().catch( error => {
		console.error( error );
		process.exit( 1 );
	} );
}
