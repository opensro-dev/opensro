/*
===========================================================================

monster-portrait-publisher.test.mjs - the shared still publication contract

Exercise the publisher's actual viewer batch in installed Chrome: a normal
monster, a unique, a failed reference and recovery. No second renderer.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	renderMonsterPortrait,
	MONSTER_PORTRAIT_SIZE
} from "../../../../scripts/build/char/buildMonsterPortraitAssets.mjs";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"publisher keeps transparent catalogue stills across a viewer batch and failure",
	{ timeout: 180000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			for ( const id of [ 1933, 1954 ] ) {
				const bytes = await renderMonsterPortrait( page, CLIENT_NEXT_BASE_URL + "/", id );
				assert.equal( bytes.readUInt32BE( 16 ), MONSTER_PORTRAIT_SIZE );
				assert.equal( bytes.readUInt32BE( 20 ), MONSTER_PORTRAIT_SIZE );
				const pixels = await page.evaluate( async png => {
					const image = new Image();
					image.src = "data:image/png;base64," + png;
					await image.decode();
					const canvas = new OffscreenCanvas( image.width, image.height );
					const context = canvas.getContext( "2d" );
					if ( !context ) throw Error( "Still inspection has no 2D context" );
					context.drawImage( image, 0, 0 );
					const rgba = context.getImageData( 0, 0, image.width, image.height ).data;
					let opaque = 0;
					for ( let i = 3; i < rgba.length; i += 4 ) if ( rgba[i] ) opaque++;
					return { corner: rgba[3], opaque };
				}, bytes.toString( "base64" ) );
				assert.equal( pixels.corner, 0 );
				assert.ok( pixels.opaque > 100 );
			}
			await assert.rejects(
				renderMonsterPortrait( page, CLIENT_NEXT_BASE_URL + "/", 99999999 ),
				/unknown-monster/
			);
			assert.equal( page.url(), "about:blank" );
			const recovered = await renderMonsterPortrait( page, CLIENT_NEXT_BASE_URL + "/", 1933 );
			assert.equal( recovered.readUInt32BE( 16 ), MONSTER_PORTRAIT_SIZE );
		} finally {
			await browser.close();
		}
	}
);
