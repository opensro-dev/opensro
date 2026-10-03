/*
===========================================================================

helper-effects.test.mjs - the helper mark state effect

A character with visual flag 2 carries the native helper mark (state
effect 0x8000001E): its model and particle program render, loop, follow
the owner, leave with the flag, return with it, and clear on reset.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "native helper assets render, loop, follow their owner and disappear with the appearance flag", {
	timeout: 90000
}, async () => {
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { assetRequestBudget } = await import( "/src/engine/foundation/assets/asset-budget.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createCharacterEffects } = await import( "/src/engine/runtime/characters/effects/effects.ts" );
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const assets = createAssets(), canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas );
			const effects = createCharacterEffects( assets, location.origin, () => {}, createPresentationRandom( 1 ) );
			const models = new Set(), jobs = new Map();
			const body = {
				gid: 101,
				height: 20,
				model: "/assets/fixture-root",
				pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
				clip: "",
				time: 0,
				loop: true,
				scale: 1
			};
			let entities = [ {
				gid: 101,
				refObjId: 1907,
				visualFlags: 2,
				regionId: 257,
				x: 0,
				y: 0,
				z: 0,
				heading: 0
			} ];
			renderer.setCharacterModel( body.model, {
				nodes: [ {
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				} ],
				primitives: [],
				clips: [],
				images: []
			}, [] );
			renderer.setWorld( { id: "helper", originRegion: 257, warnings: [], groups: [] } );
			const output = document.createElement( "canvas" );
			output.width = output.height = 192;
			const context = output.getContext( "2d" );
			function ready( path ) {
				if ( models.has( path ) ) return true;
				if ( !jobs.has( path ) ) {
					jobs.set(
						path,
						assets.request(
							new URL( path, location.origin ).href,
							assetRequestBudget( path.includes( "#" ) ? "effect" : "character" ),
							path.includes( "#" ) ? "effect" : "character"
						)
					);
				}
				return false;
			}
			function step( at ) {
				for ( const [path, id] of jobs ) {
					const result = assets.take( id );
					if ( !result ) continue;
					if ( result.kind !== "character" ) throw Error( result.error ?? "Unexpected model result" );
					renderer.setCharacterModel( path, result.model, result.images );
					models.add( path );
					jobs.delete( path );
				}
				const actors = effects.step( entities, { casts: [] }, at, ready, () => 1.5, [], undefined, [ body ] );
				if ( effects.error() ) throw Error( effects.error() );
				return actors;
			}
			try {
				const deadline = performance.now() + 45000;
				let actors;
				do {
					actors = step( 0 );
					if ( actors.length === 2 ) break;
					if ( performance.now() > deadline ) throw Error( "Helper admission deadline" );
					await new Promise( requestAnimationFrame );
				} while ( true );
				async function sample( at, eye = [ 0, 36, -60 ] ) {
					const actors = step( at );
					renderer.setCharacterActors( [ body, ...actors ] );
					renderer.setWorldCamera( {
						originRegion: 257,
						eye,
						target: [ 0, 36, 0 ],
						fov: 1,
						near: .1,
						far: 500
					} );
					for ( let i = 0; i < 120; i++ ) {
						renderer.frame( { width: 192, height: 192 }, 0 );
						if ( renderer.error() ) throw Error( renderer.error() );
						if ( renderer.phase() === "running" ) break;
						await new Promise( requestAnimationFrame );
					}
					await new Promise( requestAnimationFrame );
					renderer.frame( { width: 192, height: 192 }, 0 );
					const bitmap = await createImageBitmap( canvas );
					context.drawImage( bitmap, 0, 0 );
					bitmap.close();
					return {
						actors: actors.length,
						pixels: [ ...context.getImageData( 0, 0, 192, 192 ).data ],
						png: output.toDataURL()
					};
				}
				const front = await sample( .2 ), loop = await sample( 3.2 ), side = await sample( 3.2, [ 60, 36, 0 ] );
				// Effect elements take their owner's position at a 20 Hz tick
				// (A2E980), and native draws the last tick: the moved sample is
				// one tick later.
				body.pose = { ...body.pose, x: 20 };
				const moved = await sample( 3.25 );
				body.pose = { ...body.pose, x: 0 };
				entities = [ { ...entities[0], visualFlags: 1 } ];
				const removed = await sample( 3.3 );
				entities = [ { ...entities[0], visualFlags: 3 } ];
				const restored = await sample( 3.4 );
				effects.reset();
				entities = [];
				const reset = await sample( 4 );
				const difference = ( a, b ) => a.pixels.reduce( ( n, v, i ) => n + Number( v !== b.pixels[i] ), 0 );
				return {
					models: [ ...models ],
					frontChanged: difference( front, removed ),
					loopDifference: difference( front, loop ),
					sideChanged: difference( side, removed ),
					movedDifference: difference( moved, front ),
					restoredChanged: difference( restored, removed ),
					resetDifference: difference( reset, removed ),
					removedActors: removed.actors,
					images: {
						front: front.png,
						loop: loop.png,
						side: side.png,
						moved: moved.png,
						removed: removed.png,
						restored: restored.png
					}
				};
			} finally {
				effects.dispose();
				renderer.dispose();
				assets.dispose();
				canvas.remove();
			}
		} );
		const out = "temp/artifacts/helper-effects";
		await mkdir( out, { recursive: true } );
		for ( const [name, png] of Object.entries( result.images ) ) {
			await writeFile( `${out}/${name}.png`, Buffer.from( png.split( "," )[1], "base64" ) );
		}
		const { images, ...report } = result;
		await writeFile( `${out}/browser.json`, JSON.stringify( { ...report, errors }, null, 2 ) + "\n" );
		assert.deepEqual( errors, [] );
		assert.equal( result.models.length, 2 );
		assert.ok( result.frontChanged > 100, JSON.stringify( report ) );
		assert.equal( result.loopDifference, 0 );
		assert.ok( result.sideChanged > 100 );
		assert.ok( result.movedDifference > 100 );
		assert.equal( result.removedActors, 0 );
		assert.ok( result.restoredChanged > 100 );
		assert.equal( result.resetDifference, 0 );
	} finally {
		await browser.close();
	}
} );
