/*
===========================================================================

layout.test.mjs - fitted artwork and input share one coordinate transform

Modal screen coverage survives fitting; masks and deferred text fallbacks
follow the artwork without mutating cached source geometry.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { fitUiGroup } = await import( "../../src/engine/foundation/ui/layout.ts" );
const { compactWindowDrag } = await import( "../../src/engine/runtime/ui/hud/compact-hud.ts" );
const { textRunQuad, expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { resolveTextOverlaps } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );

/** @typedef {import("../../src/engine/contracts/ui").UiQuad} UiQuad */
/** @typedef {import("../../src/engine/contracts/ui").UiRect} UiRect */
/** @typedef {import("../../src/engine/contracts/ui").UiControl} UiControl */

/*
================
quad
================
*/
/** @param {UiRect} rect @param {UiRect} clip @returns {UiQuad} */
function quad( rect, clip ) {
	return { rect, clip, uv: [ 0, 0, 1, 1 ], color: [ 1, 1, 1, 1 ], texture: "fixture" };
}

test("exhausted inset bounds retain a positive common transform for every group member", () => {
	const screen = /** @type {UiRect} */ ([ 0, 0, 1, 1 ]);
	const source = /** @type {UiRect} */ ([ 20, 20, 100, 50 ]);
	for ( const [width, height] of [ [ -15, -16 ], [ 0, 0 ], [ -15, 50 ], [ 100, -16 ] ] ) {
		for ( const explicit of [ false, true ] ) {
			const local = /** @type {UiRect} */ ([ 30, 30, 20, 10 ]);
			const run = textRunQuad( [ quad( local, local ) ] );
			assert.ok( run );
			const quads = [ quad( source, screen ), {
				...run,
				mask: { texture: "mask", rect: local },
				textLayout: { box: local, fitted: run }
			} ];
			/** @type {UiControl[]} */
			const controls = [ { id: "button", kind: "button", label: "Button", rect: local } ];
			const blocks = [ local, screen ];
			fitUiGroup( quads, controls, 0, 0, [ 8, 9, width, height ], screen, {
				blocks,
				...(explicit ? { sourceBounds: source } : {})
			} );
			const scale = width <= 0 ? 0.01 : 0.02;
			const x = width <= 0 ? 8 : 69, y = height <= 0 ? 9 : 44.75;
			assert.deepEqual( quads[0].rect, [ x, y, 100 * scale, 50 * scale ] );
			const expected = [ x + 10 * scale, y + 10 * scale, 20 * scale, 10 * scale ];
			assert.deepEqual( controls[0].rect, expected );
			assert.deepEqual( blocks[0], expected );
			assert.equal( blocks[1], screen );
			assert.equal( quads[0].clip, screen );
			assert.deepEqual( quads[1].clip, expected );
			assert.deepEqual( quads[1].mask?.rect, expected );
			assert.deepEqual( quads[1].textLayout?.box, expected );
			assert.deepEqual( quads[1].textLayout?.fitted?.rect, expected );
			assert.deepEqual( expandTextRuns( [ quads[1] ] )[0].rect, expected );
			assert.deepEqual( run.rect, local, "cached text geometry stays reusable" );
		}
	}
});

