/*
===========================================================================

run.mjs - `pnpm local`: the whole game on localhost, one command

Builds the Agent and one GameWorld, provisions the development world
(the existing sro-bootstrap-development), and runs both plus the Vite
dev server as plain child processes. No Nomad: the deployment contract
the processes actually read is environment plus the bootstrap state
under apps/server/.state, and this runner supplies both. The workload
identity the GameWorld shows the Agent (a Nomad JWT in production) is
minted locally instead: the runner holds a development Ed25519 key
under .state/local, serves its JWKS on loopback, and rewrites the
GameWorld's identity file as the token ages.

Settings live in apps/server/config/local.env. Stopping is graceful
where the OS allows it: the children share this console, so a real
Ctrl+C reaches them directly and each closes its own store; POSIX
stops forward SIGTERM. A programmatic stop (the runner's stdin
closing under a harness, as the smoke test does) has no targeted
console signal on Windows - a CTRL_BREAK broadcast hits every process
on the console, parent shells included - so there it kills each
child's tree instead, GameWorld first. Either way nothing is left
orphaned: whatever is still alive after the grace period is killed.

`pnpm local --doctor` names every missing prerequisite and its fix;
`pnpm local --reset` moves the local world aside (never deletes) and
starts fresh.

Three stops name their own fix instead of dying inside a child (#3081):
the runner upgrades a local authority left behind by a schema bump
(copy under .state/backups first, the offline upgrader second, never
for a schema its own validator refuses); the doctor compares the
assets the client requires at startup against the published pack
manifest, not just the directory's existence; and a stdin close before
ready says it was stdin, not a process that "never became ready".

===========================================================================
*/

import { spawn, spawnSync } from "node:child_process";
import { createPrivateKey, createPublicKey, generateKeyPairSync, sign } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, renameSync, statSync, writeFileSync } from "node:fs";
import http from "node:http";
import { createRequire } from "node:module";
import net from "node:net";
import path from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { authorityDirFor, backupStamp, unpublishedStartupAssets, upgradeLocalAuthority } from "./preboot.mjs";
import { resolveServerGameDataRoot } from "../build/world/paths.mjs";
import { resolveGeneratedRoot, resolveMainCheckout } from "../lib/generatedRoot.mjs";
import { sharedCacheEnvironment } from "../lib/goEnvironment.mjs";

const repoRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const clientRoot = path.join( repoRoot, "apps", "client-next" );
const serverRoot = path.join( repoRoot, "apps", "server" );
const configDir = path.join( serverRoot, "config" );
const stateDir = path.join( serverRoot, ".state" );
const localStateDir = path.join( stateDir, "local" );
const binDir = path.join( localStateDir, "bin" );
const clusterStateDir = path.join( stateDir, "cluster" );
const catalogPath = path.join( configDir, "shards.json" );
const localEnvPath = path.join( configDir, "local.env" );
const devAccountPath = path.join( configDir, "dev-account.env" );
const exeSuffix = process.platform === "win32" ? ".exe" : "";

const vitePort = 5180;
const readyTimeoutMs = 300_000;
const readyPollMs = 400;
const stopTimeoutMs = 30_000;
const tokenLifetimeMs = 2 * 60 * 60_000;
const tokenRefreshMs = 30 * 60_000;
const childOutputTail = 40;

/*
================
main
================
*/
async function main() {
	const args = process.argv.slice( 2 );
	if ( args.length > 1 || (args.length === 1 && args[0] !== "--reset" && args[0] !== "--doctor") ) {
		console.error( "usage: pnpm local [--reset | --doctor]" );
		process.exit( 2 );
	}
	const settings = loadSettings();
	if ( args[0] === "--doctor" ) {
		process.exit( await doctor( settings, true ) === 0 ? 0 : 1 );
	}
	if ( args[0] === "--reset" ) {
		await resetWorld( settings );
	}
	if ( await doctor( settings, false ) !== 0 ) {
		console.error( "pnpm local: fix the above, or run `pnpm local --doctor` any time." );
		process.exit( 1 );
	}
	await run( settings );
}

