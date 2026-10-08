/*
===========================================================================
generatedArtifactArchive.test.mjs - archive across publication volumes safely
===========================================================================
*/
import assert from "node:assert/strict";
import * as files from "node:fs/promises";
import { constants } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { archiveGeneratedArtifact } from "../../build/artifacts/generatedArtifactArchive.mjs";

/*
================
fixture
================
*/
async function fixture( t ) {
	const root = await files.mkdtemp( path.join( os.tmpdir(), "sro-archive-volumes-" ) );
	t.after( () => files.rm( root, { recursive: true, force: true } ) );
	const source = path.join( root, "live", "delivery.json" );
	await files.mkdir( path.dirname( source ) );
	const payload = Buffer.alloc( 1024 * 1024 + 137, 0x59 );
	await files.writeFile( source, payload );
	const options = {
		scopeRoot: path.dirname( source ),
		archiveRoot: path.join( root, "archive" ),
		files: {
			async link( from, to ) {
				if ( from === source ) throw Object.assign( new Error( "different volume" ), { code: "EXDEV" } );
				return files.link( from, to );
			}
		}
	};
	return { source, payload, options };
}

test("cross-device copy is exclusive, verified and recorded before source removal", async t => {
	const f = await fixture( t );
	let copied = false;
	f.options.files.copyFile = async ( source, destination, flags ) => {
		assert.equal( flags, constants.COPYFILE_EXCL );
		await files.copyFile( source, destination, flags );
		assert.deepEqual( await files.readFile( f.source ), f.payload );
		copied = true;
	};
	const result = await archiveGeneratedArtifact( f.source, f.options );
	assert.equal( copied, true );
	assert.deepEqual( await files.readFile( result.destination ), f.payload );
	assert.equal(
		JSON.parse( await files.readFile( `${result.destination}.archive.json`, "utf8" ) ).bytes,
		f.payload.length
	);
	await assert.rejects( files.stat( f.source ), { code: "ENOENT" } );
});

for ( const failure of [ "copy", "verification", "provenance" ] ) {
	test(`failed ${failure} keeps the original live artifact`, async t => {
		const f = await fixture( t );
		if ( failure === "provenance" ) {
			f.options.files.writeFile = async () => {
				throw new Error( "provenance failure" );
			};
		} else {
			f.options.files.copyFile = async ( source, destination, flags ) => {
				await files.copyFile( source, destination, flags );
				if ( failure === "copy" ) throw new Error( "copy failure" );
				const corrupted = Buffer.from( f.payload );
				corrupted[corrupted.length - 1] ^= 1;
				await files.writeFile( destination, corrupted );
			};
		}
		await assert.rejects( archiveGeneratedArtifact( f.source, f.options ), /failure|verification failed/ );
		assert.deepEqual( await files.readFile( f.source ), f.payload );
	});
}

test("a destination created during publication is never overwritten", async t => {
	const f = await fixture( t );
	const originalLink = f.options.files.link;
	let reserved;
	f.options.files.link = async ( source, destination ) => {
		if ( source !== f.source && !reserved ) {
			reserved = destination;
			await files.writeFile( destination, "existing archive", { flag: "wx" } );
		}
		return originalLink( source, destination );
	};
	const archived = await archiveGeneratedArtifact( f.source, f.options );
	assert.notEqual( archived.destination, reserved );
	assert.equal( await files.readFile( reserved, "utf8" ), "existing archive" );
	assert.deepEqual( await files.readFile( archived.destination ), f.payload );
});

test("a provenance collision reserves its name without losing either artifact", async t => {
	const f = await fixture( t );
	let existing;
	f.options.files.writeFile = async ( filename, data, options ) => {
		if ( !existing ) {
			existing = filename;
			await files.writeFile( filename, "existing record", { flag: "wx" } );
		}
		return files.writeFile( filename, data, options );
	};
	const archived = await archiveGeneratedArtifact( f.source, f.options );
	assert.notEqual( `${archived.destination}.archive.json`, existing );
	assert.equal( await files.readFile( existing, "utf8" ), "existing record" );
	assert.deepEqual( await files.readFile( archived.destination ), f.payload );
});

test("a source replaced after staging survives the final identity check", async t => {
	const f = await fixture( t );
	const replacement = Buffer.from( "new live publication" );
	f.options.files.writeFile = async ( filename, data, options ) => {
		await files.writeFile( filename, data, options );
		// Provenance publication occurs after the staged copy was verified.
		await files.writeFile( f.source, replacement );
	};
	await assert.rejects( archiveGeneratedArtifact( f.source, f.options ), /source changed/ );
	assert.deepEqual( await files.readFile( f.source ), replacement );
});

test("an archive path past 260 characters is staged and recorded", async t => {
	const f = await fixture( t );
	// The superseded pack slots nest deep enough to pass MAX_PATH once the
	// dated archive root prefixes them (mkdtemp failed there on Windows).
	const deep = path.join( path.dirname( f.source ), "a".repeat( 120 ), "b".repeat( 120 ), "slot.pack" );
	await files.mkdir( path.dirname( deep ), { recursive: true } );
	await files.writeFile( deep, f.payload );
	const result = await archiveGeneratedArtifact( deep, { ...f.options, files: {} } );
	assert.ok( result.destination.length > 260, String( result.destination.length ) );
	assert.deepEqual( await files.readFile( result.destination ), f.payload );
	await assert.rejects( files.stat( deep ), { code: "ENOENT" } );
});

test("a staging name already taken is retried, never shared", async t => {
	const f = await fixture( t );
	const taken = [];
	f.options.files.mkdir = async ( directory, options ) => {
		if ( !options && path.basename( directory ).startsWith( ".archive-" ) && !taken.length ) {
			taken.push( directory );
			await files.mkdir( directory );
			throw Object.assign( new Error( "exists" ), { code: "EEXIST" } );
		}
		return files.mkdir( directory, options );
	};
	const result = await archiveGeneratedArtifact( f.source, f.options );
	assert.deepEqual( await files.readFile( result.destination ), f.payload );
	// The foreign directory is left alone; only the archive's own staging is removed.
	assert.equal( (await files.stat( taken[0] )).isDirectory(), true );
});
