/*
===========================================================================

fontActions.test.mjs - the packaged font repairs replay from the action store

Temp trees stand in for the extracted fonts and the public tree (no game
data, no fontTools: the repair is injected). A warm run repairs nothing
and still claims every served font; a changed source byte or a new
fontTools repairs again; a touched source does not; a deleted served font
comes back from the store without a repair.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

// The public tree is resolved when the build modules load: point it at a
// temp tree first, so claims name /assets/fonts/... paths under it.
const scratch = await mkdtemp( path.join( os.tmpdir(), "sro-font-actions-" ) );
process.env.SRO_GENERATED_ROOT = path.join( scratch, "generated" );
const { PACKAGED_FONTS, repairPackagedFonts } = await import( "../../build/shared/fontResources.mjs" );
const { openStore } = await import( "../../build/graph/store.mjs" );
const { openFileHashCache } = await import( "../../build/shared/fileHashCache.mjs" );
const { abandonPublication, beginPublication, recordClaims } = await import(
	"../../build/shared/publicationLedger.mjs"
);
const { fontPublicRoot } = await import( "../../build/shared/resourceIo.mjs" );

test.after( () => rm( scratch, { recursive: true, force: true } ) );

/*
================
fixture

Source fonts in a temp folder, the real public font folder of the temp
generated root, a store of the test's own. repairs counts the repairs;
the fake repair writes the source bytes reversed.
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( scratch, "case-" ) );
	const roots = { source: path.join( root, "fonts-src" ), public: fontPublicRoot };
	await rm( roots.public, { recursive: true, force: true } );
	await mkdir( roots.source, { recursive: true } );
	await mkdir( roots.public, { recursive: true } );
	for ( const font of PACKAGED_FONTS ) await writeFile( path.join( roots.source, font.sourceFile ), font.role );
	const store = await openStore( {
		root: path.join( root, "store" ),
		hashCache: await openFileHashCache( path.join( root, "hash-cache.json" ) )
	} );
	t.after( () => store.shutdown() );
	const repairs = [];
	const run = ( toolchain = "fontTools 4.63.0" ) =>
		repairPackagedFonts( roots, {
			store,
			toolchain,
			repair: async ( source, target ) => {
				repairs.push( path.basename( target ) );
				await writeFile( target, Buffer.from( await readFile( source ) ).reverse() );
			}
		} );
	return { roots, repairs, run };
}

test("a warm run repairs nothing and still claims every served font", async t => {
	const f = await fixture( t );
	await f.run();
	assert.equal( f.repairs.length, PACKAGED_FONTS.length );
	f.repairs.length = 0;
	const claims = new Set();
	beginPublication( "font-actions-test" );
	try {
		await recordClaims( claims, () => f.run() );
	} finally {
		abandonPublication();
	}
	assert.deepEqual( f.repairs, [] );
	assert.deepEqual( [ ...claims ].sort(), PACKAGED_FONTS.map( font => `/assets/fonts/${font.publicFile}` ).sort() );
});

test("a changed source or a new fontTools repairs again; a touched source does not", async t => {
	const f = await fixture( t );
	await f.run();
	f.repairs.length = 0;
	const [first] = PACKAGED_FONTS;
	const source = path.join( f.roots.source, first.sourceFile );
	const later = new Date( Date.now() + 60_000 );
	await utimes( source, later, later );
	await f.run();
	assert.deepEqual( f.repairs, [], "a touch keeps the key" );
	await writeFile( source, "changed" );
	await f.run();
	assert.deepEqual( f.repairs, [ first.publicFile ] );
	assert.equal( await readFile( path.join( f.roots.public, first.publicFile ), "utf8" ), "degnahc" );
	f.repairs.length = 0;
	await f.run( "fontTools 4.64.0" );
	assert.equal( f.repairs.length, PACKAGED_FONTS.length );
});

test("a deleted served font comes back from the store without a repair", async t => {
	const f = await fixture( t );
	await f.run();
	f.repairs.length = 0;
	const [first] = PACKAGED_FONTS;
	const served = path.join( f.roots.public, first.publicFile );
	const bytes = await readFile( served );
	await rm( served );
	await f.run();
	assert.deepEqual( f.repairs, [] );
	assert.deepEqual( await readFile( served ), bytes );
});