/*
================
loadSettings

apps/server/config/local.env: LOCAL_* lines steer the runner, every
other KEY=VALUE is passed to both server processes as environment.
================
*/
function loadSettings() {
	const settings = { local: new Map(), env: new Map() };
	if ( !existsSync( localEnvPath ) ) {
		return settings;
	}
	for ( const raw of readFileSync( localEnvPath, "utf8" ).split( /\r?\n/ ) ) {
		const line = raw.trim();
		if ( line === "" || line.startsWith( "#" ) ) {
			continue;
		}
		const cut = line.indexOf( "=" );
		if ( cut < 1 ) {
			continue;
		}
		const key = line.slice( 0, cut ).trim();
		let value = line.slice( cut + 1 ).trim();
		if ( value.length > 1 && value.startsWith( '"' ) && value.endsWith( '"' ) ) {
			value = value.slice( 1, -1 );
		}
		(key.startsWith( "LOCAL_" ) ? settings.local : settings.env).set( key, value );
	}
	return settings;
}

/*
================
localSetting
================
*/
function localSetting( settings, key, fallback ) {
	return settings.local.get( key ) ?? fallback;
}

/*
================
readCatalog

The enabled shard row the run serves: its transport and control URLs
address the readiness probes and the GameWorld's control listener.
================
*/
function readCatalog( settings ) {
	const shardID = localSetting( settings, "LOCAL_SHARD_ID", "global-official" );
	const catalog = JSON.parse( readFileSync( catalogPath, "utf8" ) );
	const shard = catalog.shards.find( row => row.id === shardID );
	if ( !shard ) {
		throw new Error( `shards.json has no shard ${JSON.stringify( shardID )} (LOCAL_SHARD_ID)` );
	}
	if ( !shard.enabled ) {
		throw new Error( `shard ${JSON.stringify( shardID )} is disabled in shards.json` );
	}
	return shard;
}

/*
================
readDevAccount

The login `pnpm local` prints at the end: the shared development
account from apps/server/config/dev-account.env.
================
*/
function readDevAccount() {
	const values = new Map();
	for ( const raw of readFileSync( devAccountPath, "utf8" ).split( /\r?\n/ ) ) {
		const cut = raw.indexOf( "=" );
		if ( cut > 0 ) {
			values.set( raw.slice( 0, cut ).trim(), raw.slice( cut + 1 ).trim() );
		}
	}
	return { id: values.get( "SRO_DEV_ACCOUNT_ID" ) ?? "?", password: values.get( "SRO_DEV_ACCOUNT_PASSWORD" ) ?? "?" };
}

/*
================
doctor

Every prerequisite of a local run, each with its fix. Quiet mode (used
before a start) prints only the problems; --doctor prints the passes
too and never starts anything.
================
*/
async function doctor( settings, verbose ) {
	const problems = [];
	const passes = [];
	const check = ( ok, label, fix ) => (ok ? passes : problems).push( ok ? label : `${label} - fix: ${fix}` );

	const go = spawnSync( "go", [ "version" ], { encoding: "utf8" } );
	check( go.status === 0, "Go is installed", "install Go 1.27 (apps/server/go.mod) and put it on PATH" );
	check(
		existsSync( devAccountPath ),
		"development account file exists",
		"restore apps/server/config/dev-account.env from the checkout"
	);
	check(
		existsSync( catalogPath ),
		"shard catalog exists",
		"restore apps/server/config/shards.json from the checkout"
	);

	// The resolvers own both trees (AGENTS.md "Worktrees"), overrides included:
	// the Go server reads SRO_SERVER_GAME_DATA_ROOT, so the doctor must too.
	const mainCheckout = resolveMainCheckout( repoRoot );
	const serverData = resolveServerGameDataRoot( process.env, mainCheckout );
	check(
		existsSync( serverData ) && statSync( serverData ).isDirectory(),
		"server game data is built",
		"run `pnpm assets build full` then `pnpm task build server-game-data`"
	);
	// The client loads published assets only (the pack index), so a loose
	// tree with a stale manifest is not built enough: compare the startup
	// set against the manifest and name what it lacks.
	const clientPublic = path.join( resolveGeneratedRoot( process.env, mainCheckout ), "client-public" );
	const unpublished = await unpublishedStartupAssets( clientPublic );
	check( unpublished === null, "client startup assets are published", unpublished ?? "" );

	if ( problems.length === 0 ) {
		const shard = readCatalog( settings );
		const agentAddr = localSetting( settings, "LOCAL_AGENT_ADDR", "127.0.0.1:8787" );
		const ports = [
			[ "Agent", portOf( agentAddr ) ],
			[ "GameWorld transport", portOf( shard.transportUrl ) ],
			[ "GameWorld control", portOf( shard.controlUrl ) ],
			[ "Vite", vitePort ]
		];
		for ( const [label, port] of ports ) {
			check(
				await portFree( port ),
				`${label} port ${port} is free`,
				`stop what is using it (another \`pnpm local\`, or \`sro-nomad stop\` plus the dev agent)`
			);
		}
	}
	for ( const label of verbose ? passes : [] ) {
		console.log( `ok    ${label}` );
	}
	for ( const label of problems ) {
		console.error( `MISS  ${label}` );
	}
	return problems.length;
}