test("fitting transforms only the selected artwork, controls and blocks", () => {
	const screen = /** @type {const} */ ([ 0, 0, 200, 150 ]);
	const prefix = quad( [ 1, 2, 3, 4 ], screen );
	const art = Object.freeze( {
		...quad( [ 100, 50, 400, 200 ], screen ),
		mask: { texture: "mask", rect: /** @type {const} */ ([ 120, 70, 80, 40 ]) }
	} );
	const quads = [ prefix, art, quad( [ 120, 70, 80, 40 ], [ 110, 60, 100, 60 ] ) ];
	/** @type {UiControl[]} */
	const controls = [
		{ id: "outside", kind: "button", label: "Outside", rect: [ 1, 2, 3, 4 ], draggable: true },
		{ id: "inside", kind: "button", label: "Inside", rect: [ 120, 70, 80, 40 ], draggable: true, carry: true }
	];
	const firstControl = controls[0];
	/** @type {UiRect[]} */
	const blocks = [ [ 1, 2, 3, 4 ], [ 100, 50, 400, 200 ], screen, [ ...screen ] ];
	const firstBlock = blocks[0], modalCopy = blocks[3];
	fitUiGroup( quads, controls, 1, 1, screen, screen, { blocks, firstBlock: 1, disableDrag: true } );
	assert.equal( quads[0], prefix );
	assert.equal( controls[0], firstControl );
	assert.equal( blocks[0], firstBlock );
	assert.deepEqual( quads[1].rect, [ 0, 50, 200, 100 ] );
	assert.equal( quads[1].clip, screen );
	assert.deepEqual( quads[1].mask, { texture: "mask", rect: [ 10, 60, 40, 20 ] } );
	assert.deepEqual( quads[2].clip, [ 5, 55, 50, 30 ] );
	assert.deepEqual( controls[1].rect, quads[2].rect );
	assert.equal( controls[1].draggable, false );
	assert.equal( controls[1].carry, true );
	assert.deepEqual( blocks[1], quads[1].rect );
	assert.equal( blocks[2], screen );
	assert.equal( blocks[3], modalCopy );
	assert.deepEqual( art.mask.rect, [ 120, 70, 80, 40 ] );
});

test("fitted text keeps overlap boxes, shared identity and replacement glyphs aligned", () => {
	const screen = /** @type {const} */ ([ 0, 0, 100, 100 ]);
	const fitted = textRunQuad( [ quad( [ 0, 0, 3, 10 ], screen ) ] );
	const wide = textRunQuad( [ quad( [ 0, 0, 6, 10 ], screen ), quad( [ 24, 0, 6, 10 ], screen ) ] );
	const shared = textRunQuad( [ quad( [ 0, 0, 1, 1 ], screen ) ] );
	const neighbor = textRunQuad( [ quad( [ 22, 0, 6, 10 ], screen ) ] );
	assert.ok( fitted && wide && shared && neighbor );
	const layout = Object.freeze( { box: /** @type {const} */ ([ 0, 0, 15, 12 ]), fitted } );
	/** @type {UiQuad[]} */
	const quads = [
		quad( [ 0, 0, 200, 200 ], screen ),
		{ ...wide, textLayout: layout },
		{ ...shared, textLayout: layout },
		{ ...neighbor, textLayout: { box: [ 20, 0, 20, 12 ], fitted: null } }
	];
	fitUiGroup( quads, [], 0, 0, screen, screen );
	assert.equal( quads[1].textLayout, quads[2].textLayout );
	assert.deepEqual( quads[1].textLayout?.box, [ 0, 0, 7.5, 6 ] );
	assert.equal( quads[3].textLayout?.fitted, null );
	assert.deepEqual( expandTextRuns( resolveTextOverlaps( quads ) ).map( q => q.rect ), [
		[ 0, 0, 100, 100 ],
		[ 0, 0, 1.5, 5 ],
		[ 11, 0, 3, 5 ]
	] );
	assert.deepEqual( fitted.rect, [ 0, 0, 3, 10 ] );
});

test("legacy no-op preserves identities while explicit drag suppression also works without fitting", () => {
	const screen = /** @type {const} */ ([ 0, 0, 200, 150 ]);
	const art = quad( [ 20, 30, 40, 50 ], screen );
	/** @type {UiControl} */
	const control = { id: "drag", kind: "button", label: "Drag", rect: art.rect, draggable: true };
	const quads = [ art ], controls = [ control ];
	fitUiGroup( quads, controls, 0, 0, screen, screen );
	assert.equal( quads[0], art );
	assert.equal( controls[0], control );
	fitUiGroup( quads, controls, 0, 0, screen, screen, { disableDrag: true } );
	assert.equal( quads[0], art );
	assert.deepEqual( controls[0], { ...control, draggable: false } );
	fitUiGroup( [], controls, 0, 0, screen, screen, { blocks: [] } );
});

