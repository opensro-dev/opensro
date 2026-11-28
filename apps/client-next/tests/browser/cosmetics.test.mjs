/*
===========================================================================

cosmetics.test.mjs - the live presenter equips and unequips a published cosmetic

Drives the shipped character presentation in a browser and compares the
drawn pixels before and after a cosmetic equip and unequip.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test( "published cosmetic equip and unequip change pixels through the live presenter", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createCharacterPresentation } = await import( "/src/engine/runtime/characters/characters.ts" );
			const random = (await import( "/src/engine/runtime/random/random.ts" )).createPresentationRandom( 1 );
			const assets = createAssets(), canvas = document.createElement( "canvas" );
			document.body.append( canvas );
			const renderer = createRenderer( canvas, random );
			let actors = [];
			const presenter = createCharacterPresentation(
				assets,
				{
					...renderer,
					setCharacterActors( value, portraits, clocks ) {
						actors = value;
						renderer.setCharacterActors( value, portraits, clocks );
					}
				},
				location.origin,
				() => {},
				random
			);
			const output = document.createElement( "canvas" );
			output.width = output.height = 192;
			const context = output.getContext( "2d" );
			async function json( path ) {
				const id = assets.request( new URL( path, location.origin ).href ),
					deadline = performance.now() + 15000;
				let result;
				while ( !(result = assets.take( id )) ) {
					if ( performance.now() > deadline ) throw new Error( "Catalog deadline" );
					await new Promise( requestAnimationFrame );
				}
				if ( result.kind !== "bytes" ) throw new Error( result.error );
				return JSON.parse( new TextDecoder().decode( result.buffer ) );
			}
			try {
				const roster = await json( "/assets/char/roster.json" ),
					body = roster.models.find( m => m.codename.startsWith( "CHAR_CH_MAN_" ) );
				if ( !body ) throw new Error( "Missing body fixture" );
				renderer.setWorld( { id: "costume", originRegion: 257, warnings: [], groups: [] } );
				renderer.setWorldCamera( {
					originRegion: 257,
					eye: [ 0, 15, 65 ],
					target: [ 0, 10, 0 ],
					fov: 1,
					near: 1,
					far: 500
				} );
				const entity = {
					gid: 7,
					refObjId: body.refObjId,
					kind: "player",
					regionId: 257,
					x: 0,
					y: 0,
					z: 0,
					heading: 0,
					name: "fixture",
					equipment: []
				};
				const worn = [ { slot: 0, refObjId: 24284, typeFlags: 0xeac, plus: 0 }, {
					slot: 1,
					refObjId: 24286,
					typeFlags: 0x16ac,
					plus: 0
				} ];
				async function sample( avatars, assembly ) {
					const deadline = performance.now() + 30000;
					while ( true ) {
						presenter.step( [ { ...entity, avatars } ], null, 0 );
						renderer.frame( { width: 192, height: 192 }, 0 );
						if ( renderer.error() || presenter.error() ) {
							throw new Error( renderer.error() ?? presenter.error() );
						}
						if (
							actors.length === 1 && actors[0].model.startsWith( "assembly:" ) === assembly &&
							renderer.characterStats().draws > 0
						) break;
						if ( performance.now() > deadline ) throw new Error( "Cosmetic admission deadline" );
						await new Promise( requestAnimationFrame );
					}
					await new Promise( requestAnimationFrame );
					renderer.frame( { width: 192, height: 192 }, 0 );
					const bitmap = await createImageBitmap( canvas );
					context.drawImage( bitmap, 0, 0 );
					bitmap.close();
					return { pixels: [ ...context.getImageData( 0, 0, 192, 192 ).data ], png: output.toDataURL() };
				}
				const before = await sample( [], false ),
					wornFrame = await sample( worn, true ),
					after = await sample( [], false );
				const changed = before.pixels.reduce(
						( n, value, i ) => n + Number( value !== wornFrame.pixels[i] ),
						0
					),
					restored = before.pixels.every( ( value, i ) => value === after.pixels[i] );
				return { changed, restored, before: before.png, worn: wornFrame.png, after: after.png };
			} finally {
				presenter.dispose();
				renderer.dispose();
				assets.dispose();
				canvas.remove();
			}
		} );
		await mkdir( "temp/artifacts/bugs/cosmetics", { recursive: true } );
		for ( const name of [ "before", "worn", "after" ] ) {
			await writeFile(
				`temp/artifacts/bugs/cosmetics/${name}.png`,
				Buffer.from( result[name].split( "," )[1], "base64" )
			);
		}
		assert.ok( result.changed > 100, `Only ${result.changed} changed channels` );
		assert.equal( result.restored, true );
	} finally {
		await browser.close();
	}
} );
