/*
===========================================================================

bug-report-upload.test.mjs - bounded, replayable technical report attachments

Read the actual ZIP bytes with Node's independent inflater. Shared reports
retain movement identities and clocks without exporting chat or credentials.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readZipEntries } from "../helpers/zip-reader.mjs";
import { createHash } from "node:crypto";
const { createDiagnosticUpload, diagnosticUploadBudget, diagnosticValue } = await import(
	"../../src/engine/runtime/bug-report/archive.ts"
);
const OPTIONS = { id: "BR-261007-0125-52FC", clip: { start: 3, end: 8 }, maxBytes: 2 * 1024 * 1024 };

test("old or malformed Agent capabilities keep reports on the legacy multipart contract", () => {
	for ( const advertised of [ undefined, null, false, "2097152", 0, -1, NaN, Infinity, 4095 ] ) {
		assert.equal( diagnosticUploadBudget( 10 * 1024 * 1024, advertised ), 0 );
	}
	assert.equal( diagnosticUploadBudget( 10 * 1024 * 1024, 2 * 1024 * 1024 ), 2 * 1024 * 1024 );
	assert.equal( diagnosticUploadBudget( 10 * 1024 * 1024, 100 * 1024 * 1024 ), 2 * 1024 * 1024 );
	assert.equal( diagnosticUploadBudget( 8192, 2 * 1024 * 1024 ), 8192 );
});

/*
================
readZip
================
*/
async function readZip( blob ) {
	const entries = readZipEntries( await blob.arrayBuffer() );
	const documents = {}, methods = [];
	for ( const [name, entry] of entries ) {
		documents[name] = JSON.parse( entry.data.toString() );
		methods.push( entry.method );
	}
	return { documents, methods };
}

/*
================
fixture
================
*/
function fixture() {
	return {
		"movement.json": JSON.stringify( {
			mainTimeOriginMs: 100,
			simulationOriginMs: 80,
			localGid: 7,
			events: [ { kind: "movement", atMs: 120, token: 9, revision: 2, pose: { x: 12, y: 0, z: 4 } } ]
		} ),
		"timeline.json": JSON.stringify( {
			events: [ { kind: "chat", text: "PRIVATE_CHAT" }, {
				kind: "sample",
				t: 3,
				hp: 200,
				targetName: "PRIVATE_NAME"
			} ]
		} ),
		"state.json": JSON.stringify( {
			session: { accessToken: "PRIVATE_TOKEN", account: "PRIVATE_ACCOUNT" },
			entities: 12,
			gameplay: {
				localGid: 7,
				pose: { x: 12, y: 0, z: 4 },
				casts: [ { token: 9, caster: 7 } ],
				chat: { lines: [ "PRIVATE_CHAT" ] },
				social: { guild: "PRIVATE_GUILD" }
			}
		} ),
		"environment.json": JSON.stringify( {
			cpuThreads: 8,
			preferences: { token: "PRIVATE_STORAGE" },
			userAgent: "test"
		} )
	};
}

test("the shared report replacer keeps bigint, Map, Set and binary values serializable", () => {
	// Bare JSON.stringify throws on the bigint; every report stringify goes
	// through diagnosticValue (archive.ts), so none of them can.
	const text = JSON.stringify( {
		sessionId: 9007199254740993n,
		seen: new Map( [ [ "a", 1 ] ] ),
		tags: new Set( [ "x" ] ),
		bytes: new Uint8Array( 3 )
	}, diagnosticValue );
	assert.equal(
		text,
		'{"sessionId":"9007199254740993","seen":{"a":1},"tags":["x"],"bytes":"[Uint8Array: 3 bytes]"}'
	);
});