test("compact fitting preserves gameplay drags and carries while suppressing window positions and map pan", () => {
	const cases = [
		{ id: "slot:13", draggable: true, carry: true, expected: true },
		{ id: "storage-slot:0", draggable: true, carry: true, expected: true },
		{ id: "skill:100", draggable: true, carry: undefined, expected: true },
		{ id: "action:1000", draggable: true, carry: undefined, expected: true },
		{ id: "hotbar:1", draggable: true, carry: undefined, expected: true },
		{ id: "hotbar:41", draggable: true, carry: undefined, expected: true },
		{ id: "hotbar:42", draggable: false, carry: undefined, expected: false },
		{ id: "main-popup-drag", draggable: true, carry: undefined, expected: false },
		{ id: "ext-drag", draggable: true, carry: undefined, expected: false },
		{ id: "window-drag:Inventory", draggable: true, carry: undefined, expected: false },
		{ id: "map-drag", draggable: true, carry: undefined, expected: false },
		{ id: "map-pan", draggable: true, carry: undefined, expected: false }
	];
	// Suppression must also apply when the group already fits. A blanket
	// disableDrag would lose binding drags and break click-carry admission.
	for ( const scale of [ 1, 0.5 ] ) {
		const screen = /** @type {UiRect} */ ([ 0, 0, 400 * scale, 200 * scale ]);
		const art = quad( [ 0, 0, 400, 200 ], screen );
		const prefix = quad( [ 1, 2, 3, 4 ], screen );
		/** @type {UiControl} */
		const outside = Object.freeze( {
			id: "window-drag:Outside",
			kind: "button",
			label: "Outside",
			rect: /** @type {UiRect} */ ([ 1, 2, 3, 4 ]),
			draggable: true
		} );
		/** @type {UiControl[]} */
		const originals = cases.map( row =>
			Object.freeze( {
				id: row.id,
				kind: row.id === "map-pan" ? "region" : "button",
				label: row.id,
				rect: /** @type {UiRect} */ ([ 40, 40, 32, 32 ]),
				draggable: row.draggable,
				carry: row.carry
			} )
		);
		const quads = [ prefix, art ], controls = [ outside, ...originals ];
		fitUiGroup( quads, controls, 1, 1, screen, screen, { disableDrag: compactWindowDrag } );
		assert.equal( quads[0], prefix );
		assert.equal( controls[0], outside, "predicate only affects the selected group" );
		assert.deepEqual( quads[1].rect, screen );
		for ( const [index, row] of cases.entries() ) {
			assert.deepEqual( controls[index + 1], {
				...originals[index],
				draggable: row.expected,
				rect: [ 40 * scale, 40 * scale, 32 * scale, 32 * scale ]
			}, `${row.id}: scale ${scale} retains carry and activation fields` );
			assert.equal( originals[index].draggable, row.draggable, "source control remains reusable" );
			assert.deepEqual( originals[index].rect, [ 40, 40, 32, 32 ] );
		}
	}
});

test("explicit source bounds keep a minimap stable as clipped neighboring tiles arrive", () => {
	const screen = /** @type {const} */ ([ 0, 0, 667, 375 ]);
	const sourceBounds = /** @type {const} */ ([ 400, 100, 160, 160 ]);
	const bounds = /** @type {const} */ ([ 0, 89, 667, 194 ]);
	const frame = quad( sourceBounds, screen );
	const tile = quad( [ 240, -60, 480, 480 ], [ 416, 116, 128, 128 ] );
	for ( const tiles of [ [], [ tile ] ] ) {
		const quads = [ frame, ...tiles ];
		/** @type {UiControl[]} */
		const controls = [ { id: "minimap-in", label: "Zoom in", kind: "button", rect: [ 540, 110, 16, 16 ] } ];
		fitUiGroup( quads, controls, 0, 0, bounds, screen, { sourceBounds } );
		assert.equal( quads[0], frame, "visible frame already fits; off-window tile coverage cannot shrink it" );
		assert.deepEqual( controls[0].rect, [ 540, 110, 16, 16 ] );
		if ( tiles.length ) assert.equal( quads[1], tile );
	}
});

