/*
===========================================================================

runtime-text-admission.test.mjs - the HUD and worker agree with publication

Records the production HUD's requests rather than searching source text.
The actual worker reads raw-table pack bytes and rejects unlisted assets
even when a loose URL could answer them.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { runtimeTextPack } from "../helpers/runtime-text-pack.mjs";
import { REQUIRED_RUNTIME_TEXT_ASSETS } from "../../../../scripts/build/assetPackOwnership.mjs";
const { createHudResources } = await import( "../../src/engine/runtime/ui/hud/resources.ts" );
const { createPacks, AssetAbsentError } = await import( "../../src/engine/runtime/assets/worker/packs/packs.ts" );
const BASE = "https://runtime-text.invalid";

/*
================
TestHudRawRequirements
================
*/
test("every non-JSON HUD metadata request has explicit required pack ownership", () => {
	/** @type {string[]} */
	const requests = [];
	const hud = createHudResources( {
		available: () => Number.MAX_SAFE_INTEGER,
		request: url => {
			requests.push( new URL( url ).pathname );
			return requests.length;
		},
		take: () => null,
		cancel: () => {}
	}, BASE );
	try {
		hud.step();
		const raw = requests.filter( name => !name.endsWith( ".json" ) );
		assert.ok( raw.includes( "/assets/config/command.txt" ) );
		assert.ok( raw.includes( "/assets/textdata/abusefilter.txt" ) );
		assert.deepEqual( raw.filter( name => !REQUIRED_RUNTIME_TEXT_ASSETS.includes( name ) ), [] );
	} finally {
		hud.dispose();
	}
});

/*
================
TestWorkerRawTables
================
*/
test("the worker reads every required raw table from its published SROPACK2 member", async () => {
	const fixture = runtimeTextPack();
	const packs = createPacks( async url => {
		const name = new URL( url ).pathname;
		if ( name === "/assets/packs/manifest.json" ) {
			return new TextEncoder().encode( JSON.stringify( fixture.index ) );
		}
		assert.equal( name, fixture.packPath, "no loose URL is available" );
		return new Uint8Array( fixture.bytes );
	} );
	try {
		for ( const name of REQUIRED_RUNTIME_TEXT_ASSETS ) {
			const bytes = await packs.read(
				new URL( name, BASE ),
				fixture.payload.length,
				new AbortController().signal
			);
			assert.deepEqual( Buffer.from( bytes ), fixture.payload );
		}
	} finally {
		packs.dispose();
	}
});

/*
================
TestLooseFileCannotReplaceMissingMembership
================
*/
test("a loose command table cannot satisfy an absent manifest member", async () => {
	const missing = "/assets/config/command.txt";
	const fixture = runtimeTextPack( REQUIRED_RUNTIME_TEXT_ASSETS.filter( name => name !== missing ) );
	/** @type {string[]} */
	const requests = [];
	const packs = createPacks( async url => {
		const name = new URL( url ).pathname;
		requests.push( name );
		return name === "/assets/packs/manifest.json" ?
			new TextEncoder().encode( JSON.stringify( fixture.index ) ) :
			new Uint8Array( fixture.payload );
	} );
	try {
		await assert.rejects(
			packs.read( new URL( missing, BASE ), fixture.payload.length, new AbortController().signal ),
			AssetAbsentError
		);
		assert.deepEqual( requests, [ "/assets/packs/manifest.json" ] );
	} finally {
		packs.dispose();
	}
});
