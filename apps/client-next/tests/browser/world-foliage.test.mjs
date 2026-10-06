/*
===========================================================================

world-foliage.test.mjs - animated foliage takes the native material path

The published animated maple (object 425) is drawn twice through the
world renderer: once with its native material admission, once with the
GLB export's near-zero alpha cutoff. The native cutoff must visibly drop
the low-alpha canopy fragments the export hint would keep.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "animated maple uses native material admission instead of GLB export hints", { timeout: 60000 }, async () => {
	const root = CLIENT_PUBLIC_ROOT, read = p => JSON.parse( readFileSync( root + p, "utf8" ) );
	const index = read( "/assets/world/outdoor/object-resources.json" ),
		ref = index.bsr.find( r => r.objectId === 425 );
	const entry = {
		...read( "/assets/world/china/animated-objects.json" ).objects[ref.sourcePath],
		sourcePath: ref.sourcePath
	};
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: {
			placements: [ { objectId: 425, uid: 1, regionId: "257", position: { x: 0, y: 0, z: 0 }, yaw: 0 } ],
			resources: {
				bsr: [ ref ],
				materialSets: index.materialSets.filter( r => ref.materialPaths.includes( r.sourcePath ) ),
				meshes: ref.meshPaths.map( p =>
					read( index.meshFiles.find( r => r.sourcePath === p ).publicPath ).mesh
				)
			}
		}
	};
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async ( { bundle, entry } ) => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ),
				{ createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
				{ createWorldDecoder } = await import( "/src/engine/runtime/assets/worker/world/world.ts" );
			const assets = createAssets(),
				canvas = document.createElement( "canvas" ),
				renderer = createRenderer( canvas ),
				copy = document.createElement( "canvas" );
			copy.width = copy.height = 512;
			const ctx = copy.getContext( "2d" );
			try {
				const id = assets.request(
						new URL( entry.glbPublicPath, location.origin ).href,
						16 << 20,
						"character"
					),
					deadline = performance.now() + 20000;
				let loaded;
				while ( !(loaded = assets.take( id )) ) {
					if ( performance.now() > deadline ) throw Error( "Tree model deadline" );
					await new Promise( requestAnimationFrame );
				}
				if ( loaded.kind !== "character" ) throw Error( loaded.error ?? "Tree model unavailable" );
				bundle.animated = [ { ...entry, model: { ...loaded.model, images: [] } } ];
				const scene = createWorldDecoder().decode( bundle ),
					policies = scene.groups.map( g => ({
						id: g.id,
						alpha: g.material.alphaCutoff,
						doubleSided: g.material.doubleSided
					}) );
				// Perturb the foreign hints. World BMT admission must make them irrelevant.
				for ( const p of loaded.model.primitives ) {
					p.geometry.material = {
						...p.geometry.material,
						alphaCutoff: .99,
						blend: true,
						doubleSided: false,
						ambient: [ 0, 0, 0 ]
					};
				}
				const repeated = createWorldDecoder().decode( bundle );
				if (
					JSON.stringify( scene.groups.map( g => g.material ) ) !==
						JSON.stringify( repeated.groups.map( g => g.material ) )
				) throw Error( "GLB hints leaked into native world material" );
				async function sample( legacy ) {
					const value = structuredClone( scene );
					value.id = legacy ? "export-hints" : "native-world";
					// Isolate material coverage from the separately tested distance-fade owner.
					for ( const g of value.groups ) delete g.visibility;
					if ( legacy ) {
						for ( const g of value.groups ) {
							if ( g.material.alphaCutoff ) g.material = { ...g.material, alphaCutoff: .004 };
						}
					}
					renderer.setWorld( value );
					for ( const texture of new Set( value.groups.map( g => g.material.texture ).filter( Boolean ) ) ) {
						const id = assets.request( new URL( texture, location.origin ).href, 16 << 20, "png" );
						let image;
						while ( !(image = assets.take( id )) ) {
							if ( performance.now() > deadline ) throw Error( "Tree texture deadline" );
							await new Promise( requestAnimationFrame );
						}
						if ( image.kind !== "image" ) throw Error( image.error ?? "Tree texture unavailable" );
						renderer.setWorldTexture( texture, image.image );
					}
					renderer.setWorldCamera( {
						originRegion: 257,
						eye: [ 0, 200, 640 ],
						target: [ 0, 200, 0 ],
						fov: Math.PI / 3,
						near: 1,
						far: 3500
					} );
					for ( let i = 0; i < 12; i++ ) {
						renderer.frame( { width: 512, height: 512 }, 2 );
						if ( renderer.error() ) throw Error( renderer.error() );
						await new Promise( requestAnimationFrame );
					}
					const image = await createImageBitmap( canvas );
					ctx.drawImage( image, 0, 0 );
					image.close();
					return { png: copy.toDataURL(), pixels: [ ...ctx.getImageData( 0, 0, 512, 512 ).data ] };
				}
				const before = await sample( true ), after = await sample( false );
				let changed = 0;
				for ( let i = 0; i < before.pixels.length; i += 4 ) {
					if ( before.pixels.slice( i, i + 3 ).some( ( v, c ) => Math.abs( v - after.pixels[i + c] ) > 4 ) ) {
						changed++;
					}
				}
				for ( const image of loaded.images ) if ( !("kind" in image) ) image.close();
				return { before: before.png, after: after.png, changed, policies };
			} finally {
				renderer.dispose();
				assets.dispose();
			}
		}, { bundle, entry } );
		await mkdir( "temp/artifacts/tree-fidelity", { recursive: true } );
		for ( const key of [ "before", "after" ] ) {
			await writeFile(
				`temp/artifacts/tree-fidelity/${key}.png`,
				Buffer.from( result[key].split( "," )[1], "base64" )
			);
		}
		await writeFile(
			"temp/artifacts/tree-fidelity/materials.json",
			JSON.stringify( { changed: result.changed, policies: result.policies }, null, 2 )
		);
		assert.ok( result.policies.some( p => p.alpha === 128 / 255 ) );
		assert.ok( result.changed > 1000, "native cutoff must visibly remove low-alpha canopy fragments" );
	} finally {
		await browser.close();
	}
} );
