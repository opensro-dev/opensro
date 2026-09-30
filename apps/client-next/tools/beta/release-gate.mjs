/*
===========================================================================

release-gate.mjs - prove a browser release works, without rendering it

The release gate a candidate must pass before publication. A browser loads
the staged candidate and must boot its runtime without errors (title); the
rest runs the release's own simulation worker headlessly
(headless-simulation.mjs): login, the probe's single-character roster,
world entry, a server-confirmed move and back (gameplay), and a reload that
restores the session into the world (resume). Scene rendering is not part
of this gate: under software WebGPU it made the gate slower with every
player in view. The rendered flow runs as release-smoke.mjs where a GPU is
available.

The report has the phases and shape the host records as browser evidence
(ops/release/client_deploy.record_smoke).

	node release-gate.mjs CANDIDATE_JSON OUTPUT_DIRECTORY

===========================================================================
*/
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { bootstrapSource, simulationWorkerPath, startHeadlessSimulation } from "./headless-simulation.mjs";

const GATE_FORMAT = "headless-gate-v1";
const HTTP_OK = 200;
const BOOT_BUDGET_MS = 180_000;
const STEP_BUDGET_MS = 60_000;
const MOVE_BUDGET_MS = 20_000;
// A short, server-confirmed move and back: small enough to stay walkable in
// the probe's town, large enough to be a real movement.
const MOVE_DISTANCE = 4;
const PROBE_VIEWPORT = { width: 1024, height: 768 };

/*
================
passPhase
================
*/
function passPhase( result, phase ) {
	const elapsedMs = Date.now() - result.startedAt;
	const previousMs = result.phaseTimings.at( -1 )?.elapsedMs ?? 0;
	result.phases[phase] = "PASS";
	const timing = { phase, elapsedMs, durationMs: elapsedMs - previousMs };
	result.phaseTimings.push( timing );
	console.log( "Release phase:", JSON.stringify( timing ) );
}

/*
================
bootInBrowser

The candidate's entry must be the approved bytes, and its runtime must start
in a real browser without page or runtime errors. Returns the application
script the page loaded.
================
*/
async function bootInBrowser( url, candidate ) {
	const { browser, page } = await launchProbeBrowser( {
		headed: process.env.RELEASE_HEADED === "1",
		viewport: PROBE_VIEWPORT,
		...(process.env.RELEASE_CHROME ? { executablePath: process.env.RELEASE_CHROME } : {})
	} );
	const errors = [];
	page.on( "pageerror", error => errors.push( String( error ) ) );
	page.on( "console", message => {
		if ( message.type() === "error" && message.text().includes( "[SRO runtime]" ) ) errors.push( message.text() );
	} );
	try {
		const response = await page.goto( url, { waitUntil: "commit" } );
		if ( !response || response.status() !== HTTP_OK ) throw Error( "Candidate entry is unavailable" );
		const entry = await response.body();
		if ( createHash( "sha256" ).update( entry ).digest( "hex" ) !== candidate.entrySha256 ) {
			throw Error( "HTTPS served a different candidate entry" );
		}
		await page.waitForFunction(
			() => document.querySelector( "output" )?.textContent?.startsWith( "Replacement runtime: running" ),
			null,
			{ timeout: BOOT_BUDGET_MS }
		);
		if ( errors.length ) throw Error( "Runtime errors while booting: " + errors.join( "; " ) );
		const script = /src="([^"]+\.js)"/.exec( entry.toString( "utf8" ) )?.[1];
		if ( !script ) throw Error( "Candidate entry names no application script" );
		return new URL( script, url ).href;
	} finally {
		await browser.close();
	}
}

/*
================
prepareWorker

Fetch the release's simulation worker exactly as served, with the preamble
the headless runner loads first.
================
*/
async function prepareWorker( origin, script, directory ) {
	const application = await fetch( script );
	if ( !application.ok ) throw Error( "Application script is unavailable" );
	const workerPath = simulationWorkerPath( await application.text() );
	const worker = await fetch( new URL( workerPath, origin ) );
	if ( !worker.ok ) throw Error( "Simulation worker is unavailable: " + workerPath );
	const workerFile = path.join( directory, "simulation-worker.mjs" ),
		bootstrapFile = path.join( directory, "bootstrap.mjs" );
	await writeFile( workerFile, Buffer.from( await worker.arrayBuffer() ) );
	await writeFile( bootstrapFile, bootstrapSource() );
	return {
		workerUrl: pathToFileURL( workerFile ).href,
		bootstrapUrl: pathToFileURL( bootstrapFile ),
		workerPath
	};
}

/*
================
enterWorld

Tell the server this client is ready as soon as the session is in the world
(the page does so once its scene is drawn), then wait for our own spawn.
================
*/
async function enterWorld( simulation, name, label ) {
	await simulation.waitFor( state => state.session?.phase === "world", STEP_BUDGET_MS, label );
	simulation.session( { kind: "world-ready", travelRevision: 0 } );
	return simulation.waitFor( () => simulation.entityNamed( name ), STEP_BUDGET_MS, label + " spawn" );
}

