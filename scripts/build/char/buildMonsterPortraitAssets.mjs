/*
===========================================================================

buildMonsterPortraitAssets.mjs - cached stills of authored monster models

Port-only map illustrations. No licensed images enter source control: the
family renders the locally baked native GLBs with the production decoder and
renderer. Aliased references share a still; ordinary material slots remain
distinct. A focused publication works without a game server or asset packs.

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
import { writeIntoPublicTree } from "../shared/publicWrite.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { clientBuildDefinitions } from "../../../apps/client-next/tools/build-metadata.mjs";

const PREFIX = "/assets/npc/hunting-portraits/";
const CLIENT = path.resolve( import.meta.dirname, "../../../apps/client-next" );

/*
================
monsterPortraitReferences

Use the ordinary authored BMT slot, matching monsterMaterialSlot's normal
grade branch. Champion enlargement and live availability are not depicted.
================
*/
export function monsterPortraitReferences( manifest ) {
	return Object.values( npcManifestModels( manifest ) ).filter( row =>
		row.kind === "monster" && Number.isInteger( row.refObjId ) && row.refObjId > 0
	).sort( ( a, b ) => a.refObjId - b.refObjId ).map( row => {
		const slot = [ 1, 3, 4 ].includes( row.materialKind ) ? row.materialKind : 0;
		return { refObjId: row.refObjId, glb: row.materialVariants?.[String( slot )] ?? row.glb };
	} );
}

/*
================
rendererFingerprint

Changes to the baker, decoder or rendering code invalidate cached stills.
================
*/
async function rendererFingerprint() {
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
			else hash.update( await readFile( file ) );
		}
	}
	for (
		const directory of [
			"runtime/renderer",
			"runtime/assets/worker/model",
			"foundation/rendering",
			"foundation/assets"
		]
	) {
		await visit( path.join( CLIENT, "src/engine", directory ) );
	}
	hash.update( await readFile( import.meta.filename ) );
	return hash.digest( "hex" );
}

/*
================
renderPortrait

One retained scene and one model at a time. Copy the WebGL frame immediately
before its drawing buffer can be cleared, then reduce to a static 96px PNG.
================
*/
async function renderPortrait( url ) {
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createModelDecoder } = await import( "/src/engine/runtime/assets/worker/model/model.ts" );
	const { decodeNativeTexture, NATIVE_TEXTURE_MIME } = await import(
		"/src/engine/foundation/assets/native-texture.ts"
	);
	if ( !window.portraitBaker ) {
		const canvas = document.querySelector( "canvas" ), renderer = createRenderer( canvas );
		window.portraitBaker = { canvas, renderer, decoder: createModelDecoder() };
		const deadline = performance.now() + 20000;
		while ( renderer.phase() === "starting" ) {
			if ( performance.now() > deadline ) throw Error( "Portrait renderer startup timed out" );
			await new Promise( requestAnimationFrame );
		}
	}
	const { canvas, renderer, decoder } = window.portraitBaker;
	const response = await fetch( url );
	if ( !response.ok ) throw Error( `Portrait model HTTP ${response.status}` );
	const source = decoder.character( decoder.decode( new Uint8Array( await response.arrayBuffer() ) ) );
	const images = [];
	for ( const image of source.images ) {
		images.push(
			image.mime === NATIVE_TEXTURE_MIME ?
				decodeNativeTexture( image.bytes ) :
				await createImageBitmap( new Blob( [ image.bytes ], { type: image.mime } ), {
					premultiplyAlpha: "none",
					colorSpaceConversion: "none"
				} )
		);
	}
	const model = { ...source, images: images.map( image => ({ width: image.width, height: image.height }) ) };
	const box = model.aggregateBox;
	if ( !box || box.some( value => !Number.isFinite( value ) ) ) throw Error( "Monster has no finite bounds" );
	const cx = -(box[0] + box[3]) / 2, cy = (box[1] + box[4]) / 2, cz = -(box[2] + box[5]) / 2;
	const height = box[4] - box[1], extent = Math.max( box[3] - box[0], height, (box[5] - box[2]) * .55 );
	const distance = Math.max( 1, extent / (2 * Math.tan( Math.PI / 12 )) * 1.35 );
	renderer.setCharacterActors( [] );
	renderer.retainCharacterModels( [] );
	renderer.setCharacterModel( url, model, images );
	renderer.setCharacterActors( [ {
		gid: 1,
		model: url,
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1,
		pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: Math.PI }
	} ] );
	// Animation can move the authored bind-pose bounds. Fit the actual frozen
	// frame, preserving the complete creature instead of clipping a large tail.
	const full = document.createElement( "canvas" );
	full.width = full.height = 256;
	const context = full.getContext( "2d" );
	let bounds, background;
	for ( let attempt = 0; attempt < 6; attempt++ ) {
		const fittedDistance = distance * Math.pow( 1.4, attempt );
		renderer.setCharacterPreview( {
			eye: [ cx + fittedDistance * .2, cy + height * .1, cz - fittedDistance ],
			target: [ cx, cy, cz ],
			near: .01,
			far: 100000,
			fov: Math.PI / 6
		} );
		await renderer.frame( { width: 256, height: 256 } );
		if ( renderer.error() ) throw Error( renderer.error() );
		const bitmap = await createImageBitmap( canvas );
		context.clearRect( 0, 0, 256, 256 );
		context.drawImage( bitmap, 0, 0 );
		bitmap.close();
		const pixels = context.getImageData( 0, 0, 256, 256 ).data;
		background = [ pixels[0], pixels[1], pixels[2] ];
		bounds = [ 256, 256, -1, -1 ];
		for ( let y = 0; y < 256; y++ ) {
			for ( let x = 0; x < 256; x++ ) {
				const i = (y * 256 + x) * 4;
				if (
					Math.max( ...background.map( ( value, channel ) => Math.abs( pixels[i + channel] - value ) ) ) < 10
				) continue;
				bounds[0] = Math.min( bounds[0], x );
				bounds[1] = Math.min( bounds[1], y );
				bounds[2] = Math.max( bounds[2], x );
				bounds[3] = Math.max( bounds[3], y );
			}
		}
		if ( bounds[2] < 0 ) throw Error( "Monster portrait rendered no pixels" );
		if ( bounds[0] > 2 && bounds[1] > 2 && bounds[2] < 253 && bounds[3] < 253 ) break;
		if ( attempt === 5 ) throw Error( "Monster portrait cannot fit its frame" );
	}
	const size = Math.max( bounds[2] - bounds[0] + 1, bounds[3] - bounds[1] + 1 ) * 1.14;
	const copy = document.createElement( "canvas" );
	copy.width = copy.height = 96;
	const output = copy.getContext( "2d" );
	output.fillStyle = "rgb(" + background.join( "," ) + ")";
	output.fillRect( 0, 0, 96, 96 );
	output.drawImage(
		full,
		(bounds[0] + bounds[2] - size) / 2,
		(bounds[1] + bounds[3] - size) / 2,
		size,
		size,
		0,
		0,
		96,
		96
	);
	return copy.toDataURL( "image/png" ).split( "," )[1];
}

