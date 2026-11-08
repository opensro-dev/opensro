/*
===========================================================================

itemIconSubstitution.test.mjs - a referenced icon that shipped in no client
publishes its named substitute instead of the NO IMAGE plate (#2790)

Run the real icon publisher against an isolated generated tree whose
converted images hold only the substitute's art: the referenced name is
published with the substitute's bytes, an icon with no shipped art and no
substitute stays absent, and a shipped icon publishes as itself.

===========================================================================
*/
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import test from "node:test";

import { imagePublicPath } from "../../build/shared/cifResources.mjs";

const run = promisify( execFile );
const CIF_RESOURCES = fileURLToPath( new URL( "../../build/shared/cifResources.mjs", import.meta.url ) );
// The dangling A-grade elixir reference, its named substitute (ITEM_ICON_SUBSTITUTES)
// and a missing icon nobody named a substitute for.
const MISSING = "icon/item/etc/archemy_reinforce_recipe_a.ddj";
const SUBSTITUTE = "icon/item/etc/archemy_reinforce_recipe_b.ddj";
const UNNAMED = "icon/item/etc/archemy_reinforce_recipe_never_shipped.ddj";
const SENTINEL = Buffer.from( "substitute icon bytes" );

/*
================
publicFile

The published file of one ddj path under an isolated generated root.
================
*/
function publicFile( root, ddjPath ) {
	return path.join( root, "client-public", imagePublicPath( ddjPath ).slice( 1 ) );
}

/*
================
seedConverted

The converted PNG of one ddj path in the isolated tree's image source.
================
*/
async function seedConverted( root, ddjPath, bytes ) {
	const file = path.join(
		root,
		"intermediate",
		"images",
		imagePublicPath( ddjPath ).replace( /^\/assets\/images\//, "" )
	);
	await mkdir( path.dirname( file ), { recursive: true } );
	await writeFile( file, bytes );
}

test("the A-grade elixir icon publishes its B-grade substitute under the referenced name", async ( t ) => {
	const temporary = await mkdtemp( path.join( os.tmpdir(), "sro-icon-substitution-" ) );
	t.after( async () => {
		assert.equal( path.dirname( temporary ), path.resolve( os.tmpdir() ) );
		await rm( temporary, { recursive: true, force: true } );
	} );
	await seedConverted( temporary, SUBSTITUTE, SENTINEL );
	// The publisher binds its roots at import, so it runs as a child with the
	// isolated generated root, exactly like the focused asset publishers.
	const driver = [
		`const { copyItemDataIconImages } = await import( ${JSON.stringify( pathToFileURL( CIF_RESOURCES ).href )} );`,
		"const copied = new Set();",
		`await copyItemDataIconImages( ${JSON.stringify( [ MISSING, UNNAMED, SUBSTITUTE ] )}, copied );`,
		"console.log( JSON.stringify( [ ...copied ].sort() ) );"
	].join( "\n" );
	const { stdout } = await run( process.execPath, [ "--input-type", "module", "-e", driver ], {
		env: { ...process.env, SRO_GENERATED_ROOT: temporary },
		timeout: 120000,
		windowsHide: true
	} );
	const copied = JSON.parse( stdout );
	assert.ok( copied.includes( imagePublicPath( MISSING ) ), "the referenced name is published" );
	assert.deepEqual( await readFile( publicFile( temporary, MISSING ) ), SENTINEL );
	assert.deepEqual( await readFile( publicFile( temporary, SUBSTITUTE ) ), SENTINEL );
	assert.ok( !copied.includes( imagePublicPath( UNNAMED ) ), "no substitute, no publication" );
	await assert.rejects( stat( publicFile( temporary, UNNAMED ) ) );
});
