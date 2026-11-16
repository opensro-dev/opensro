/*
===========================================================================

ui-text-placement.test.mjs - exact retained placements across UI frames

Compares the real UI step with an injected ungated text owner. Direct text
frames also check cache reuse, input edits, eviction and overlap identities,
and that each field of the memo keys selects its own entry.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { registerHooks } from "node:module";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { createReferenceUiText } from "../helpers/ui-text-placement-reference.mjs";
const { createUiText } = await import( "../../src/engine/runtime/ui/text/text.ts" );
const { resolveTextOverlaps } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );
const FRAME_COUNT = 120, MAX_LAYOUTS = 4096, PEER_COUNT = 32;
const atlas = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json" );

/*
================
textFixture
================
*/
function textFixture( factory ) {
	const owner = factory( {
		available: () => 1,
		request: () => 1,
		take: () => ({
			kind: "bytes",
			id: 1,
			buffer: atlas.buffer.slice( atlas.byteOffset, atlas.byteOffset + atlas.byteLength )
		}),
		cancel() {}
	}, "https://fixture.invalid/" );
	owner.step();
	assert.equal( owner.step(), true );
	return owner;
}

test("text frames retain geometry and match ungated placement on every input change", () => {
	const a = textFixture( createUiText ), b = textFixture( createReferenceUiText );
	const box = /** @type {[number, number, number, number]} */ ([ 10.25, 20.5, 24, 22 ]),
		clip = /** @type {[number, number, number, number]} */ ([ 0, 0, 800, 600 ]),
		color = /** @type {[number, number, number, number]} */ ([ 1, 1, 1, 1 ]);
	let previous, hits = 0;
	try {
		for ( let frame = 0; frame < FRAME_COUNT; frame++ ) {
			a.step();
			b.step();
			if ( frame === 20 ) box[0] += 1;
			if ( frame === 30 ) box[1] += .25;
			if ( frame === 40 ) box[2] += 4;
			if ( frame === 50 ) box[3] += 1;
			if ( frame === 60 ) clip[0] += .1;
			if ( frame === 70 ) clip[2] -= 7;
			if ( frame === 80 ) color[1] = .5;
			const style = /** @type {import("../../src/engine/foundation/rendering/ui-glyphs").GlyphStyle} */ ({
				overflow: frame >= 90 ? "clip" : "avoid-overlap",
				fontIndex: frame >= 100 ? 2 : 0,
				hAlign: frame >= 110 ? 1 : 0
			});
			const actual = a.quads( "Inventory", box, clip, color, style ),
				expected = b.quads( "Inventory", box, clip, color, style );
			assert.deepStrictEqual( actual, expected, "frame " + frame );
			assert.deepStrictEqual( resolveTextOverlaps( actual ), resolveTextOverlaps( expected ) );
			if ( previous && actual[0].rect === previous[0].rect ) hits++;
			previous = actual;
		}
		assert.ok( hits > FRAME_COUNT / 2, "unchanged frames reuse geometry" );
		const first = a.quads( "Inventory", box, clip, color );
		const second = a.quads( "Inventory", [ ...box ], [ ...clip ], [ ...color ] );
		assert.notEqual( first, second );
		assert.equal( first[0].rect, second[0].rect );
		assert.notEqual( first[0].textLayout, second[0].textLayout );
		assert.throws( () => {
			first[0].rect[0] = 999;
		}, TypeError );
		assert.equal( Object.isFrozen( box ), false );
		for ( let i = 0; i <= MAX_LAYOUTS; i++ ) {
			a.quads( "Label " + i, box, clip, color );
			b.quads( "Label " + i, box, clip, color );
		}
		assert.deepStrictEqual(
			a.quads( "Inventory", box, clip, color ),
			b.quads( "Inventory", box, clip, color )
		);
		for ( const overflow of [ "avoid-overlap", "clip", "ellipsis" ] ) {
			for ( const x of [ 0, -0, -.5, .5, .1 + .2 ] ) {
				const rect = [ x, -.1, 24, 22 ],
					style = /** @type {import("../../src/engine/foundation/rendering/ui-glyphs").GlyphStyle} */ ({
						overflow
					});
				const actual = [], expected = [];
				for (
					const [value, bounds] of [
						[ "Inventory", rect ],
						[ "Inventory", rect ],
						[ "48", [ x + 25, -.1, 30, 22 ] ],
						[ "", rect ]
					]
				) {
					actual.push( ...a.quads( value, bounds, clip, color, style ) );
					expected.push( ...b.quads( value, bounds, clip, color, style ) );
				}
				assert.deepStrictEqual( resolveTextOverlaps( actual ), resolveTextOverlaps( expected ) );
			}
		}
	} finally {
		a.dispose();
		b.dispose();
	}
	assert.deepStrictEqual( a.quads( "Inventory", box, clip, color ), [] );
});

