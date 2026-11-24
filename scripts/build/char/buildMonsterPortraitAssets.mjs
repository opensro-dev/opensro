/*
===========================================================================

buildMonsterPortraitAssets.mjs - the published monster stills' single owner

Port-only, not native. The hunting guide and website catalogue share this
asset family's content-addressed manifest: all monster references, including
uniques and aliases, as 384px transparent PNGs. The production viewer owns
framing, lighting, material selection and readiness; this family owns the
cache and publication ledger. Licensed images never enter source control.

A published still is named by its own PNG bytes, so an unchanged portrait
keeps its name (and its pack) across builds whose other inputs moved
(#3104). The renderer fingerprint below only decides whether a still must
be drawn again; the drawn bytes live in a cache outside the public tree.
The catalogue's switch to this manifest and retirement of its old publishing
stage are a maintainer follow-up after this family lands.

A machine without a usable GPU cannot draw stills at all (#4015). The
viewer therefore starts lazily, on the first still the cache cannot
serve, and when it cannot draw - the browser will not launch, or the
first draw fails before any still exists - the family falls back to the
stills the previous published manifest carries and says so in its note,
instead of stopping the full build. A draw that fails after a still was
drawn still names its row and stops the family: the viewer works, and
the model is broken. Fallback bytes never enter the fingerprint cache;
they were drawn by an older renderer.

===========================================================================
*/
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../../lib/probeBrowser.mjs";
import { generatedRoot, publicRoot } from "../world/paths.mjs";
import { npcManifestModels } from "../shared/npcManifest.mjs";
import { containedPublicFile } from "../shared/assetPaths.mjs";
import { writeIntoPublicTree } from "../shared/publicWrite.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { clientBuildDefinitions } from "../../../apps/client-next/tools/build-metadata.mjs";
import { publishedAssets } from "../../../apps/client-next/tools/published-assets.mjs";
import { compareText } from "../shared/textOrder.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import {
	EFFECT_PROGRAM_INDEX,
	effectProgramIndexFiles
} from "../../../apps/client-next/src/engine/foundation/assets/effect-programs.ts";

const PREFIX = "/assets/npc/hunting-portraits/";
// Drawn stills wait here between builds, outside the public tree: only the
// content-named publication below is ever claimed or packed.
const CACHE_ROOT = path.join( generatedRoot, "hunting-portrait-cache" );
const CLIENT = path.resolve( import.meta.dirname, "../../../apps/client-next" );
export const MONSTER_PORTRAIT_SIZE = 384;
const STILL_TIMEOUT_MS = 45000;
const PNG_DATA_URL = /^data:image\/png;base64,([A-Za-z0-9+/=]+)$/;
const VIEWER_MANIFESTS = [
	"assets/char/roster.json",
	"assets/npc/manifest.json",
	"assets/data/missionPresentation.json",
	"assets/audio/effectsound.json",
	"assets/anim/manifest.json",
	"assets/itemdrop/manifest.json",
	"assets/data/skillAudioData.json",
	"assets/data/characterActionData.json"
];

/*
================
monsterPortraitReferences

Select every authored monster, not only the guide's ordinary outdoor set.
The ordinary BMT slot matches the viewer's normal-grade material selection.
================
*/
export function monsterPortraitReferences( manifest ) {
	return Object.values( npcManifestModels( manifest ) ).filter( row =>
		row.kind === "monster" && Number.isInteger( row.refObjId ) && row.refObjId > 0
	).sort( ( a, b ) => a.refObjId - b.refObjId ).map( row => {
		const slot = [ 1, 3, 4 ].includes( row.materialKind ) ? row.materialKind : 0;
		return {
			refObjId: row.refObjId,
			// Named only when the manifest names the row: an undefined
			// key would change the reference's shape for every consumer.
			...(row.codename ? { codename: row.codename } : {}),
			glb: row.materialVariants?.[String( slot )] ?? row.glb
		};
	} );
}