/*
================
portOf
================
*/
function portOf( address ) {
	const url = new URL( address.includes( "://" ) ? address : `http://${address}` );
	return Number( url.port );
}

/*
================
portFree
================
*/
function portFree( port ) {
	return new Promise( resolve => {
		const probe = net.createServer();
		probe.once( "error", () => resolve( false ) );
		probe.once( "listening", () => probe.close( () => resolve( true ) ) );
		probe.listen( port, "127.0.0.1" );
	} );
}

/*
================
stopAnnouncement

What a stop prints when it begins. A stdin close (no terminal) names
itself (#3081): it is the runner working as designed, and the fix is a
terminal, not a process hunt.
================
*/
function stopAnnouncement( reason ) {
	return reason === "stdin" ?
		"stdin closed: stopping (start with a terminal, or keep stdin open)" :
		"stopping...";
}

/*
================
resetWorld

--reset: move the local world aside (the reset of GETTING_STARTED
section 6) after asking, then fall through to a normal start, which
provisions a fresh one.
================
*/
async function resetWorld( settings ) {
	const shard = readCatalog( settings );
	const agentAddr = localSetting( settings, "LOCAL_AGENT_ADDR", "127.0.0.1:8787" );
	for ( const port of [ portOf( agentAddr ), portOf( shard.transportUrl ), vitePort ] ) {
		if ( !await portFree( port ) ) {
			console.error( `pnpm local --reset: port ${port} is in use; stop the running game first.` );
			process.exit( 1 );
		}
	}
	const answer = await ask(
		"Move the local world (accounts, characters, cluster state) aside and start fresh? [y/N] "
	);
	if ( answer.toLowerCase() !== "y" && answer.toLowerCase() !== "yes" ) {
		console.log( "reset cancelled" );
		process.exit( 0 );
	}
	const backup = path.join( stateDir, "backups", `local-reset-${backupStamp( new Date() )}` );
	let moved = 0;
	for ( const name of [ "cluster", "shards", "agent", "local" ] ) {
		const from = path.join( stateDir, name );
		if ( existsSync( from ) ) {
			mkdirSync( backup, { recursive: true } );
			renameSync( from, path.join( backup, name ) );
			moved++;
		}
	}
	console.log( moved === 0 ? "nothing to move aside; starting fresh" : `moved the old world to ${backup}` );
}

/*
================
ask
================
*/
function ask( question ) {
	return new Promise( resolve => {
		const rl = createInterface( { input: process.stdin, output: process.stdout } );
		rl.question( question, answer => {
			rl.close();
			resolve( answer.trim() );
		} );
	} );
}