test("shared diagnostic ZIP preserves replay clocks and cast IDs, omitting private fields", async () => {
	const input = fixture(), before = JSON.stringify( input );
	const blob = await createDiagnosticUpload( input, OPTIONS );
	assert.equal( blob.type, "application/zip" );
	const { documents, methods } = await readZip( blob );
	assert.ok( methods.includes( 8 ), "native DEFLATE entries are decoded independently" );
	assert.equal( documents["manifest.json"].reportId, OPTIONS.id );
	assert.equal( documents["manifest.json"].videoTimelineOffsetSeconds, 3 );
	assert.equal( documents["movement.json"].simulationOriginMs, 80 );
	assert.equal( documents["movement.json"].events[0].token, 9 );
	assert.equal( documents["state.json"].gameplay.casts[0].caster, 7 );
	assert.equal( documents["timeline.json"].events.length, 1 );
	assert.ok( !JSON.stringify( documents ).includes( "PRIVATE_" ) );
	assert.equal( JSON.stringify( input ), before, "the full local archive is preserved" );
});

test("attachment budget retains the newest events and declares truncation", async () => {
	const input = fixture();
	input["movement.json"] = JSON.stringify( {
		mainTimeOriginMs: 100,
		events: Array.from( { length: 2000 }, ( _, revision ) => ({
			revision,
			event: "receipt",
			x: [ ...createHash( "sha256" ).update( String( revision ) ).digest() ]
		}) )
	} );

	const blob = await createDiagnosticUpload( input, { ...OPTIONS, maxBytes: 4096 } );
	assert.ok( blob.size <= 4096 );
	const { documents } = await readZip( blob );
	assert.equal( documents["movement.json"].events.at( -1 ).revision, 1999 );
	assert.ok( documents["manifest.json"].omissions.length > 0 );
});