/*
================
monsterPortraitCacheKey

What decides a still is drawn again: the renderer fingerprint and the
reference. The key names only the cache file, never a publication.
================
*/
export function monsterPortraitCacheKey( fingerprint, refObjId ) {
	return createHash( "sha256" ).update( fingerprint ).update( String( refObjId ) ).digest( "hex" );
}

/*
================
monsterPortraitPublishedPath

The public name of a still: the SHA-256 of its PNG bytes. Same bytes,
same name, whatever else the build changed; a redrawn still that comes
out byte-identical keeps the name its players already hold (#3104).
================
*/
export function monsterPortraitPublishedPath( bytes ) {
	return PREFIX + sha256Hex( bytes ) + ".png";
}

/*
================
readPortraitAsset

Resolve published dependencies through the shared containment boundary.
================
*/
async function readPortraitAsset( publicPath ) {
	return readFile( containedPublicFile( publicRoot, publicPath ) );
}

/*
================
monsterPortraitRendererFingerprint

The viewer's owners and presentation inputs invalidate cached stills. Include
the NPC resources too: a riding unique's still depends on its mount's bytes.
Ambient effects are drawn by the viewer; both their programs and referenced
texture bytes must invalidate a still, even when no model changed.
================
*/
export async function monsterPortraitRendererFingerprint( manifest, readAsset = readPortraitAsset ) {
	const hash = createHash( "sha256" );
	/*
	================
	visit
	================
	*/
	async function visit( directory ) {
		for (
			const entry of (await readdir( directory, { withFileTypes: true } )).sort( ( a, b ) =>
				compareText( a.name, b.name )
			)
		) {
			const file = path.join( directory, entry.name );
			if ( entry.isDirectory() ) await visit( file );
			else hash.update( path.relative( CLIENT, file ) ).update( await readFile( file ) );
		}
	}
	await visit( path.join( CLIENT, "src/engine" ) );
	for ( const file of [ "src/bootstrap.ts", "viewer.html" ] ) {
		hash.update( await readFile( path.join( CLIENT, file ) ) );
	}
	for ( const file of VIEWER_MANIFESTS ) hash.update( await readAsset( "/" + file ) );
	// The effect programs are one file per effect and per mesh (#3951).
	const indexBytes = await readAsset( EFFECT_PROGRAM_INDEX );
	hash.update( EFFECT_PROGRAM_INDEX ).update( indexBytes );
	const effects = JSON.parse( indexBytes.toString( "utf8" ) );
	for ( const file of effectProgramIndexFiles( effects ) ) hash.update( file ).update( await readAsset( file ) );
	for ( const texture of [ ...new Set( Object.values( effects.textures ?? {} ) ) ].sort() ) {
		hash.update( texture ).update( await readAsset( texture ) );
	}
	const models = new Set();
	for ( const row of Object.values( npcManifestModels( manifest ) ) ) {
		if ( row.glb ) models.add( row.glb );
		for ( const glb of Object.values( row.materialVariants ?? {} ) ) models.add( glb );
	}
	for ( const glb of [ ...models ].sort() ) {
		hash.update( glb ).update( await readAsset( glb ) );
	}
	hash.update( await readFile( import.meta.filename ) );
	return hash.digest( "hex" );
}

/*
================
monsterPortraitFailure

The error a failed still leaves behind: its row, the model it was
drawing and the viewer's own reason. A batch death that only says
"asset-load" cannot be reproduced, because it does not say which of
the 1919 monsters stopped the build.
================
*/
function monsterPortraitFailure( refObjId, row, error ) {
	const message = error instanceof Error ? error.message : String( error );
	const who = row?.codename ? `${refObjId} (${row.codename})` : String( refObjId );
	const what = row?.glb ? ` on ${row.glb}` : "";
	return Error( `Monster portrait ${who} failed${what}: ${message}`, { cause: error } );
}

