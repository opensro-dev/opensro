/*
===========================================================================

headless-simulation.test.mjs - the release gate runs a worker the way a page does

A stand-in release worker talks to a local HTTP origin through the headless
runner: it must see browser same-origin behaviour (Origin header, cookies on
credentialed requests only, cookies kept across a reload), and the runner
must acknowledge world batches and recycle snapshot buffers.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
	bootstrapSource,
	simulationWorkerPath,
	startHeadlessSimulation
} from "../../tools/beta/headless-simulation.mjs";

// A stand-in for the release's simulation worker: HostMessage in,
// WorkerMessage out, with one credentialed and one anonymous request.
const FAKE_WORKER = `
let sequence = 0, acked = 0, recycled = 0;
globalThis.onmessage = async ( { data } ) => {
	if ( data.kind === "recycle" ) recycled++;
	if ( data.kind === "world-ack" ) acked = data.sequence;
	if ( data.kind !== "session" ) return;
	const base = data.command.apiBase;
	const login = await fetch( base + "/login", { method: "POST", credentials: "include" } );
	const anonymous = await fetch( base + "/anonymous", { credentials: "omit" } );
	const again = await fetch( base + "/login", { method: "POST", credentials: "include" } );
	postMessage( { kind: "snapshot", buffer: new ArrayBuffer( 8 ) }, [] );
	postMessage( { kind: "world", batch: { sequence: ++sequence, events: [
		{ kind: "spawn", entity: { gid: 7, name: "Probe", x: 1, y: 0, z: 2 } }
	] } } );
	await new Promise( resolve => setTimeout( resolve, 100 ) );
	postMessage( { kind: "session", state: { phase: "signed-out", seen: {
		login: await login.json(), anonymous: await anonymous.json(), again: await again.json(), acked, recycled,
		// The incident report's build name, as the bundler compiles import.meta.url in a worker.
		build: new URL( self.location.href ).pathname.split( "/" ).at( -1 )
	} } } );
};
`;

/*
================
serve

An origin that sets a session cookie and echoes the headers it received.
================
*/
async function serve() {
	const server = createServer( ( request, response ) => {
		const seen = { origin: request.headers.origin ?? null, cookie: request.headers.cookie ?? null };
		if ( (request.url ?? "").endsWith( "/login" ) ) {
			response.setHeader( "Set-Cookie", "session=abc; Path=/; HttpOnly" );
		}
		response.setHeader( "Content-Type", "application/json" );
		response.end( JSON.stringify( seen ) );
	} );
	await new Promise( resolve => server.listen( 0, "127.0.0.1", () => resolve( undefined ) ) );
	const address = /** @type {import("node:net").AddressInfo} */ (server.address());
	return { server, origin: `http://127.0.0.1:${address.port}` };
}

test("the release's worker is found by its simulation name, exactly once", () => {
	const script = 'x=new Worker(new URL("/assets/entry-A.js",""+import.meta.url),{type:"module",name:"sro-assets"});' +
		'y=new Worker(new URL("/assets/entry-B.js",""+import.meta.url),{type:"module",name:"sro-simulation"});';
	assert.equal( simulationWorkerPath( script ), "/assets/entry-B.js" );
	assert.throws( () => simulationWorkerPath( "no workers here" ), /found 0/ );
	assert.throws( () => simulationWorkerPath( script + script ), /found 2/ );
});

test("the worker gets browser same-origin behaviour and the page's acknowledgements", async t => {
	const directory = await mkdtemp( path.join( os.tmpdir(), "sro-headless-" ) );
	const { server, origin } = await serve();
	t.after( async () => {
		server.close();
		await rm( directory, { recursive: true, force: true } );
	} );
	await writeFile( path.join( directory, "worker.mjs" ), FAKE_WORKER );
	await writeFile( path.join( directory, "bootstrap.mjs" ), bootstrapSource() );
	const files = {
		workerUrl: pathToFileURL( path.join( directory, "worker.mjs" ) ).href,
		bootstrapUrl: pathToFileURL( path.join( directory, "bootstrap.mjs" ) )
	};
	const simulation = await startHeadlessSimulation( { origin, ...files } );
	simulation.session( { kind: "servers", apiBase: origin + "/api" } );
	const state = await simulation.waitFor( s => s.session?.seen ? s.session : null, 10_000, "fake session" );
	assert.deepEqual( state.seen.login, { origin, cookie: null } );
	assert.deepEqual( state.seen.anonymous, { origin, cookie: null } );
	assert.deepEqual( state.seen.again, { origin, cookie: "session=abc" } );
	assert.equal( state.seen.acked, 1 );
	assert.equal( state.seen.recycled, 1 );
	assert.equal( state.seen.build, "worker.mjs", "the worker scope has self and its own location" );
	assert.equal( simulation.entityNamed( "Probe" )?.gid, 7 );
	const cookies = simulation.cookies();
	await simulation.stop();

	// A reload keeps the jar.
	const reloaded = await startHeadlessSimulation( { origin, ...files, cookies } );
	reloaded.session( { kind: "servers", apiBase: origin + "/api" } );
	const after = await reloaded.waitFor( s => s.session?.seen ? s.session : null, 10_000, "reloaded session" );
	assert.equal( after.seen.login.cookie, "session=abc" );
	await reloaded.stop();
});
