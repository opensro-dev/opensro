/*
===========================================================================

pick-alpha.test.mjs - the worker's picking mask equals the canvas readback

World DDS textures now carry a picking mask taken from the pixels the
asset worker decodes (pick-alpha.ts). The renderer used to read every such
texture back through a canvas on the main thread. In a real browser, both
paths must give the same bytes for the same texture, including DXT1
punch-through transparency.

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