/*
================
run
================
*/
async function run( settings ) {
	const shard = readCatalog( settings );
	mkdirSync( binDir, { recursive: true } );
	buildBinaries();
	// Before provisioning and before any child opens the store: an
	// authority left behind by a schema bump is upgraded in place (its
	// copy stays under .state/backups), never discovered by the
	// GameWorld's exact-version refusal.
	try {
		upgradeLocalAuthority( {
			dir: authorityDirFor( settings, shard.id ),
			tool: path.join( binDir, `sro-authority-upgrade${exeSuffix}` ),
			backupsDir: path.join( stateDir, "backups" )
		} );
	} catch ( error ) {
		console.error( `pnpm local: ${error.message}` );
		process.exit( 1 );
	}
	provision();
	const identity = await prepareIdentity( shard );
	const agentAddr = localSetting( settings, "LOCAL_AGENT_ADDR", "127.0.0.1:8787" );

	const agent = startChild( "agent", path.join( binDir, `sro-agent${exeSuffix}` ), [], {
		cwd: serverRoot,
		env: {
			SRO_SHARD_CATALOG_PATH: catalogPath,
			SRO_AGENT_ACCOUNTS_PATH: path.join( clusterStateDir, "accounts.json" ),
			SRO_AGENT_ACCOUNTS_DB_PATH: path.join( stateDir, "agent", "accounts.db" ),
			SRO_AGENT_DIRECTORY_STATE_PATH: path.join( stateDir, "agent", "shard-leases.json" ),
			SRO_AGENT_PROVISIONING_TOKEN_PATH: path.join( clusterStateDir, "agent-provisioning-token" ),
			SRO_AGENT_SESSION_KEYRING_PATH: path.join( clusterStateDir, "agent-session-keys.json" ),
			SRO_AGENT_API_ADDR: agentAddr,
			SRO_NOMAD_IDENTITY_ISSUER: identity.issuer,
			SRO_NOMAD_JWKS_URL: `${identity.issuer}/jwks.json`,
			SRO_NOMAD_NAMESPACE: "local"
		},
		settings
	} );
	const gameworld = startChild( "gameworld", path.join( binDir, `sro-gameworld${exeSuffix}` ), [], {
		cwd: serverRoot,
		env: {
			SRO_SHARD_CATALOG_PATH: catalogPath,
			SRO_SHARD_ID: shard.id,
			SRO_AGENT_URL: `http://${agentAddr}`,
			SRO_AGENT_URL_REQUIRE: "1",
			SRO_AGENT_IDENTITY_FILE: identity.tokenPath,
			SRO_AGENT_SESSION_PUBLIC_KEYS_PATH: identity.sessionKeysPath,
			SRO_GAMEWORLD_CONTROL_ADDR: hostPortOf( shard.controlUrl ),
			SRO_AUTHORITY_REQUIRE: "1"
		},
		settings
	} );
	const vite = startVite();

	const children = [ agent, gameworld, vite ];
	const state = { stopping: false, consoleStop: false, stopReason: null, failed: null };
	watchChildren( children, state );
	installStopHandlers( children, state );

	try {
		await waitReady( `http://${agentAddr}/readyz`, "Agent", children, state );
		await waitReady( `${shard.transportUrl}/transport/readyz`, "GameWorld", children, state );
		await waitReady( `http://127.0.0.1:${vitePort}/`, "Vite", children, state );
	} catch ( error ) {
		// A stdin-closed stop already printed its own reason; a
		// "never became ready" line after it only sends the reader
		// hunting for a crashed process (#3081).
		if ( state.stopReason !== "stdin" ) {
			console.error( `pnpm local: ${error.message}` );
		}
		await stopChildren( children, state );
		process.exit( 1 );
	}
	const account = readDevAccount();
	console.log(
		`Local game is ready: open http://127.0.0.1:${vitePort}/ and log in as ${account.id} / ${account.password} (apps/server/config/dev-account.env).`
	);
	console.log( "Ctrl+C stops the client, the GameWorld and the Agent." );

	const code = await new Promise( resolve => {
		state.onStopped = resolve;
	} );
	process.exit( code );
}

/*
================
buildBinaries

The three Go programs a local run needs, into .state/local/bin.
================
*/
function buildBinaries() {
	// A local run from any checkout reuses what another already compiled.
	const env = sharedCacheEnvironment();
	for (
		const [name, pkg] of [
			[ "sro-agent", "./cmd/services/sro-agent" ],
			[ "sro-gameworld", "./cmd/services/sro-gameworld" ],
			[ "sro-bootstrap-development", "./cmd/operations/sro-bootstrap-development" ],
			[ "sro-authority-upgrade", "./cmd/tools/sro-authority-upgrade" ]
		]
	) {
		console.log( `building ${name}...` );
		const out = path.join( binDir, name + exeSuffix );
		const result = spawnSync( "go", [ "build", "-o", out, pkg ], { cwd: serverRoot, stdio: "inherit", env } );
		if ( result.status !== 0 ) {
			console.error( `pnpm local: go build ${pkg} failed` );
			process.exit( 1 );
		}
	}
}