/*
================
renderMonsterPortrait

Keep one viewer page and renderer for the batch. Reload after a failed
draw, and name the row that failed.
================
*/
export async function renderMonsterPortrait( page, base, refObjId, row ) {
	try {
		let id = refObjId;
		if ( !(await page.evaluate( () => typeof window.__viewerNext === "function" )) ) {
			const url = new URL( "viewer.html", base );
			url.search = new URLSearchParams( {
				monster: String( refObjId ),
				still: "1",
				batch: "1",
				size: String( MONSTER_PORTRAIT_SIZE )
			} ).toString();
			await page.goto( url.href );
			await page.waitForFunction( () => !!window.__viewerStill );
			id = null;
		}
		const png = await page.evaluate( async ( { id, timeout } ) => {
			let timer;
			try {
				const still = id === null ? window.__viewerStill : window.__viewerNext( id );
				return await Promise.race( [
					still,
					new Promise( ( _, reject ) => {
						timer = setTimeout( () => reject( Error( "Monster portrait timed out" ) ), timeout );
					} )
				] );
			} finally {
				clearTimeout( timer );
			}
		}, { id, timeout: STILL_TIMEOUT_MS } );
		const match = PNG_DATA_URL.exec( png ?? "" );
		if ( !match ) throw Error( `Monster ${refObjId} did not return a PNG still` );
		return Buffer.from( match[1], "base64" );
	} catch ( error ) {
		await page.goto( "about:blank" );
		throw monsterPortraitFailure( refObjId, row, error );
	}
}

/*
================
portraitManifestIndex

The stills a previous build published, refObjId -> public path, read
from its manifest rows. Malformed rows are skipped: the fallback must
never invent a name the family did not publish.
================
*/
export function portraitManifestIndex( manifest ) {
	const index = new Map();
	for ( const row of manifest?.rows ?? [] ) {
		if ( !Array.isArray( row ) ) continue;
		const [refObjId, published] = row;
		if (
			Number.isInteger( refObjId ) && refObjId > 0 && typeof published === "string" &&
			published.startsWith( PREFIX ) && published.endsWith( ".png" ) && !index.has( refObjId )
		) {
			index.set( refObjId, published );
		}
	}
	return index;
}

/*
================
portraitRowDrawable

A reference the viewer can be asked to draw: a model under the NPC tree.
Rows without one are missing models, not failures.
================
*/
function portraitRowDrawable( row ) {
	return typeof row.glb === "string" && /^\/assets\/npc\/[a-zA-Z0-9/_.-]+\.glb$/.test( row.glb ) &&
		!row.glb.includes( ".." );
}

/*
================
collectPortraitRows

Where each reference's still comes from, in order: the fingerprint
cache, the viewer, or - only when the viewer cannot draw at all - the
still the previous manifest published. ports.cached and ports.draw
answer PNG bytes or null; ports.publishedBytes answers the published
still's bytes or null. The first draw failure before any still is drawn
means the environment cannot draw (#4015): it is kept in `down` and no
later row asks the viewer again. A draw failure after a drawn still is
the viewer's named error and propagates.
================
*/
export async function collectPortraitRows( references, ports ) {
	const outcomes = [], missing = [];
	let drawn = 0, reused = 0, down = null;
	for ( const row of references ) {
		if ( !portraitRowDrawable( row ) ) {
			missing.push( row.refObjId );
			continue;
		}
		let bytes = await ports.cached( row ), source = bytes ? "cache" : null;
		if ( !bytes && !down ) {
			try {
				bytes = await ports.draw( row );
				source = "drawn";
				drawn++;
			} catch ( error ) {
				if ( drawn > 0 ) throw error;
				down = error;
			}
		}
		if ( !bytes && down ) {
			bytes = await ports.publishedBytes( row );
			if ( bytes ) {
				source = "published";
				reused++;
			}
		}
		if ( !bytes ) {
			missing.push( row.refObjId );
			continue;
		}
		outcomes.push( { refObjId: row.refObjId, bytes, source } );
	}
	return { outcomes, missing, drawn, reused, down };
}

