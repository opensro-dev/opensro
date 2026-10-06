import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	readPublishedAssetJsonSync as read,
	readPublishedAssetBytesSync as bytes
} from "../../../../scripts/lib/publishedAsset.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "fading harbor stall cannot erase the dock submitted before it", { timeout: 60000 }, async () => {
	const base = CLIENT_PUBLIC_ROOT + "/";
	const bundle = read( "/assets/world/constantinople/region-694e.json", decodeURI( base ) ),
		resources = bundle.objects.resources;
	const refs = bundle.objects.resources.bsr.filter( r => [ 1630, 1285 ].includes( r.objectId ) );
	assert.equal( refs.length, 2 );
	bundle.objects.placements = bundle.objects.placements.filter( p =>
		p.objectId === 1630 || p.objectId === 1285 && Number( p.regionId ) === 0x6950 && p.uid === 21505
	);
	resources.bsr = refs;
	resources.meshes = resources.meshes.filter( m => refs.some( r => r.meshPaths.includes( m.sourcePath ) ) );
	resources.materialSets = resources.materialSets.filter( s =>
		refs.some( r => r.materialPaths.includes( s.sourcePath ) )
	);
	bundle.terrain = { blocks: [] };
	delete bundle.water;
	const materials = resources.materialSets.flatMap( s => s.materials ).map( m => ({
		...m,
		png: m.texturePublicPath ?
			Buffer.from( bytes( m.texturePublicPath, decodeURI( base ) ) ).toString( "base64" ) :
			null
	}) );
	const camera = read( "/assets/title/constantinople/manifest.json", decodeURI( base ) ).camera;
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const rows = await page.evaluate( async ( { bundle, materials, camera } ) => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createWorldDecoder } = await import( "/src/engine/runtime/assets/worker/world/world.ts" );
			const { sampleFrontendCamera, frontendCameraView } = await import(
				"/src/engine/foundation/rendering/frontend-camera.ts"
			);
			const canvas = document.createElement( "canvas" ),
				renderer = createRenderer( canvas ),
				output = document.createElement( "canvas" );
			output.width = 1823;
			output.height = 937;
			const ctx = output.getContext( "2d" );
			const scene = createWorldDecoder().decode( bundle, true ), rows = [];
			scene.groups = scene.groups.filter( g => g.id.startsWith( "object:" ) );
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				for ( const t of [ .9, .95, 1 ] ) {
					const pixels = [];
					for ( const mode of [ "reference", "production", "filename" ] ) {
						const groups = scene.groups.map( g => {
							const mesh = bundle.objects.resources.meshes.find( m =>
								g.id.startsWith( "object:" + m.sourcePath + ":" )
							);
							const index = materials.findIndex( m =>
								m.name.toLowerCase() === mesh.metadata.materialName.toLowerCase()
							);
							// Explicit fixture reference: dock submitted before stall; BMT order within each.
							return {
								...g,
								materialOrder: mode === "production" ? g.materialOrder : undefined,
								id: mode === "reference" ?
									(g.id.includes( "/harbor/" ) ? "0" : "1") + String( index ).padStart( 3, "0" ) +
									g.id :
									g.id
							};
						} );
						if ( mode !== "production" ) groups.sort( ( a, b ) => a.id.localeCompare( b.id ) );
						renderer.setWorld( { ...scene, id: `${t}:${mode}`, groups } );
						renderer.setWorldCamera( frontendCameraView( sampleFrontendCamera( camera, t ) ) );
						for ( const m of materials ) {
							if ( m.png ) {
								renderer.setWorldTexture(
									m.texturePublicPath,
									await createImageBitmap(
										await (await fetch( "data:image/png;base64," + m.png )).blob()
									)
								);
							}
						}
						for ( let k = 0; k < 5; k++ ) {
							renderer.frame( { width: 1823, height: 937 }, k * .04 );
							await new Promise( requestAnimationFrame );
						}
						if ( renderer.phase() !== "running" ) throw new Error( renderer.error() );
						const image = await createImageBitmap( canvas );
						ctx.drawImage( image, 0, 0 );
						image.close();
						pixels.push( ctx.getImageData( 0, 0, 1823, 937 ).data );
					}
					const differences = p => {
						let count = 0;
						for ( let i = 0; i < p.length; i += 4 ) {
							if ( [ 0, 1, 2 ].some( k => p[i + k] !== pixels[0][i + k] ) ) count++;
						}
						return count;
					};
					rows.push( { t, production: differences( pixels[1] ), filename: differences( pixels[2] ) } );
				}
				return rows;
			} finally {
				renderer.dispose();
			}
		}, { bundle, materials, camera } );
		for ( const row of rows ) {
			assert.equal( row.production, 0, JSON.stringify( row ) );
			assert.ok(
				row.filename > 0,
				"Fixture must detect the filename-order regression: " + JSON.stringify( row )
			);
		}
		assert.deepEqual( errors, [] );
	} finally {
		await browser.close();
	}
} );
