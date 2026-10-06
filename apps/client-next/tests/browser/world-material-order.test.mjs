import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	readPublishedAssetJsonSync as read,
	readPublishedAssetBytesSync as bytes
} from "../../../../scripts/lib/publishedAsset.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "published house material layers match native BMT order despite mesh filenames", { timeout: 60000 }, async () => {
	const base = CLIENT_PUBLIC_ROOT + "/";
	const bundle = read( "/assets/world/constantinople/region-694e.json", decodeURI( base ) ),
		resources = bundle.objects.resources;
	const ref = resources.bsr.find( r => r.sourcePath.endsWith( "/euro_constan_house04_h02.bsr" ) );
	assert.ok( ref );
	bundle.objects.placements = bundle.objects.placements.filter( p =>
		p.objectId === ref.objectId && Number( p.regionId ) === 0x6950 && p.uid === 1
	);
	assert.ok( bundle.objects.placements.length );
	resources.bsr = [ ref ];
	resources.meshes = resources.meshes.filter( m => ref.meshPaths.includes( m.sourcePath ) );
	resources.materialSets = resources.materialSets.filter( s => ref.materialPaths.includes( s.sourcePath ) );
	bundle.terrain = { blocks: [] };
	delete bundle.sky;
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
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				for ( const t of [ 9.2, 9.25, 9.3 ] ) {
					const pixels = [];
					for ( const mode of [ "reference", "production", "filename" ] ) {
						const groups = scene.groups.map( g => {
							const mesh = bundle.objects.resources.meshes.find( m =>
								g.id.startsWith( "object:" + m.sourcePath + ":" )
							);
							const index = materials.findIndex( m =>
								m.name.toLowerCase() === mesh.metadata.materialName.toLowerCase()
							);
							// Independent native reference: material index is the outer traversal.
							return {
								...g,
								visibility: undefined,
								materialOrder: mode === "production" ? g.materialOrder : undefined,
								id: mode === "reference" ? String( index ).padStart( 3, "0" ) + g.id : g.id
							};
						} );
						if ( mode === "production" ) groups.reverse();
						else groups.sort( ( a, b ) => a.id.localeCompare( b.id ) );
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
							renderer.frame( { width: 1823, height: 937 }, 0 );
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