/*
================
provision

The existing development bootstrap: cluster identity, the shared dev
account and an empty authority per enabled shard. Idempotent.
================
*/
function provision() {
	const result = spawnSync( path.join( binDir, `sro-bootstrap-development${exeSuffix}` ), [], {
		cwd: serverRoot,
		stdio: "inherit"
	} );
	if ( result.status !== 0 ) {
		console.error( "pnpm local: development provisioning failed" );
		process.exit( 1 );
	}
}

/*
================
prepareIdentity

The local workload identity. A development Ed25519 key lives in
.state/local (created once); the runner serves its JWKS on an
ephemeral loopback port for the run, mints the GameWorld's control
token from it (refreshed as it ages; the GameWorld re-reads the file
per call), and derives the session public keys the GameWorld verifies
player sessions with from the bootstrap's key ring.
================
*/
async function prepareIdentity( shard ) {
	mkdirSync( localStateDir, { recursive: true } );
	const keyPath = path.join( localStateDir, "workload-key.json" );
	let stored;
	if ( existsSync( keyPath ) ) {
		stored = JSON.parse( readFileSync( keyPath, "utf8" ) );
	} else {
		const pair = generateKeyPairSync( "ed25519" );
		const jwk = pair.privateKey.export( { format: "jwk" } );
		stored = { kid: `local-${Date.now().toString( 36 )}`, jwk };
		writeFileSync( keyPath, JSON.stringify( stored ), { mode: 0o600 } );
	}
	const privateKey = createPrivateKey( { key: stored.jwk, format: "jwk" } );
	const publicJwk = {
		...createPublicKey( privateKey ).export( { format: "jwk" } ),
		kid: stored.kid,
		alg: "EdDSA",
		use: "sig"
	};

	const jwks = http.createServer( ( request, response ) => {
		if ( request.url === "/jwks.json" ) {
			response.writeHead( 200, { "content-type": "application/json" } );
			response.end( JSON.stringify( { keys: [ publicJwk ] } ) );
		} else {
			response.writeHead( 404 ).end();
		}
	} );
	await new Promise( resolve => jwks.listen( 0, "127.0.0.1", resolve ) );
	const issuer = `http://127.0.0.1:${jwks.address().port}`;

	const tokenPath = path.join( localStateDir, "gameworld.jwt" );
	const mint = () =>
		writeFileSync( tokenPath, mintToken( privateKey, stored.kid, issuer, shard.id ), { mode: 0o600 } );
	mint();
	setInterval( mint, tokenRefreshMs ).unref();

	const ring = JSON.parse( readFileSync( path.join( clusterStateDir, "agent-session-keys.json" ), "utf8" ) );
	const sessionKeysPath = path.join( localStateDir, "agent-session-public-keys.json" );
	writeFileSync(
		sessionKeysPath,
		JSON.stringify( {
			keys: ring.keys.map( key => ({
				id: key.id,
				publicKey: Buffer.from( key.privateKey, "base64url" ).subarray( 32 ).toString( "base64url" ),
				createdAt: key.createdAt
			}) )
		} )
	);
	return { issuer, tokenPath, sessionKeysPath };
}

/*
================
mintToken

The GameWorld's workload token, shaped exactly as the Agent's control
authorization reads it: namespace, job id and task name the claims
from internal/agent/server (sro-gameworld-<shard>, task gameworld).
================
*/
function mintToken( privateKey, kid, issuer, shardID ) {
	const now = Math.floor( Date.now() / 1000 );
	const encode = value => Buffer.from( JSON.stringify( value ) ).toString( "base64url" );
	const head = encode( { alg: "EdDSA", kid, typ: "JWT" } );
	const body = encode( {
		iss: issuer,
		aud: "sro-agent",
		iat: now,
		exp: now + Math.floor( tokenLifetimeMs / 1000 ),
		nomad_namespace: "local",
		nomad_job_id: `sro-gameworld-${shardID}`,
		nomad_allocation_id: "local",
		nomad_task: "gameworld"
	} );
	const signature = sign( null, Buffer.from( `${head}.${body}` ), privateKey ).toString( "base64url" );
	return `${head}.${body}.${signature}`;
}

