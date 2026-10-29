/*
===========================================================================

buildMonsterPortraitAssets.mjs - the published monster stills' single owner

Port-only, not native. The hunting guide and website catalogue share this
asset family's content-addressed manifest: all monster references, including
uniques and aliases, as 384px transparent PNGs. The production viewer owns
framing, lighting, material selection and readiness; this family owns the
cache and publication ledger. Licensed images never enter source control.
The catalogue's switch to this manifest and retirement of its old publishing
stage are a maintainer follow-up after this family lands.

===========================================================================
*/
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { readFile, readdir, stat } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../../lib/probeBrowser.mjs";
import { publicRoot } from "../world/paths.mjs";
import { npcManifestModels } from "../shared/npcManifest.mjs";
import { containedPublicFile } from "../shared/assetPaths.mjs";
import { writeIntoPublicTree } from "../shared/publicWrite.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { clientBuildDefinitions } from "../../../apps/client-next/tools/build-metadata.mjs";
import { publishedAssets } from "../../../apps/client-next/tools/published-assets.mjs";

const PREFIX = "/assets/npc/hunting-portraits/";
const CLIENT = path.resolve( import.meta.dirname, "../../../apps/client-next" );
export const MONSTER_PORTRAIT_SIZE = 384;
const STILL_TIMEOUT_MS = 45000;
const PNG_DATA_URL = /^data:image\/png;base64,([A-Za-z0-9+/=]+)$/;
const EFFECT_PROGRAMS = "/assets/effects/programs.json";
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
			codename: row.codename,
			glb: row.materialVariants?.[String( slot )] ?? row.glb
		};
	} );
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
				a.name.localeCompare( b.name )
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
	const effectBytes = await readAsset( EFFECT_PROGRAMS );
	hash.update( EFFECT_PROGRAMS ).update( effectBytes );
	const effects = JSON.parse( effectBytes.toString( "utf8" ) );
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
buildMonsterPortraitAssets
================
*/
export async function buildMonsterPortraitAssets() {
	const manifestData = JSON.parse( await readFile( path.join( publicRoot, "assets/npc/manifest.json" ), "utf8" ) );
	const references = monsterPortraitReferences( manifestData );
	const fingerprint = await monsterPortraitRendererFingerprint( manifestData ), rows = [], missing = [], files = [];
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
	let browser;
	try {
		await server.listen();
		const launched = await launchProbeBrowser( {
			viewport: { width: MONSTER_PORTRAIT_SIZE, height: MONSTER_PORTRAIT_SIZE }
		} );
		browser = launched.browser;
		const base = `http://127.0.0.1:${server.httpServer.address().port}/`;
		let rendered = 0;
		for ( const row of references ) {
			if (
				typeof row.glb !== "string" || !/^\/assets\/npc\/[a-zA-Z0-9/_.-]+\.glb$/.test( row.glb ) ||
				row.glb.includes( ".." )
			) {
				missing.push( row.refObjId );
				continue;
			}
			// Ref ids have their own presentation (rides, scale, effects). Never
			// collapse them solely because they alias the same base GLB.
			const hash = createHash( "sha256" ).update( fingerprint ).update( String( row.refObjId ) ).digest( "hex" );
			const output = PREFIX + hash + ".png", target = path.join( publicRoot, output.slice( 1 ) );
			const cached = await stat( target ).then( value => value.size > 100 ).catch( () => false );
			if ( cached ) claimPublicFile( target );
			else {
				await writeIntoPublicTree(
					target,
					await renderMonsterPortrait( launched.page, base, row.refObjId, row )
				);
				rendered++;
			}
			rows.push( [ row.refObjId, output ] );
			files.push( output );
		}
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
		return {
			files,
			note: `${rows.length} native references (${rendered} rendered), ${missing.length} missing models`
		};
	} finally {
		await browser?.close();
		await server.close();
	}
}
