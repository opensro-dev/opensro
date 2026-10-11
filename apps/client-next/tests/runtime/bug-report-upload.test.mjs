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
const { createDiagnosticUpload, diagnosticUploadBudget } = await import(
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