/*
================
buildMonsterPortraitAssets
================
*/
export async function buildMonsterPortraitAssets() {
	const manifestData = JSON.parse( await readFile( path.join( publicRoot, "assets/npc/manifest.json" ), "utf8" ) );
	const references = monsterPortraitReferences( manifestData );
	const fingerprint = await monsterPortraitRendererFingerprint( manifestData ), rows = [], files = [];
	const publishedIndex = await readFile( path.join( publicRoot, (PREFIX + "manifest.json").slice( 1 ) ), "utf8" )
		.then( text => portraitManifestIndex( JSON.parse( text ) ) ).catch( () => new Map() );
	// The viewer starts on the first still the cache cannot serve, so a
	// fully cached build never needs a browser - or a GPU - at all.
	let viewer = null;
	/*
	================
	startViewer

	The Vite server and probe browser behind ports.draw, started once.
	================
	*/
	async function startViewer() {
		const require = createRequire( path.join( CLIENT, "package.json" ) );
		const { createServer } = await import( pathToFileURL( require.resolve( "vite" ) ).href );
		const server = await createServer( {
			configFile: false,
			root: CLIENT,
			publicDir: false,
			logLevel: "error",
			define: clientBuildDefinitions( CLIENT ),
			resolve: { alias: { "@": path.join( CLIENT, "src" ) } },
			server: { host: "127.0.0.1", port: 0 },
			plugins: [ publishedAssets() ]
		} );
		await server.listen();
		const launched = await launchProbeBrowser( {
			viewport: { width: MONSTER_PORTRAIT_SIZE, height: MONSTER_PORTRAIT_SIZE }
		} );
		return {
			server,
			browser: launched.browser,
			page: launched.page,
			base: `http://127.0.0.1:${server.httpServer.address().port}/`
		};
	}
	try {
		const collected = await collectPortraitRows( references, {
			async cached( row ) {
				const cacheFile = path.join(
					CACHE_ROOT,
					monsterPortraitCacheKey( fingerprint, row.refObjId ) + ".png"
				);
				return await stat( cacheFile ).then( value => value.size > 100 ).catch( () => false ) ?
					await readFile( cacheFile ) :
					null;
			},
			async draw( row ) {
				viewer ??= await startViewer();
				// Ref ids have their own presentation (rides, scale, effects). Never
				// collapse them solely because they alias the same base GLB.
				const bytes = await renderMonsterPortrait( viewer.page, viewer.base, row.refObjId, row );
				const cacheFile = path.join(
					CACHE_ROOT,
					monsterPortraitCacheKey( fingerprint, row.refObjId ) + ".png"
				);
				await mkdir( CACHE_ROOT, { recursive: true } );
				await writeFile( cacheFile, bytes );
				return bytes;
			},
			async publishedBytes( row ) {
				const published = publishedIndex.get( row.refObjId );
				if ( !published ) return null;
				const file = containedPublicFile( publicRoot, published );
				return await stat( file ).then( value => value.size > 100 ).catch( () => false ) ?
					await readFile( file ) :
					null;
			}
		} );
		for ( const outcome of collected.outcomes ) {
			const output = monsterPortraitPublishedPath( outcome.bytes ),
				target = path.join( publicRoot, output.slice( 1 ) );
			if ( await stat( target ).then( value => value.size > 100 ).catch( () => false ) ) {
				claimPublicFile( target );
			} else await writeIntoPublicTree( target, outcome.bytes );
			rows.push( [ outcome.refObjId, output ] );
			files.push( output );
		}
		const missing = collected.missing;
		const manifest = path.join( publicRoot, (PREFIX + "manifest.json").slice( 1 ) );
		await writeJsonIfChanged( manifest, {
			format: "sro-hunting-portraits",
			version: 1,
			image: {
				mime: "image/png",
				width: MONSTER_PORTRAIT_SIZE,
				height: MONSTER_PORTRAIT_SIZE,
				transparent: true
			},
			rows,
			missing
		} );
		await refreshPrecompressedSidecars( [ manifest ], { onlyWhenStale: true } );
		files.push( PREFIX + "manifest.json.gz" );
		const note = `${rows.length} native references (${collected.drawn} rendered), ${missing.length} missing models`;
		return {
			files,
			note: collected.down ?
				`${note}; the viewer could not draw (${collected.down.message}), ` +
				`${collected.reused} stills reused from the published set` :
				note
		};
	} finally {
		await viewer?.browser?.close();
		await viewer?.server.close();
	}
}