/*
================
startChild

One server process: the local settings file's environment first, the
runner's computed environment over it, output prefixed by name. The
last output lines are kept for a failure dump.
================
*/
function startChild( name, command, args, options ) {
	const child = spawn( command, args, {
		cwd: options.cwd,
		env: { ...process.env, ...Object.fromEntries( options.settings.env ), ...options.env },
		stdio: [ "ignore", "pipe", "pipe" ],
		windowsHide: false
	} );
	child.sroName = name;
	child.sroTail = [];
	prefixOutput( child, name, child.sroTail );
	return child;
}

/*
================
startVite

The browser client's dev server, spawned as vite.js under this same
Node with apps/client-next as root (its vite.config owns the port and
the /api proxy; the flags mirror its dev script). Going through the
pnpm shim instead would make the child a launcher that can exit ahead
of the server on Windows and orphan the port holder, so the runner
starts the process that actually holds port 5180.
================
*/
function startVite() {
	const require = createRequire( path.join( repoRoot, "package.json" ) );
	const viteBin = path.join( path.dirname( require.resolve( "vite/package.json" ) ), "bin", "vite.js" );
	const child = spawn(
		process.execPath,
		[ viteBin, "--host", "127.0.0.1", "--port", String( vitePort ), "--strictPort" ],
		{
			cwd: clientRoot,
			env: process.env,
			stdio: [ "ignore", "pipe", "pipe" ],
			windowsHide: false
		}
	);
	child.sroName = "vite";
	child.sroTail = [];
	prefixOutput( child, "vite", child.sroTail );
	return child;
}

/*
================
prefixOutput
================
*/
function prefixOutput( child, name, tail ) {
	for ( const stream of [ child.stdout, child.stderr ] ) {
		let pending = "";
		stream.setEncoding( "utf8" );
		stream.on( "data", chunk => {
			pending += chunk;
			const lines = pending.split( /\r?\n/ );
			pending = lines.pop();
			for ( const line of lines ) {
				if ( line !== "" ) {
					tail.push( line );
					if ( tail.length > childOutputTail ) {
						tail.shift();
					}
				}
				console.log( `[${name}] ${line}` );
			}
		} );
	}
}

/*
================
watchChildren

A child that exits while the run should be up fails the run: dump its
last output and stop the rest, so nothing is left orphaned either way.
================
*/
function watchChildren( children, state ) {
	for ( const child of children ) {
		child.on( "exit", ( code, signal ) => {
			child.sroExited = true;
			if ( state.stopping ) {
				maybeStopped( children, state );
				return;
			}
			state.failed = `${child.sroName} exited early (code ${code}, signal ${signal})`;
			console.error( `pnpm local: ${state.failed}` );
			for ( const line of child.sroTail ) {
				console.error( `[${child.sroName}] ${line}` );
			}
			void stopChildren( children, state ).then( () => process.exit( 1 ) );
		} );
		child.on( "error", error => {
			if ( !state.stopping ) {
				state.failed = `${child.sroName} failed to start: ${error.message}`;
				console.error( `pnpm local: ${state.failed}` );
				void stopChildren( children, state ).then( () => process.exit( 1 ) );
			}
		} );
	}
}

/*
================
installStopHandlers

Ctrl+C (and SIGTERM/SIGBREAK, and the runner's stdin closing under a
harness) stop the run. A console signal already reached the children
through the shared console, so it only marks the stop graceful - the
runner then waits for their own shutdown. A stdin close is a
programmatic stop: POSIX children still get a forwarded SIGTERM, but
on Windows no graceful channel exists (see the file banner), so the
stop goes straight to killing trees.
================
*/
function installStopHandlers( children, state ) {
	const begin = ( graceful, reason ) => {
		if ( !state.stopping ) {
			state.consoleStop = graceful;
			state.stopReason = reason;
			console.log( stopAnnouncement( reason ) );
			void stopChildren( children, state );
		}
	};
	process.on( "SIGINT", () => begin( true, "console" ) );
	process.on( "SIGTERM", () => begin( true, "console" ) );
	process.on( "SIGBREAK", () => begin( true, "console" ) );
	if ( !process.stdin.isTTY ) {
		process.stdin.resume();
		process.stdin.once( "end", () => begin( false, "stdin" ) );
		process.stdin.once( "close", () => begin( false, "stdin" ) );
	}
}