/*
================
moveAndConfirm

One server-confirmed move: the server's echo of our entity must carry a new
movement toward the requested destination.
================
*/
async function moveAndConfirm( simulation, name, destination ) {
	const before = simulation.entityNamed( name );
	const revision = before?.movementRevision ?? -1;
	simulation.session( { kind: "gameplay", command: { kind: "move", destination } } );
	return simulation.waitFor(
		() => {
			const entity = simulation.entityNamed( name );
			if ( !entity || (entity.movementRevision ?? -1) === revision ) return null;
			const to = entity.movementPath?.to ?? entity;
			return Math.hypot( to.x - destination.x, to.z - destination.z ) < MOVE_DISTANCE / 2 ? entity : null;
		},
		MOVE_BUDGET_MS,
		"gameplay move"
	);
}

/*
================
exercise
================
*/
async function exercise( candidate, credentials, origin, result, directory ) {
	const url = `${origin}/releases/candidates/${candidate.candidate}/index.html`;
	const script = await bootInBrowser( url, candidate );
	const worker = await prepareWorker( origin, script, directory );
	result.simulationWorker = worker.workerPath;
	passPhase( result, "title" );

	const apiBase = origin + "/api";
	let simulation = await startHeadlessSimulation( { origin, ...worker } );
	try {
		simulation.session( { kind: "servers", apiBase } );
		await simulation.waitFor(
			state => state.session?.servers?.some( row => row.id === credentials.shard ),
			STEP_BUDGET_MS,
			"server list"
		);
		simulation.session( {
			kind: "login",
			apiBase,
			id: credentials.username,
			password: credentials.password,
			serverId: credentials.shard
		} );
		await simulation.waitFor( state => state.session?.phase === "character-select", STEP_BUDGET_MS, "login" );
		passPhase( result, "login" );
		// As the page does: character-select without characters asks for them.
		if ( !simulation.state.session?.characters ) simulation.session( { kind: "roster" } );
		const characters = await simulation.waitFor(
			state =>
				state.session?.phase === "character-select" && Array.isArray( state.session.characters ) ?
					state.session.characters :
					null,
			STEP_BUDGET_MS,
			"roster"
		);
		if ( characters.length !== 1 || characters[0].name !== credentials.character ) {
			throw Error(
				`Release probe requires its dedicated single-character roster; got ${characters.length}: ` +
					characters.map( row => row.name ).join( ", " )
			);
		}
		passPhase( result, "roster" );

		simulation.session( { kind: "enter-world", character: credentials.character } );
		const spawned = await enterWorld( simulation, credentials.character, "world" );
		passPhase( result, "world" );

		const home = {
			regionId: spawned.regionId,
			x: spawned.x,
			y: spawned.y,
			z: spawned.z,
			angle: spawned.heading ?? 0
		};
		let moved = null;
		for ( const dx of [ MOVE_DISTANCE, -MOVE_DISTANCE ] ) {
			try {
				moved = await moveAndConfirm( simulation, credentials.character, { ...home, x: home.x + dx } );
				break;
			} catch ( error ) {
				if ( dx < 0 ) throw error;
			}
		}
		if ( !moved ) throw Error( "gameplay move was not confirmed" );
		await moveAndConfirm( simulation, credentials.character, home );
		passPhase( result, "gameplay" );

		// A reload: a new worker with the same cookies restores the session.
		const cookies = simulation.cookies();
		await simulation.stop();
		simulation = await startHeadlessSimulation( { origin, ...worker, cookies } );
		simulation.session( { kind: "servers", apiBase } );
		await enterWorld( simulation, credentials.character, "resume" );
		passPhase( result, "resume" );
	} finally {
		result.session = simulation.state.session?.phase ?? null;
		result.worldEvents = simulation.state.events;
		await simulation.stop().catch( () => {} );
	}
}

/*
================
main
================
*/
async function main() {
	const [candidatePath, destination] = process.argv.slice( 2 );
	if ( !candidatePath || !destination ) throw Error( "Usage: release-gate.mjs CANDIDATE_JSON OUTPUT_DIRECTORY" );
	const candidate = JSON.parse( await readFile( candidatePath, "utf8" ) );
	const credentials = JSON.parse( process.env.RELEASE_PROBE_ACCOUNT ?? "{}" );
	assertCharacterAllowed( credentials.character );
	if ( !credentials.username || !credentials.password || !credentials.character || !credentials.shard ) {
		throw Error( "Missing release probe account" );
	}
	if ( !process.env.RELEASE_ORIGIN ) throw Error( "Missing RELEASE_ORIGIN for the host under test" );
	const origin = new URL( process.env.RELEASE_ORIGIN ).origin;
	await mkdir( destination, { recursive: true } );
	const result = {
		...candidate,
		gate: GATE_FORMAT,
		verdict: "FAIL",
		phases: {},
		phaseTimings: [],
		errors: [],
		startedAt: Date.now()
	};
	try {
		await exercise( candidate, credentials, origin, result, destination );
		result.verdict = "PASS";
	} catch ( error ) {
		result.failure = String( error );
		result.errors.push( result.failure );
		process.exitCode = 1;
	} finally {
		result.finishedAt = Date.now();
		await writeFile( path.join( destination, "report.json" ), JSON.stringify( result, null, 2 ) );
		console.log( JSON.stringify( { verdict: result.verdict, phases: result.phases, failure: result.failure } ) );
	}
}

if ( process.argv[1] && import.meta.url === pathToFileURL( path.resolve( process.argv[1] ) ).href ) await main();