/*
================
referenceUiFixture

Resolve only the reference UI's font constructor to the oracle. Both UI
steps load the same shipped sources through the shared native source loader.
================
*/
async function referenceUiFixture() {
	const fixtureUrl = new URL( "../helpers/ui-fixture.mjs", import.meta.url ).href;
	const uiUrl = new URL( "../../src/engine/runtime/ui/ui.ts", import.meta.url ).href;
	const referenceUrl = new URL( "../helpers/ui-text-placement-reference.mjs", import.meta.url ).href;
	const suffix = "?ungated-text-placement";
	const hook = registerHooks( {
		/*
		================
		resolve
		================
		*/
		resolve( specifier, context, nextResolve ) {
			if ( context.parentURL === uiUrl + suffix && specifier === "./text/text" ) {
				return nextResolve( referenceUrl, context );
			}
			if ( context.parentURL === fixtureUrl + suffix && specifier.endsWith( "/ui/ui.ts" ) ) {
				return { url: uiUrl + suffix, shortCircuit: true };
			}
			return nextResolve( specifier, {
				...context,
				parentURL: context.parentURL?.endsWith( suffix ) ?
					context.parentURL.slice( 0, -suffix.length ) :
					context.parentURL
			} );
		},
		/*
		================
		load
		================
		*/
		load( url, context, nextLoad ) {
			return nextLoad( url.endsWith( suffix ) ? url.slice( 0, -suffix.length ) : url, context );
		}
	} );
	try {
		return (await import( fixtureUrl + suffix )).uiFixture;
	} finally {
		hook.deregister();
	}
}

test("real UI step publishes identical products and semantics across unchanged and changed frames", async () => {
	const referenceFixture = await referenceUiFixture();
	const a = uiFixture(), b = referenceFixture();
	for ( const f of [ a, b ] ) {
		f.state.entities = Array.from( { length: PEER_COUNT }, ( _, index ) => ({
			...f.state.entities[0],
			gid: index + 1,
			name: "Peer " + index,
			x: index * 4
		}) );
	}
	try {
		for ( let frame = 0; frame < FRAME_COUNT; frame++ ) {
			const publications = a.scenes.length;
			for ( const f of [ a, b ] ) {
				if ( frame === 30 ) {
					f.state.gameplay = {
						...f.state.gameplay,
						vitals: [ { gid: 1, hp: 80, maxHp: 100, mp: 40, maxMp: 100 } ]
					};
				}
				if ( frame === 40 ) {
					f.state.entities = f.state.entities.map( ( entity, index ) =>
						index === 0 ? { ...entity, name: "Renamed player" } : entity
					);
				}
				if ( frame === 50 ) {
					f.state.entities = f.state.entities.map( entity => ({ ...entity, x: entity.x + 1 }) );
				}
				if ( frame === 60 ) f.state.width += 20;
				if ( frame === 70 ) f.ui.event( { kind: "hover", id: "inventory" } );
				if ( frame === 80 ) f.state.session.revision++;
			}
			assert.deepStrictEqual( a.ui.step( a.state, frame * 16 ), b.ui.step( b.state, frame * 16 ) );
			assert.deepStrictEqual( a.products, b.products, "raw products frame " + frame );
			assert.deepStrictEqual( a.scenes, b.scenes, "painted products frame " + frame );
			if ( frame === 40 || frame === 60 ) assert.ok( a.scenes.length > publications, "input change publishes" );
		}
		assert.ok( a.scenes.some( scene => scene?.quads.length > 0 ) );
	} finally {
		a.dispose();
		b.dispose();
	}
});