/*
================
stopChildren

GameWorld first (its store closes on its own signal handling), then
Agent, then Vite. The grace period is the full stop timeout whenever
the children had a graceful signal (a console stop, or the SIGTERM
the POSIX path just sent); a programmatic stop on Windows has no
graceful signal to wait on, so it goes straight to the kill.
Anything still alive after the grace period is killed by tree.
================
*/
async function stopChildren( children, state ) {
	if ( state.stopping ) {
		return;
	}
	state.stopping = true;
	const order = [ "gameworld", "agent", "vite" ]
		.map( name => children.find( child => child.sroName === name ) )
		.filter( Boolean );
	for ( const child of order ) {
		signalChild( child );
	}
	const graceMs = process.platform === "win32" && !state.consoleStop ? 0 : stopTimeoutMs;
	const deadline = Date.now() + graceMs;
	while ( Date.now() < deadline && children.some( child => !child.sroExited ) ) {
		await new Promise( resolve => setTimeout( resolve, 200 ) );
	}
	for ( const child of children ) {
		if ( !child.sroExited ) {
			killTree( child );
		}
	}
	maybeStopped( children, state );
	if ( state.onStopped ) {
		state.onStopped( state.failed ? 1 : 0 );
	}
}

/*
================
maybeStopped
================
*/
function maybeStopped( children, state ) {
	if ( state.stopping && state.onStopped && children.every( child => child.sroExited ) ) {
		const done = state.onStopped;
		state.onStopped = null;
		done( state.failed ? 1 : 0 );
	}
}

/*
================
signalChild

Ask one child to stop gracefully where a targeted signal exists:
POSIX gets SIGTERM. On Windows there is none - a real Ctrl+C already
reached the children through the shared console (the stop is marked
graceful and this stays a no-op so stopChildren simply waits), and
GenerateConsoleCtrlEvent cannot be aimed at one child or group: a
pid-targeted broadcast at a detached child never arrives, and a
group-0 broadcast hits every process on the console, parent shells
included. Windows programmatic stops therefore rely on killTree;
its /F terminate is safe for the local stores (bbolt commits are
atomic; a dev world loses at most the in-flight tick).
================
*/
function signalChild( child ) {
	if ( child.sroExited || process.platform === "win32" ) {
		return;
	}
	child.kill( "SIGTERM" );
}

/*
================
killTree
================
*/
function killTree( child ) {
	if ( process.platform === "win32" ) {
		spawnSync( "taskkill", [ "/PID", String( child.pid ), "/T", "/F" ], { stdio: "ignore" } );
	} else {
		child.kill( "SIGKILL" );
	}
}

/*
================
waitReady

Poll a readiness URL until it answers 200, a child dies, or the boot
times out (a first GameWorld boot fills the world; it may take a few
minutes).
================
*/
async function waitReady( url, label, children, state ) {
	const deadline = Date.now() + readyTimeoutMs;
	console.log( `waiting for ${label}...` );
	while ( Date.now() < deadline ) {
		if ( state.failed ) {
			throw new Error( state.failed );
		}
		if ( children.some( child => child.sroExited ) ) {
			throw new Error( `${label} never became ready (a process exited)` );
		}
		try {
			const response = await fetch( url, { signal: AbortSignal.timeout( 2000 ) } );
			if ( response.status === 200 ) {
				console.log( `${label} is ready` );
				return;
			}
		} catch {
			// Not up yet; keep polling.
		}
		await new Promise( resolve => setTimeout( resolve, readyPollMs ) );
	}
	throw new Error( `${label} did not become ready in ${Math.round( readyTimeoutMs / 1000 )}s (${url})` );
}

/*
================
hostPortOf
================
*/
function hostPortOf( url ) {
	const parsed = new URL( url );
	return `${parsed.hostname}:${parsed.port}`;
}

// The stop announcement is importable so its test runs without a
// stack (the pre-boot checks are preboot.mjs); the runner itself
// starts only when this file is the program.
export { stopAnnouncement };

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main().catch( error => {
		console.error( `pnpm local: ${error.message}` );
		process.exit( 1 );
	} );
}
