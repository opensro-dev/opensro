/*
===========================================================================

crowd-host.mjs - holds one admitted crowd for many benches

Usage:
  node tools/perf/bench/crowd-host.mjs --fixture FILE.json [--count 32]
       [--port 8796] [--provisioning http://127.0.0.1:8789] [--token FILE]
       [--journal FILE] [--act walk=0.5,chat=0.2] [--seed 1]
  node tools/perf/bench/crowd-host.mjs --accounts FILE --count 600 [--port 8796]
       [--act walk=0.5,chat=0.2] [--seed 1]

Admitting 32 authenticated peers is bounded by the Agent's login budget
(burst 10, one per 6 s), minutes per bench. The host pays it once per
server boot and keeps the peers in world; benches attach with obtainCrowd
(core/crowd.mjs) in no time. It serves GET /crowd (key, live peer
evidence, activity, source) and POST /shutdown on loopback only. When any
peer drops (server restart, kick) it exits: a broken crowd is never
offered to a bench.

With --fixture it admits a fresh crowd of at most 32 and disables those
accounts on shutdown. With --accounts it holds the first --count
characters of a file crowd-provision.mjs made, on their saved sessions
(core/crowd-accounts.mjs), at the file's fixture; on shutdown it closes
the sockets and keeps the accounts for the next hold.

--act gives a share of the peers scripted input from --seed
(core/crowd-activity.mjs): walkers click near their spot, chatters speak
in All chat, the rest stand. The activity is part of the crowd's key, and
GET /crowd carries its evidence, which a bench copies into its rows.

===========================================================================
*/
import path from "node:path";
import { createServer } from "node:http";
import { mkdir, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parseOptions } from "../core/report.mjs";
import { createCrowd, crowdKey, DEFAULT_ACTIVITY_SEED } from "../core/crowd.mjs";
import { createAccountsSaver, holdCrowd, loadAccountsFile } from "../core/crowd-accounts.mjs";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../../.." );
const USAGE = "crowd-host.mjs --fixture FILE.json | --accounts FILE [--count 32] [--port 8796] [--provisioning URL] " +
	"[--token FILE] [--journal FILE] [--act walk=SHARE,chat=SHARE] [--seed N]";
const HEALTH_MS = 1000;

const options = parseOptions( process.argv.slice( 2 ), {
	fixture: "",
	accounts: "",
	count: 32,
	port: 8796,
	provisioning: "http://127.0.0.1:8789",
	token: path.join( REPO_ROOT, "apps/server/.state/cluster/agent-provisioning-token" ),
	journal: path.join( REPO_ROOT, ".state/crowd-host/crowd-cleanup.json" ),
	act: "",
	seed: DEFAULT_ACTIVITY_SEED
}, USAGE );
if ( !options.fixture === !options.accounts ) throw Error( `name one of --fixture and --accounts\nusage: ${USAGE}` );
if ( !Number.isInteger( options.seed ) || options.seed < 0 ) {
	throw Error( `--seed must be a whole number\nusage: ${USAGE}` );
}
const file = options.accounts ? await loadAccountsFile( options.accounts ) : null;
const fixture = file ? file.fixture : JSON.parse( await readFile( options.fixture, "utf8" ) );
const key = crowdKey( options.count, fixture, options.act, options.seed );

const started = performance.now();
let crowd;
if ( file ) {
	crowd = await holdCrowd( {
		file,
		count: options.count,
		save: createAccountsSaver( options.accounts ),
		act: options.act,
		seed: options.seed
	} );
	console.log(
		`[crowd-host] ${options.count} peers held in ${((performance.now() - started) / 1000).toFixed( 1 )} s: ` +
			`${crowd.source.reused} saved sessions, ${crowd.source.logins} logins`
	);
} else {
	await mkdir( path.dirname( options.journal ), { recursive: true } );
	crowd = await createCrowd( {
		count: options.count,
		fixture,
		provisioningUrl: options.provisioning,
		tokenPath: options.token,
		journalPath: options.journal,
		act: options.act,
		seed: options.seed
	} );
	crowd.source = { kind: "fresh", accounts: options.count };
	console.log(
		`[crowd-host] ${options.count} peers admitted in ${((performance.now() - started) / 1000).toFixed( 1 )} s`
	);
}

let stopping = false;
/*
================
shutdown

Closes the sockets once: a fresh crowd also disables its accounts, a
held one saves its file and keeps them.
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
		response.end( JSON.stringify( { key, peers: crowd.peers, activity: crowd.activity, source: crowd.source } ) );
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