/*
================
buildMonsterPortraitAssets
================
*/
export async function buildMonsterPortraitAssets() {
	const references = monsterPortraitReferences(
		JSON.parse( await readFile( path.join( publicRoot, "assets/npc/manifest.json" ), "utf8" ) )
	);
	const fingerprint = await rendererFingerprint(), sources = new Map(), rows = [], missing = [], files = [];
	for ( const row of references ) {
		if (
			typeof row.glb !== "string" || !/^\/assets\/npc\/[a-zA-Z0-9/_.-]+\.glb$/.test( row.glb ) ||
			row.glb.includes( ".." )
		) {
			missing.push( row.refObjId );
			continue;
		}
		if ( !sources.has( row.glb ) ) {
			const source = path.join( publicRoot, row.glb.slice( 1 ) );
			const bytes = await readFile( source );
			const hash = createHash( "sha256" ).update( fingerprint ).update( bytes ).digest( "hex" );
			sources.set( row.glb, { source, output: PREFIX + hash + ".png" } );
		}
		rows.push( [ row.refObjId, sources.get( row.glb ).output ] );
	}
	const require = createRequire( path.join( CLIENT, "package.json" ) );
	const { createServer } = await import( pathToFileURL( require.resolve( "vite" ) ).href );
	const server = await createServer( {
		configFile: false,
		root: CLIENT,
		logLevel: "error",
		define: clientBuildDefinitions( CLIENT ),
		resolve: { alias: { "@": path.join( CLIENT, "src" ) } },
		server: { host: "127.0.0.1", port: 0 },
		plugins: [ {
			name: "native-monster-stills",
			configureServer( instance ) {
				instance.middlewares.use( async ( request, response, next ) => {
					if ( request.url === "/" ) {
						response.setHeader( "Content-Type", "text/html" );
						response.end( '<!doctype html><canvas style="width:256px;height:256px"></canvas>' );
					} else if ( sources.has( request.url ) ) {
						response.setHeader( "Content-Type", "model/gltf-binary" );
						response.end( await readFile( sources.get( request.url ).source ) );
					} else next();
				} );
			}
		} ]
	} );
	let browser;
	try {
		await server.listen();
		const launched = await launchProbeBrowser( { viewport: { width: 256, height: 256 } } );
		browser = launched.browser;
		await launched.page.goto( `http://127.0.0.1:${server.httpServer.address().port}/` );
		let rendered = 0;
		for ( const [glb, row] of sources ) {
			const target = path.join( publicRoot, row.output.slice( 1 ) );
			const cached = await stat( target ).then( value => value.size > 100 ).catch( () => false );
			if ( cached ) claimPublicFile( target );
			else {
				await writeIntoPublicTree(
					target,
					Buffer.from( await launched.page.evaluate( renderPortrait, glb ), "base64" )
				);
				rendered++;
			}
			files.push( row.output );
		}
		const manifest = path.join( publicRoot, (PREFIX + "manifest.json").slice( 1 ) );
		await writeJsonIfChanged( manifest, { format: "sro-hunting-portraits", version: 1, rows, missing } );
		await refreshPrecompressedSidecars( [ manifest ], { onlyWhenStale: true } );
		files.push( PREFIX + "manifest.json.gz" );
		return {
			files,
			note:
				`${rows.length} native references, ${sources.size} stills (${rendered} rendered), ${missing.length} missing models`
		};
	} finally {
		await browser?.close();
		await server.close();
	}
}
