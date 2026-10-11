/*
===========================================================================

sun-shadow-placement.test.mjs - the sun cascade shades where the light puts the shadow

#707 flipped the receiver's cascade lookup and #754 reverted it: a
mirrored lookup still darkens some ground under a symmetric caster, which
is all hdr-stages.test.mjs asks. This test pins the mapping itself: an
off-axis pillar's shadow must cover its exact geometric footprint and
darken nothing else on the ground.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

import { captureSunShadowPlacement } from "../helpers/sun-shadow-gpu.mjs";

// A channel difference below this is filtering noise, not a shadow or a proxy.
const PIXEL_TOLERANCE = 4;
// The inner footprint is fully shaded; at most this share of the darkened
// ground falls outside the outer footprint.
const MIN_COVERED = 0.9;
const MAX_STRAY = 0.02;
// The footprint must be a visible region of the capture, not a few pixels.
const MIN_FOOTPRINT_PIXELS = 200;

/*
================
differing

Pixel offsets whose colour differs between two captures.
================
*/
function differing( a, b ) {
	const pixels = [];
	for ( let i = 0; i < a.length; i += 4 ) {
		if ( [ 0, 1, 2 ].some( c => Math.abs( a[i + c] - b[i + c] ) > PIXEL_TOLERANCE ) ) pixels.push( i );
	}
	return pixels;
}

test(
	"an off-axis caster's sun shadow covers its geometric footprint and nothing else",
	{ timeout: 120000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		try {
			await page.route(
				CLIENT_NEXT_BASE_URL + "/",
				route => route.fulfill( { contentType: "text/html", body: "<!doctype html><body></body>" } )
			);
			await page.goto( CLIENT_NEXT_BASE_URL );
			const rows = Object.fromEntries(
				(await page.evaluate( captureSunShadowPlacement )).map( row => [ row.mode, row.rgba ] )
			);
			const pillar = new Set( differing( rows.bare, rows.native ) );
			const inner = differing( rows.native, rows.inner ).filter( i => !pillar.has( i ) );
			const outer = new Set( differing( rows.native, rows.outer ) );
			const darkened = differing( rows.native, rows.shadow ).filter( i =>
				!pillar.has( i ) && rows.shadow[i] < rows.native[i]
			);
			assert.ok( pillar.size > 0, "The pillar must be in view" );
			assert.ok( inner.length >= MIN_FOOTPRINT_PIXELS, `The footprint must be in view (${inner.length} pixels)` );
			const covered = inner.filter( i => darkened.includes( i ) ).length / inner.length;
			const stray = darkened.filter( i => !outer.has( i ) ).length / Math.max( 1, darkened.length );
			assert.ok(
				covered >= MIN_COVERED,
				`The shadow must cover its footprint (${(covered * 100).toFixed( 1 )}%)`
			);
			assert.ok(
				stray <= MAX_STRAY,
				`The shadow must not land off its footprint (${(stray * 100).toFixed( 1 )}% stray)`
			);
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);
