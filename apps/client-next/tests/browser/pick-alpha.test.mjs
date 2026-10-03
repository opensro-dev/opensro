/*
===========================================================================

pick-alpha.test.mjs - the worker's picking mask equals the canvas readback

World DDS textures now carry a picking mask taken from the pixels the
asset worker decodes (pick-alpha.ts). The renderer used to read every such
texture back through a canvas on the main thread. In a real browser, both
paths must give the same bytes for the same texture, including DXT1
punch-through transparency. World PNGs read their mask back inside the
asset worker; that must equal the main-thread readback too.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "decoded DDS alpha matches the bitmap readback byte for byte", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const result = await page.evaluate( async () => {
			const { decodeDxt1 } = await import( "/src/engine/foundation/assets/dds.ts" );
			const { rgbaPickAlpha } = await import( "/src/engine/foundation/rendering/pick-alpha.ts" );
			const { readPickAlpha } = await import( "/src/engine/runtime/renderer/readback/readback.ts" );
			// A DDS header for a DXT1 surface, then the given blocks.
			const dds = ( width, height, blocks ) => {
				const bytes = new Uint8Array( 128 + blocks.length ), view = new DataView( bytes.buffer );
				view.setUint32( 0, 0x20534444, true );
				view.setUint32( 4, 124, true );
				view.setUint32( 8, 0x1007, true );
				view.setUint32( 12, height, true );
				view.setUint32( 16, width, true );
				view.setUint32( 76, 32, true );
				view.setUint32( 80, 4, true );
				view.setUint32( 84, 0x31545844, true );
				bytes.set( blocks, 128 );
				return bytes;
			};
			let seed = 11;
			const random = () => ((seed = (seed * 1103515245 + 12345) >>> 0) / 4294967296);
			const samples = [];
			for ( const [width, height] of [ [ 16, 16 ], [ 64, 32 ], [ 8, 128 ] ] ) {
				const blocks = Uint8Array.from( { length: width * height / 2 }, () => Math.floor( random() * 256 ) );
				// Half the blocks in BC1's three-colour mode (first <= second endpoint).
				for ( let b = 0; b < blocks.length; b += 16 ) {
					blocks[b + 2] = blocks[b];
					blocks[b + 3] = blocks[b + 1];
				}
				samples.push( dds( width, height, blocks ) );
			}
			const real = await fetch( "/assets/world/china/terrain-lightmaps/97-167.dds" );
			if ( real.ok ) samples.push( new Uint8Array( await real.arrayBuffer() ) );
			const rows = [];
			for ( const bytes of samples ) {
				const decoded = decodeDxt1( bytes );
				const worker = rgbaPickAlpha( decoded.width, decoded.height, decoded.pixels );
				const image = await createImageBitmap( new ImageData( decoded.pixels, decoded.width, decoded.height ), {
					premultiplyAlpha: "none",
					colorSpaceConversion: "none"
				} );
				const canvas = readPickAlpha( image );
				let differing = 0, transparent = 0;
				for ( let i = 0; i < worker.pixels.length; i++ ) {
					if ( worker.pixels[i] !== canvas.pixels[i] ) differing++;
					if ( worker.pixels[i] === 0 ) transparent++;
				}
				rows.push( {
					size: [ worker.width, worker.height, canvas.width, canvas.height ],
					differing,
					transparent
				} );
			}
			return rows;
		} );
		assert.ok( result.length >= 3 );
		for ( const row of result ) {
			assert.deepEqual( row.size.slice( 0, 2 ), row.size.slice( 2 ) );
			assert.equal( row.differing, 0 );
		}
		assert.ok( result.some( row => row.transparent > 0 ), "punch-through transparency was exercised" );
	} finally {
		await browser.close();
	}
} );

test( "a PNG mask read in the asset worker equals the main-thread readback", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	try {
		await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
		const result = await page.evaluate( async () => {
			const { readPickAlpha } = await import( "/src/engine/runtime/renderer/readback/readback.ts" );
			// A PNG with graded and fully transparent texels, as world textures have.
			const width = 37, height = 21, rgba = new Uint8ClampedArray( width * height * 4 );
			for ( let i = 0; i < width * height; i++ ) {
				rgba.set( [ i * 7 & 255, i * 13 & 255, i * 29 & 255, i % 5 === 0 ? 0 : i * 31 & 255 ], i * 4 );
			}
			const canvas = new OffscreenCanvas( width, height );
			const context = canvas.getContext( "2d" );
			if ( !context ) throw new Error( "2D canvas unavailable" );
			context.putImageData( new ImageData( rgba, width, height ), 0, 0 );
			const png = new Uint8Array( await (await canvas.convertToBlob( { type: "image/png" } )).arrayBuffer() );
			// The worker side: decode and read back as loader.ts does for world PNGs.
			const source = `
				const { bitmapPickAlpha } = await import( "${location.origin}/src/engine/foundation/rendering/pick-alpha.ts" );
				onmessage = async ( event ) => {
					const image = await createImageBitmap( new Blob( [ event.data ], { type: "image/png" } ), {
						premultiplyAlpha: "none",
						colorSpaceConversion: "none"
					} );
					const alpha = bitmapPickAlpha( image );
					postMessage( alpha, [ alpha.pixels.buffer ] );
				};
				postMessage( "ready" );
			`;
			const worker = new Worker( URL.createObjectURL( new Blob( [ source ], { type: "text/javascript" } ) ), {
				type: "module"
			} );
			await new Promise( resolve => worker.addEventListener( "message", resolve, { once: true } ) );
			const fromWorker = await new Promise( resolve => {
				worker.addEventListener( "message", event => resolve( event.data ), { once: true } );
				worker.postMessage( png );
			} );
			worker.terminate();
			const image = await createImageBitmap( new Blob( [ png ], { type: "image/png" } ), {
				premultiplyAlpha: "none",
				colorSpaceConversion: "none"
			} );
			const main = readPickAlpha( image );
			let differing = 0, transparent = 0;
			for ( let i = 0; i < main.pixels.length; i++ ) {
				if ( main.pixels[i] !== fromWorker.pixels[i] ) differing++;
				if ( main.pixels[i] === 0 ) transparent++;
			}
			return { size: [ fromWorker.width, fromWorker.height, main.width, main.height ], differing, transparent };
		} );
		assert.deepEqual( result.size, [ 37, 21, 37, 21 ] );
		assert.equal( result.differing, 0 );
		assert.ok( result.transparent > 0 );
	} finally {
		await browser.close();
	}
} );