test("unknown identity fields and free-form strings never enter shared technical documents", async () => {
	const input = fixture();
	input["movement.json"] = JSON.stringify( {
		localGid: 7,
		events: [ {
			kind: "movement",
			event: "receipt",
			reason: "correction",
			atMs: 100,
			sender: "PRIVATE_SENDER",
			nick: "PRIVATE_NICK",
			label: "PRIVATE_LABEL",
			from: "PRIVATE_CHAT",
			target: { PRIVATE_NAME: 7 },
			source: "PRIVATE_PATH",
			pose: { x: 1, y: 2, z: 3 },
			token: 123
		} ]
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	assert.ok( !JSON.stringify( documents ).includes( "PRIVATE_" ) );
	const event = documents["movement.json"].events[0];
	assert.equal( event.reason, "correction" );
	assert.equal( event.token, 123 );
	assert.deepEqual( event.pose, { x: 1, y: 2, z: 3 } );
});

test("a click keeps its control kind and slot, and a refusal its reason, but never a name", async () => {
	const { uiControl, refusalReason } = await import( "../../src/engine/runtime/bug-report/journal.ts" );
	const input = fixture();
	const click = id => ({ kind: "ui", t: 1, id, ...uiControl( id ), button: 0 });
	input["timeline.json"] = JSON.stringify( {
		events: [
			click( "slot:13" ),
			click( "shop-repair:GDR_STORE_BTN_REPAIR" ),
			click( "friend:PRIVATE_NAME" ),
			click( "PRIVATE_BARE" ),
			{ kind: "refusal", t: 2, refusal: refusalReason( "Error: Inventory is busy" ) },
			{ kind: "refusal", t: 3, refusal: refusalReason( "PRIVATE_FREE_TEXT" ) }
		]
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	assert.ok( !JSON.stringify( documents ).includes( "PRIVATE_" ) );
	const events = documents["timeline.json"].events;
	assert.deepEqual( events.slice( 0, 4 ).map( e => [ e.control, e.index ] ), [
		[ "slot", 13 ],
		[ "shop-repair", undefined ],
		[ "friend", undefined ],
		[ undefined, undefined ]
	] );
	assert.ok( events.every( e => e.id === undefined ), "the raw control id stays on the page" );
	assert.deepEqual( events.slice( 4 ).map( e => e.refusal ), [ "inventory-busy", "other" ] );
});

test("a report names the adapter's compression features and limits, never free text", async () => {
	const input = fixture();
	input["environment.json"] = JSON.stringify( {
		gpu: {
			features: [ "texture-compression-etc2", "texture-compression-astc", "PRIVATE_FEATURE" ],
			limits: { maxBufferSize: 268435456, maxStorageBufferBindingSize: 134217728, maxTextureDimension2D: 8192 },
			vendor: "apple",
			architecture: "PRIVATE Architecture",
			degradedPipelines: [ "particle-presentation", "PRIVATE_PIPELINE" ]
		}
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	assert.ok( !JSON.stringify( documents ).includes( "PRIVATE" ) );
	assert.deepEqual( documents["environment.json"].gpu, {
		features: [ "texture-compression-etc2", "texture-compression-astc" ],
		limits: { maxBufferSize: 268435456, maxStorageBufferBindingSize: 134217728, maxTextureDimension2D: 8192 },
		vendor: "apple",
		degradedPipelines: [ "particle-presentation" ]
	} );
});

test("a report marks bag and storage rows drawn with no icon, never their names or paths", async () => {
	// Threads 1559577119187673109 and 1559743886681972759: items "going
	// invisible" in the bag and in storage, and the report could not say
	// which slot had a row with nothing to draw.
	const input = fixture();
	input["state.json"] = JSON.stringify( {
		gameplay: {
			inventory: [
				{ slot: 13, refObjId: 61, quantity: 20, name: "PRIVATE_NAME", icon: "item/etc/PRIVATE.ddj" },
				{ slot: 14, refObjId: 24198, quantity: 1 }
			],
			storage: {
				npc: 17,
				phase: "open",
				capacity: 150,
				gold: "999",
				items: [ { slot: 31, refObjId: 3630, quantity: 10, name: "PRIVATE_NAME" } ]
			}
		}
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	assert.ok( !JSON.stringify( documents ).includes( "PRIVATE" ) );
	const gameplay = documents["state.json"].gameplay;
	assert.deepEqual( gameplay.inventory, [
		{ slot: 13, refObjId: 61, quantity: 20 },
		{ slot: 14, refObjId: 24198, quantity: 1, iconless: true }
	] );
	assert.deepEqual( gameplay.storage, {
		capacity: 150,
		items: [ { slot: 31, refObjId: 3630, quantity: 10, iconless: true } ]
	} );
});

test("a report keeps the inventory journal's slot rows", async () => {
	// The same threads: the journal says which slots each bag answer changed.
	const input = fixture();
	const journal = [ 7, 0xb06d, 1, 0, 13, 0, 7, 0xb06d, 1, 0, 20, 1 ];
	input["state.json"] = JSON.stringify( { gameplay: { inventory: [], inventoryJournal: journal } } );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	assert.deepEqual( documents["state.json"].gameplay.inventoryJournal, journal );
});

test("a browser without raw DEFLATE still sends a bounded stored ZIP", async t => {
	t.mock.method( globalThis, "CompressionStream", function unavailable() {
		throw Error( "unsupported" );
	} );
	const blob = await createDiagnosticUpload( fixture(), OPTIONS );
	const { documents, methods } = await readZip( blob );
	assert.ok( methods.every( method => method === 0 ) );
	assert.equal( documents["movement.json"].localGid, 7 );
});

test("an invalid attachment budget rejects instead of sending an oversized ZIP", async () => {
	for ( const maxBytes of [ 0, -1, NaN, Infinity, 4095 ] ) {
		await assert.rejects( createDiagnosticUpload( fixture(), { ...OPTIONS, maxBytes } ), /budget/ );
	}
});

test("a bulky state snapshot cannot discard the only movement receipt", async () => {
	const input = fixture();
	input["state.json"] = JSON.stringify( {
		gameplay: {
			inventory: Array.from( { length: 2000 }, ( _, id ) => ({
				id,
				value: [ ...createHash( "sha256" ).update( String( id ) ).digest() ]
			}) )
		}
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, { ...OPTIONS, maxBytes: 4096 } ) );
	assert.equal( documents["movement.json"].events.length, 1 );
	assert.equal( documents["movement.json"].events[0].token, 9 );
	assert.ok( documents["manifest.json"].omissions.some( text => text.includes( "inventory" ) ) );
});

test("highly compressible diagnostics respect the Agent expanded-byte limit too", async () => {
	const input = fixture();
	input["state.json"] = JSON.stringify( {
		gameplay: { inventory: [ { value: Array( 5 * 1024 * 1024 ).fill( 0 ) } ] }
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	const bytes = Object.values( documents ).reduce(
		( total, document ) => total + Buffer.byteLength( JSON.stringify( document ) + "\n" ),
		0
	);
	assert.ok( bytes <= 8 * 1024 * 1024 );
	assert.equal( documents["movement.json"].events[0].token, 9 );
	assert.ok( documents["manifest.json"].omissions.some( text => text.includes( "inventory" ) ) );
});

test("the shared network.json keeps only time, direction, opcode and size", async () => {
	const files = {
		...fixture(),
		"network.json": JSON.stringify( {
			note: "rows",
			events: [ [ 1.25, "in", "0xB070", 40 ], [ 1.5, "out", "0x7074", 12, "EXTRA" ], [
				2,
				"sideways",
				"0x7074",
				1
			], {
				t: 3,
				text: "PRIVATE_CHAT"
			} ]
		} )
	};
	const { documents } = await readZip( await createDiagnosticUpload( files, OPTIONS ) );
	assert.deepEqual( documents["network.json"].events, [ [ 1.25, "in", "0xB070", 40 ] ] );
	assert.doesNotMatch( JSON.stringify( documents ), /PRIVATE_CHAT|EXTRA/ );
});

test("every closed movement refusal code survives receipt error, sample gameplayError and refusal sanitization", async () => {
	const { movementErrorCodes } = await import( "../../src/engine/contracts/movement-diagnostic.ts" );
	const { MOVEMENT_ERROR_CODES, refusalReason } = await import( "../../src/engine/runtime/bug-report/journal.ts" );
	assert.deepEqual( MOVEMENT_ERROR_CODES, movementErrorCodes() );
	assert.equal( MOVEMENT_ERROR_CODES.length, 22 );
	const input = fixture();
	input["movement.json"] = JSON.stringify( {
		localGid: 7,
		events: [
			{
				kind: "movement",
				event: "request",
				atMs: 90,
				requestId: 1,
				mode: 1,
				cos: true,
				acknowledged: 0
			},
			...MOVEMENT_ERROR_CODES.map( ( code, index ) => ({
				kind: "movement",
				event: "receipt",
				atMs: 100 + index,
				requestId: index + 1,
				acknowledged: index,
				accepted: false,
				stale: false,
				error: code
			}) ),
			{
				kind: "movement",
				event: "receipt",
				atMs: 999,
				requestId: 999,
				acknowledged: 998,
				accepted: false,
				stale: false,
				error: "PRIVATE_ERROR_COORDINATES"
			}
		]
	} );
	input["timeline.json"] = JSON.stringify( {
		events: [
			...MOVEMENT_ERROR_CODES.map( ( code, index ) => ({
				kind: "sample",
				t: index,
				gameplayError: code
			}) ),
			{
				kind: "sample",
				t: 999,
				gameplayError: "PRIVATE_GAMEPLAY_ERROR"
			},
			...MOVEMENT_ERROR_CODES.map( ( code, index ) => ({
				kind: "refusal",
				t: index,
				refusal: refusalReason( code )
			}) )
		]
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	assert.doesNotMatch( JSON.stringify( documents ), /PRIVATE_/ );
	const movementEvents = documents["movement.json"].events;
	assert.deepEqual( movementEvents[0], {
		kind: "movement",
		event: "request",
		atMs: 90,
		requestId: 1,
		mode: 1,
		cos: true,
		acknowledged: 0
	} );
	assert.deepEqual(
		movementEvents.slice( 1, 1 + MOVEMENT_ERROR_CODES.length ).map( event => event.error ),
		[ ...MOVEMENT_ERROR_CODES ]
	);
	assert.equal( movementEvents.at( -1 ).error, undefined );
	const timelineEvents = documents["timeline.json"].events;
	assert.deepEqual(
		timelineEvents.slice( 0, MOVEMENT_ERROR_CODES.length ).map( event => event.gameplayError ),
		[ ...MOVEMENT_ERROR_CODES ]
	);
	assert.equal( timelineEvents[MOVEMENT_ERROR_CODES.length].gameplayError, undefined );
	assert.deepEqual(
		timelineEvents.slice( MOVEMENT_ERROR_CODES.length + 1 ).map( event => event.refusal ),
		MOVEMENT_ERROR_CODES.map( code => code === "Movement rejected" ? "movement-rejected" : code )
	);
});

/*
================
assetSample
================
*/
function assetSample( t, jobs ) {
	return { t, phase: "character-select", queue: { phase: "running", error: null, available: 0, jobs } };
}

test("assets.json reaches the uploaded ZIP, rebuilt from typed fields alone (#1975)", async () => {
	const input = fixture();
	input["assets.json"] = JSON.stringify( {
		note: "n",
		current: assetSample( 9, [] ),
		samples: [
			assetSample( 1, [ {
				id: 4,
				path: "/assets/world/region.bundle",
				decode: "frontend-world",
				ageMs: 30000,
				state: "cancelling",
				result: null,
				extra: "PRIVATE_EXTRA"
			}, {
				id: 5,
				path: "https://x/PRIVATE_URL?token=PRIVATE_TOKEN",
				decode: "bytes",
				ageMs: 1,
				state: "loading",
				result: null
			} ] ),
			{ t: 2, phase: "PRIVATE PHASE", queue: { phase: "failed", error: "PRIVATE_ERROR", available: 3, jobs: [] } }
		]
	} );
	const { documents } = await readZip( await createDiagnosticUpload( input, OPTIONS ) );
	const assets = documents["assets.json"];
	assert.ok( assets, "the member is in the shared ZIP" );
	assert.doesNotMatch( JSON.stringify( assets ), /PRIVATE_/ );
	assert.deepEqual( assets.samples[0].queue.jobs[0], {
		id: 4,
		path: "/assets/world/region.bundle",
		decode: "frontend-world",
		ageMs: 30000,
		state: "cancelling",
		result: null
	} );
	assert.equal( assets.samples[0].queue.jobs[1].path, null );
	assert.equal( assets.samples[1].phase, null );
	assert.equal( assets.samples[1].queue.failed, true );
	assert.equal( assets.current.t, 9 );
});

test("an oversized asset history is trimmed oldest first, never refused", async () => {
	const input = fixture();
	const job = ( id ) => ({
		id,
		path: `/assets/packs/${createHash( "sha256" ).update( String( id ) ).digest( "hex" )}.bin`,
		decode: "bytes",
		ageMs: id,
		state: "loading",
		result: null
	});
	input["assets.json"] = JSON.stringify( {
		samples: Array.from(
			{ length: 72 },
			( _, i ) => assetSample( i, Array.from( { length: 24 }, ( _, j ) => job( i * 24 + j ) ) )
		)
	} );
	const blob = await createDiagnosticUpload( input, { ...OPTIONS, maxBytes: 8192 } );
	assert.ok( blob.size <= 8192 );
	const { documents } = await readZip( blob );
	const samples = documents["assets.json"].samples;
	assert.ok( samples.length > 0 && samples.length < 72 );
	assert.equal( samples.at( -1 ).t, 71, "the newest sample is kept" );
	assert.ok( documents["manifest.json"].omissions.some( line => line.startsWith( "assets.json: dropped" ) ) );
});
