/*
===========================================================================

smoke.mjs - `pnpm local` end to end, without a browser

Starts scripts/local/run.mjs exactly as `pnpm local` runs it, waits for
the ready line, logs the development account in over the wire (the
Agent's POST /title/login the browser client uses), checks the
GameWorld's readiness endpoint, then stops the run by closing the
runner's stdin and asserts every port is free again - nothing
orphaned. Run it locally before merging a change to the local stack:

    node scripts/local/smoke.mjs

It drives the real stack, so the game data must be built and the local
ports free, exactly as `pnpm local --doctor` checks. The world it
provisions is a scratch one (SRO_LOCAL_STATE_ROOT, removed afterwards):
the shared local world is a person's, and a check never writes into it.

===========================================================================
*/

import { spawn } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const configDir = path.join( repoRoot, "apps", "server", "config" );
const readyTimeoutMs = 420_000;
const stopTimeoutMs = 90_000;

/*
================
setting

One KEY=VALUE out of a config file, or the fallback.
================
*/
function setting( file, key, fallback ) {
	for ( const raw of readFileSync( file, "utf8" ).split( /\r?\n/ ) ) {
		const cut = raw.indexOf( "=" );
		if ( cut > 0 && raw.slice( 0, cut ).trim() === key ) {
			return raw.slice( cut + 1 ).trim();
		}
	}
	return fallback;
}

/*
================
portBusy
================
*/
function portBusy( port ) {
	return new Promise( resolve => {
		const socket = net.connect( port, "127.0.0.1" );
		socket.once( "connect", () => {
			socket.end();
			resolve( true );
		} );
		socket.once( "error", () => resolve( false ) );
	} );
}

/*
================
clientProtocol

The release protocol the browser client ships, read from the client's
own protocol.ts (that file imports nothing so Node tooling reads the
same numbers). The login declares it exactly as the browser does; if
client and server ever drift, this login is refused 426 and the
smoke fails, which is the point.
================
*/
function clientProtocol() {
	const source = readFileSync(
		path.join( repoRoot, "apps", "client-next", "src", "engine", "foundation", "release", "protocol.ts" ),
		"utf8"
	);
	const value = source.match( /export const RELEASE_PROTOCOL = (\d+)/ );
	const header = source.match( /export const RELEASE_PROTOCOL_HEADER = "([^"]+)"/ );
	if ( !value || !header ) {
		throw new Error( "could not read RELEASE_PROTOCOL from the client's protocol.ts" );
	}
	return { header: header[1], value: value[1] };
}

/*
================
main
================
*/
async function main() {
	const agentAddr = setting( path.join( configDir, "local.env" ), "LOCAL_AGENT_ADDR", "127.0.0.1:8787" );
	const shardID = setting( path.join( configDir, "local.env" ), "LOCAL_SHARD_ID", "global-official" );
	const catalog = JSON.parse( readFileSync( path.join( configDir, "shards.json" ), "utf8" ) );
	const shard = catalog.shards.find( row => row.id === shardID );
	const account = setting( path.join( configDir, "dev-account.env" ), "SRO_DEV_ACCOUNT_ID", "" );
	const password = setting( path.join( configDir, "dev-account.env" ), "SRO_DEV_ACCOUNT_PASSWORD", "" );

	const scratchWorld = mkdtempSync( path.join( os.tmpdir(), "sro-local-smoke-" ) );
	process.once( "exit", () => rmSync( scratchWorld, { recursive: true, force: true } ) );
	const runner = spawn( process.execPath, [ path.join( repoRoot, "scripts", "local", "run.mjs" ) ], {
		cwd: repoRoot,
		env: { ...process.env, SRO_LOCAL_STATE_ROOT: scratchWorld },
		stdio: [ "pipe", "pipe", "pipe" ]
	} );
	let output = "";
	for ( const stream of [ runner.stdout, runner.stderr ] ) {
		stream.setEncoding( "utf8" );
		stream.on( "data", chunk => {
			output += chunk;
			process.stdout.write( chunk );
		} );
	}
	const exited = new Promise( resolve => runner.once( "exit", resolve ) );

	console.log( "smoke: waiting for the local game to become ready..." );
	const deadline = Date.now() + readyTimeoutMs;
	while ( !output.includes( "Local game is ready" ) ) {
		if ( runner.exitCode !== null ) {
			throw new Error( `the runner exited (${runner.exitCode}) before becoming ready` );
		}
		if ( Date.now() > deadline ) {
			throw new Error( "the local game did not become ready in time" );
		}
		await new Promise( resolve => setTimeout( resolve, 500 ) );
	}

	console.log( `smoke: logging ${account} in over the wire...` );
	const protocol = clientProtocol();
	const login = await fetch( `http://${agentAddr}/title/login`, {
		method: "POST",
		headers: { "content-type": "application/json", [protocol.header]: protocol.value },
		body: JSON.stringify( { id: account, password, serverId: shardID } ),
		signal: AbortSignal.timeout( 10_000 )
	} );
	const body = await login.json().catch( () => ({}) );
	if ( login.status !== 200 || body.ok !== true || !body.sessionToken ) {
		throw new Error( `login refused: HTTP ${login.status} ${JSON.stringify( body )}` );
	}
	console.log( "smoke: login accepted, session token issued" );

	const ready = await fetch( `${shard.transportUrl}/transport/readyz`, { signal: AbortSignal.timeout( 10_000 ) } );
	if ( ready.status !== 200 ) {
		throw new Error( `GameWorld readiness answered ${ready.status}` );
	}
	console.log( "smoke: GameWorld is ready" );

	console.log( "smoke: stopping the run..." );
	runner.stdin.end();
	const code = await Promise.race( [
		exited,
		new Promise( resolve => setTimeout( () => resolve( "timeout" ), stopTimeoutMs ) )
	] );
	if ( code === "timeout" ) {
		runner.kill( "SIGKILL" );
		throw new Error( "the runner did not stop in time" );
	}
	for (
		const port of [
			Number( new URL( `http://${agentAddr}` ).port ),
			Number( new URL( shard.transportUrl ).port ),
			5180
		]
	) {
		if ( await portBusy( port ) ) {
			throw new Error( `port ${port} is still bound after the stop (orphaned process)` );
		}
	}
	console.log( "smoke: everything stopped, no port left bound" );
	console.log( "SMOKE PASS" );
}

main().catch( error => {
	console.error( `SMOKE FAIL: ${error.message}` );
	process.exit( 1 );
} );