test("explicit source fitting transforms oversized tiles, masks, controls and blocks together", () => {
	const screen = /** @type {const} */ ([ 0, 0, 400, 300 ]);
	const sourceBounds = /** @type {const} */ ([ 100, 50, 160, 160 ]);
	const aperture = /** @type {const} */ ([ 116, 66, 128, 128 ]);
	const prefix = quad( [ 1, 2, 3, 4 ], screen );
	const tile = Object.freeze( {
		...quad( [ -60, -110, 480, 480 ], aperture ),
		mask: { texture: "mask", rect: aperture }
	} );
	const quads = [ prefix, quad( sourceBounds, screen ), tile ];
	/** @type {UiControl[]} */
	const controls = [ { id: "minimap-in", label: "Zoom in", kind: "button", rect: [ 240, 60, 16, 16 ] } ];
	/** @type {UiRect[]} */
	const blocks = [ sourceBounds, screen ];
	fitUiGroup( quads, controls, 1, 0, [ 0, 0, 80, 80 ], screen, { sourceBounds, blocks } );
	assert.equal( quads[0], prefix );
	assert.deepEqual( quads[1].rect, [ 0, 0, 80, 80 ] );
	assert.deepEqual( quads[2].rect, [ -80, -80, 240, 240 ] );
	assert.deepEqual( quads[2].clip, [ 8, 8, 64, 64 ] );
	assert.deepEqual( quads[2].mask?.rect, quads[2].clip );
	assert.deepEqual( controls[0].rect, [ 70, 5, 8, 8 ] );
	assert.deepEqual( blocks[0], quads[1].rect );
	assert.equal( blocks[1], screen );
	assert.deepEqual( tile.rect, [ -60, -110, 480, 480 ] );
});

test("copied viewport clips stay on screen for text and its fitted replacement", () => {
	const screen = /** @type {const} */ ([ 0, 0, 200, 150 ]);
	const copiedScreen = Object.freeze( /** @type {UiRect} */ ([ ...screen ]) );
	const fitted = textRunQuad( [ quad( [ 440, 80, 6, 10 ], copiedScreen ) ] );
	const run = textRunQuad( [ quad( [ 440, 80, 12, 10 ], copiedScreen ) ] );
	assert.ok( fitted && run );
	/** @type {UiQuad[]} */
	const quads = [
		quad( [ 100, 50, 400, 200 ], screen ),
		{ ...run, textLayout: { box: [ 440, 80, 6, 10 ], fitted } },
		quad( [ 440, 80, 12, 10 ], [ 400, 50, 100, 100 ] )
	];
	fitUiGroup( quads, [], 0, 0, screen, screen );
	assert.deepEqual( quads[1].rect, [ 170, 65, 6, 5 ] );
	assert.equal( quads[1].clip, screen, "the old transformed viewport ended at x=50 and hid this label" );
	assert.equal( quads[1].textLayout?.fitted?.clip, screen );
	assert.deepEqual( quads[2].clip, [ 150, 50, 50, 50 ], "local clips still follow their content" );
	assert.deepEqual( copiedScreen, [ 0, 0, 200, 150 ] );
});

test("only shrinking nearest bitmap runs uses linear sampling, including replacement text", () => {
	const screen = /** @type {const} */ ([ 0, 0, 400, 300 ]);
	const glyph = { ...quad( [ 110, 110, 6, 10 ], screen ), sampling: /** @type {const} */ ("nearest") };
	const run = textRunQuad( [ glyph ] );
	assert.ok( run );
	const sourceBounds = /** @type {const} */ ([ 100, 100, 200, 100 ]);
	for ( const scale of [ 1, 0.5 ] ) {
		/** @type {UiQuad[]} */
		const quads = [
			{ ...quad( sourceBounds, screen ), sampling: "nearest" },
			{ ...run, textLayout: { box: [ 110, 110, 6, 10 ], fitted: run } },
			{ ...run, sampling: "linear" }
		];
		fitUiGroup( quads, [], 0, 0, [ 0, 0, 200 * scale, 100 * scale ], screen, { sourceBounds } );
		assert.equal( quads[0].sampling, "nearest", "artwork sampling is unchanged" );
		assert.equal( quads[1].sampling, scale < 1 ? "linear" : "nearest" );
		assert.equal( quads[1].textLayout?.fitted?.sampling, quads[1].sampling );
		assert.equal( quads[2].sampling, "linear" );
		assert.deepEqual( expandTextRuns( [ quads[1] ] )[0].rect, [ 10 * scale, 10 * scale, 6 * scale, 10 * scale ] );
		assert.deepEqual( quads[1].run?.glyphs[0].uv, run.run?.glyphs[0].uv );
	}
	assert.equal( run.sampling, "nearest", "the native cached run stays reusable on desktop" );
});
