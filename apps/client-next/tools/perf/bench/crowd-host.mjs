/*
===========================================================================

crowd-host.mjs - holds one admitted crowd for many benches

Usage:
  node tools/perf/bench/crowd-host.mjs --fixture FILE.json [--count 32]
       [--port 8796] [--provisioning http://127.0.0.1:8789] [--token FILE]
       [--journal FILE]

Admitting 32 authenticated peers is bounded by the Agent's login budget
(burst 10, one per 6 s), minutes per bench. The host pays it once per
server boot and keeps the peers in world; benches attach with obtainCrowd
(core/crowd.mjs) in no time. It serves GET /crowd (key and live peer
evidence) and POST /shutdown on loopback only. When any peer drops (server
restart, kick) it disables its accounts and exits: a broken crowd is never
offered to a bench.

===========================================================================
*/
import path from "node:path";
import { createServer } from "node:http";
import { mkdir, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parseOptions } from "../core/report.mjs";
import { createCrowd, crowdKey } from "../core/crowd.mjs";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../../.." );
const USAGE =
	"crowd-host.mjs --fixture FILE.json [--count 32] [--port 8796] [--provisioning URL] [--token FILE] [--journal FILE]";
const HEALTH_MS = 1000;

const options = parseOptions( process.argv.slice( 2 ), {
	fixture: "",
	count: 32,
	port: 8796,
	provisioning: "http://127.0.0.1:8789",
	token: path.join( REPO_ROOT, "apps/server/.state/cluster/agent-provisioning-token" ),
	journal: path.join( REPO_ROOT, ".state/crowd-host/crowd-cleanup.json" )
}, USAGE );
if ( !options.fixture ) throw Error( `usage: ${USAGE}` );
const fixture = JSON.parse( await readFile( options.fixture, "utf8" ) );
const key = crowdKey( options.count, fixture );

await mkdir( path.dirname( options.journal ), { recursive: true } );
const started = performance.now();
const crowd = await createCrowd( {
	count: options.count,
	fixture,
	provisioningUrl: options.provisioning,
	tokenPath: options.token,
	journalPath: options.journal
} );
console.log(
	`[crowd-host] ${options.count} peers admitted in ${((performance.now() - started) / 1000).toFixed( 1 )} s`
);

let stopping = false;
/*
================
shutdown

Closes the sockets and disables every account the crowd created, once.
================
*/
async function shutdown( reason ) {
	if ( stopping ) return;
	stopping = true;
	console.log( `[crowd-host] shutting down: ${reason}` );
	server.close();
	try {
		await crowd.close();
	} finally {
		process.exit( reason === "requested" || reason === "SIGINT" || reason === "SIGTERM" ? 0 : 1 );
	}
}

const server = createServer( ( request, response ) => {
	if ( request.method === "GET" && request.url === "/crowd" ) {
		response.setHeader( "content-type", "application/json" );
		response.end( JSON.stringify( { key, peers: crowd.peers } ) );
	} else if ( request.method === "POST" && request.url === "/shutdown" ) {
		response.end( "stopping\n" );
		shutdown( "requested" );
	} else {
		response.statusCode = 404;
		response.end();
	}
} );
server.listen(
	options.port,
	"127.0.0.1",
	() => console.log( `[crowd-host] holding ${key} at http://127.0.0.1:${options.port}` )
);
for ( const signal of [ "SIGINT", "SIGTERM" ] ) process.on( signal, () => shutdown( signal ) );
const health = setInterval( () => {
	if ( crowd.peers.some( peer => peer.closed || !peer.ready ) ) {
		clearInterval( health );
		shutdown( "a peer dropped" );
	}
}, HEALTH_MS );