/*
================
memo key cases

Each case changes exactly one field of the layout memo's key from the base
label, chosen so a shared entry would be visible: either the output differs
from a fresh owner's miss, or the base label's retained placement is replaced.
================
*/
const MEMO_BASE = {
	value: "Inventory",
	rect: [ 10.25, 20.5, 60, 22 ],
	clip: [ 12.5, 21.25, 80, 200 ],
	color: [ 1, .9, .8, 1 ],
	style: { fontIndex: 0, fontStyle: 0, hAlign: 1, vAlign: 1, overflow: "avoid-overlap" }
};
const MEMO_CASES = {
	value: { value: "Inventorz" },
	"rect x": { rect: [ 10.75, 20.5, 60, 22 ] },
	"rect y": { rect: [ 10.25, 20.25, 60, 22 ] },
	"rect width": { rect: [ 10.25, 20.5, 70, 22 ] },
	"rect height": { rect: [ 10.25, 20.5, 60, 30 ] },
	"clip x": { clip: [ 12.75, 21.25, 80, 200 ] },
	"clip y": { clip: [ 12.5, 21.5, 80, 200 ] },
	"clip width": { clip: [ 12.5, 21.25, 81, 200 ] },
	"clip height": { clip: [ 12.5, 21.25, 80, 201 ] },
	red: { color: [ .5, .9, .8, 1 ] },
	green: { color: [ 1, .5, .8, 1 ] },
	blue: { color: [ 1, .9, .5, 1 ] },
	alpha: { color: [ 1, .9, .8, .5 ] },
	fontIndex: { style: { ...MEMO_BASE.style, fontIndex: 2 } },
	fontStyle: { style: { ...MEMO_BASE.style, fontStyle: 2 } },
	hAlign: { style: { ...MEMO_BASE.style, hAlign: 2 } },
	vAlign: { style: { ...MEMO_BASE.style, vAlign: 2 } },
	overflow: { style: { ...MEMO_BASE.style, overflow: "clip" } }
};

/*
================
layoutOf
================
*/
function layoutOf( owner, input ) {
	return owner.quads( input.value, [ ...input.rect ], [ ...input.clip ], [ ...input.color ], { ...input.style } );
}

test("text memo keys: one differing field never shares an entry, and a hit returns what a miss computes", () => {
	const owner = textFixture( createUiText );
	try {
		for ( const [field, change] of Object.entries( MEMO_CASES ) ) {
			const variant = { ...MEMO_BASE, ...change };
			const first = layoutOf( owner, MEMO_BASE );
			assert.equal( layoutOf( owner, MEMO_BASE )[0].rect, first[0].rect, field + ": the base label hits" );
			const missed = layoutOf( owner, variant ), fresh = textFixture( createUiText );
			try {
				const computed = layoutOf( fresh, variant );
				assert.notDeepStrictEqual( computed, layoutOf( fresh, MEMO_BASE ), field + ": the field shows" );
				assert.deepStrictEqual( missed, computed, field + ": the variant misses" );
				assert.deepStrictEqual( layoutOf( owner, variant ), computed, field + ": its hit equals the miss" );
			} finally {
				fresh.dispose();
			}
			const again = layoutOf( owner, MEMO_BASE );
			assert.equal( again[0].rect, first[0].rect, field + ": the base entry kept its placement" );
		}
		// Widths: text, font slot and font style each select their own entry.
		const fresh = textFixture( createUiText );
		try {
			for (
				const [value, style, index] of [ [ "Jangan", 0, 0 ], [ "Jangam", 0, 0 ], [ "Jangan", 2, 0 ], [
					"Jangan",
					0,
					2
				] ]
			) {
				const width = owner.run( value, style, index ).width;
				assert.equal( width, fresh.run( value, style, index ).width, `${value} ${style} ${index}` );
				assert.equal( owner.run( value, style, index ).width, width );
			}
			assert.notEqual( fresh.run( "Jangan", 2, 0 ).width, fresh.run( "Jangan", 0, 0 ).width );
			assert.notEqual( fresh.run( "Jangan", 0, 2 ).width, fresh.run( "Jangan", 0, 0 ).width );
		} finally {
			fresh.dispose();
		}
	} finally {
		owner.dispose();
	}
});

test("text layout memo holds MAX_LAYOUTS entries and evicts the least recently used", () => {
	const owner = textFixture( createUiText );
	const clip = [ 0, 0, 800, 600 ], color = [ 1, 1, 1, 1 ];
	const label = () => owner.quads( "Inventory", [ 10, 20, 60, 22 ], clip, color )[0].rect;
	const fill = ( count, prefix ) => {
		for ( let i = 0; i < count; i++ ) owner.quads( prefix + i, [ 10, 20, 60, 22 ], clip, color );
	};
	try {
		const first = label();
		fill( MAX_LAYOUTS - 1, "a" );
		assert.equal( label(), first, "a full memo still holds its oldest entry" );
		fill( MAX_LAYOUTS - 1, "b" );
		assert.equal( label(), first, "a hit keeps an entry from eviction" );
		fill( MAX_LAYOUTS, "c" );
		assert.notEqual( label(), first, "the least recently used entry is evicted" );
	} finally {
		owner.dispose();
	}
});
