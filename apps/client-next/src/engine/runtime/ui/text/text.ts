/*
===========================================================================

text.ts - the UI's bitmap font: measurement and glyph layout

Owns font admission and two bounded memos, run widths and glyph layouts,
so labels that repeat frame to frame are not laid out again. Each local
layout retains one immutable absolute placement; callers own their lists.
A memo lookup allocates nothing: a HUD rebuild makes some hundreds of them
(#3563), and a joined-string key cost more than the hit saved.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { UiRect, UiQuad } from "@/engine/contracts/ui";
import { decodeUiFont, titleText, titleTextBox, drawableGlyph } from "@/engine/foundation/rendering/ui-glyphs";
import type { FontAtlas, GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import { guideContent, type GuideBlock, type GuideToken } from "@/engine/foundation/ui/guide-content";

const FONT_BYTE_LIMIT = 4 << 20;
const BOARD_EXTRA_HEIGHT = 5;
const MAX_WIDTHS = 2048;
const MAX_LAYOUTS = 4096;
// Key fields of the two memos (TextMemo.probe slots).
const WIDTH_FIELDS = 2;
const LAYOUT_FIELDS = 18;
// Memo hashing: FNV-1a over 32-bit words. A number contributes its value
// scaled to 1/65536 px and truncated, so values that compare equal (0 and
// -0) hash equal; finer differences only collide, and collisions compare
// every field.
const HASH_SEED = 0x811C9DC5;
const HASH_PRIME = 0x01000193;
const HASH_SCALE = 65536;
const HASH_NAN = 0x7FC00000;
const HASH_UNDEFINED = 0x2F;

/*
================
UiTextPlacement

One placement per local layout. Input vectors are owned snapshots, so edits
to a caller's arrays cannot turn an old placement into a cache hit.
================
*/
interface UiTextPlacement {
	x: number;
	y: number;
	box: UiRect;
	clip: UiRect;
	quad: UiQuad;
}

/*
================
createUiText

Bitmap metrics own measurement and drawing. No browser font or second
raster atlas can diverge from selection/caret geometry.
================
*/
export function createUiText( assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">, base: string ) {
	// Owned by this font admission; bounded storage survives idle HUD frames.
	// Widths evict first in, first out; layouts evict the least recently used.
	const widths = createTextMemo<number>( MAX_WIDTHS, WIDTH_FIELDS, false ),
		layouts = createTextMemo<UiQuad | null>( MAX_LAYOUTS, LAYOUT_FIELDS, true );
	const placements = new WeakMap<UiQuad, UiTextPlacement>();
	let state: { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; font: FontAtlas; } | {
		kind: "failed";
		message: string;
	} | { kind: "disposed"; } = { kind: "idle" };
	return {
		/*
		================
		step
		================
		*/
		step() {
			if ( state.kind === "loading" ) {
				const result = assets.take( state.id );
				if ( result ) {
					try {
						if ( result.kind !== "bytes" ) throw Error( "Retail font resource unavailable" );
						clearTextMemo( layouts );
						state = {
							kind: "ready",
							font: decodeUiFont(
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) )
							)
						};
					} catch ( error ) {
						state = { kind: "failed", message: String( error ) };
					}
					return true;
				}
			}
			if ( state.kind === "idle" && assets.available() > 0 ) {
				state = {
					kind: "loading",
					id: assets.request(
						new URL( "/assets/fonts/native-ui-font-atlas.json", base ).href,
						FONT_BYTE_LIMIT
					)
				};
			}
			return false;
		},
		/*
		================
		height
		================
		*/
		height: () => state.kind === "ready" ? state.font.fonts["0"]!.recordHeight : 0,
		/*
		================
		boardHeight
		================
		*/
		boardHeight: () => state.kind === "ready" ? state.font.fonts["0"]!.recordHeight + BOARD_EXTRA_HEIGHT : 0,
		/*
		================
		extentHeight
		================
		*/
		extentHeight: ( index = 0, style = 0 ) => {
			if ( state.kind !== "ready" ) return 0;
			const font = state.font.fonts[String( index ) + (style === 2 ? ":2" : "")]!;
			return font.ascent + font.descent;
		},
		/*
		================
		lineInk

		Where a line's letters fall in a rect it is laid at the top of
		(vAlign 0): from the cell's height above the baseline (the
		baseline is the rect's top plus the ascent) to the descent below
		it. The label wrap spaces its two lines by this (frame.ts).
		================
		*/
		lineInk: ( index = 0, style = 0 ) => {
			if ( state.kind !== "ready" ) return { top: 0, height: 0 };
			const font = state.font.fonts[String( index ) + (style === 2 ? ":2" : "")]!;
			return { top: font.ascent - font.recordHeight, height: font.recordHeight + font.descent };
		},
		/*
		================
		path
		================
		*/
		path: () => state.kind === "ready" ? state.font.image : null,
		/*
		================
		guide
		================
		*/
		guide(
			tokens: readonly GuideToken[],
			rect: UiRect,
			clip: UiRect,
			color: UiQuad["color"],
			size: ( path: string ) => readonly [number, number] | undefined,
			block?: GuideBlock
		) {
			return state.kind === "ready" ?
				guideContent( state.font, tokens, rect, clip, color, size, block ) :
				{ quads: [], paths: [], height: 0 };
		},
		/*
		================
		error
		================
		*/
		error: () => state.kind === "failed" ? state.message : null,
		/*
		================
		run
		================
		*/
		run( value: string, fontStyle = 0, fontIndex = 0 ) {
			if ( state.kind !== "ready" ) return { width: 0 };
			widths.probe[0] = fontIndex;
			widths.probe[1] = fontStyle;
			const cached = findTextMemo( widths, value );
			if ( cached ) return { width: cached.result };
			const font = state.font.fonts[String( fontIndex ) + (fontStyle === 2 ? ":2" : "")]!;
			let width = 0;
			for ( const c of value ) width += drawableGlyph( font.glyphs, c )?.advanceX ?? 0;
			addTextMemo( widths, value, width );
			return { width };
		},
		/*
		================
		quads

		Labels repeat frame to frame; glyph layout walks every character. The
		memo is keyed by position modulo whole pixels: layout is exactly
		translation-invariant for an integer shift (Math.round( n + f ) is
		n + Math.round( f ); widths, offsets and clip sizes are unchanged), so a
		label that moves with the minimap or a character is laid out once and
		shifted after. Placed quads own frozen vectors; every caller gets its own list.
		================
		*/
		quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style: GlyphStyle = {} ) {
			if ( state.kind !== "ready" ) return [];
			const nx = Math.floor( rect[0] ), ny = Math.floor( rect[1] );
			const rx = rect[0] - nx, ry = rect[1] - ny, cx = clip[0] - nx, cy = clip[1] - ny;
			// The key is the old joined key's fields, value by value: the local
			// rect and clip, the colour and the style.
			const key = layouts.probe;
			key[0] = rx;
			key[1] = ry;
			key[2] = rect[2];
			key[3] = rect[3];
			key[4] = cx;
			key[5] = cy;
			key[6] = clip[2];
			key[7] = clip[3];
			key[8] = color[0];
			key[9] = color[1];
			key[10] = color[2];
			key[11] = color[3];
			key[12] = style.fontIndex;
			key[13] = style.fontStyle;
			key[14] = style.hAlign;
			key[15] = style.vAlign;
			key[16] = style.overflow;
			key[17] = style.label ? 1 : 0;
			const entry = findTextMemo( layouts, value );
			let cached: UiQuad | null;
			if ( entry ) {
				cached = entry.result;
			} else {
				const localRect: UiRect = [ rx, ry, rect[2], rect[3] ],
					localClip: UiRect = [ cx, cy, clip[2], clip[3] ];
				cached = titleText( state.font, value, localRect, localClip, color, style )[0] ?? null;
				addTextMemo( layouts, value, cached );
			}
			if ( !cached ) return [];
			const placed = placements.get( cached );
			if (
				placed && Object.is( placed.x, nx ) && Object.is( placed.y, ny ) &&
				sameRect( placed.box, rect ) && sameRect( placed.clip, clip ) &&
				sameRect( placed.quad.color, cached.color )
			) return emitPlacement( placed.quad );
			const quad = freezePlacement( shiftRun( cached, nx, ny, rect, clip, style.overflow ?? "avoid-overlap" ) );
			placements.set( cached, {
				x: nx,
				y: ny,
				box: Object.freeze( [ ...rect ] ) as UiRect,
				clip: Object.freeze( [ ...clip ] ) as UiRect,
				quad
			} );
			return emitPlacement( quad );
		},
		/*
		================
		box
		================
		*/
		box( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style: GlyphStyle = {} ) {
			return state.kind === "ready" ? titleTextBox( state.font, value, rect, clip, color, style ) : [];
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			clearTextMemo( widths );
			clearTextMemo( layouts );
			if ( state.kind === "loading" ) assets.cancel( state.id );
			state = { kind: "disposed" };
		}
	};
}

// ============================================================================

/*
================
TextKeyField

One scalar of a memo key. The old joined-string key wrote a number as
String( n ), which is one-to-one up to SameValueZero (0 and -0 both "0",
NaN "NaN"), and undefined as "", which no number writes.
================
*/
type TextKeyField = number | string | undefined;

/*
================
TextMemoEntry

One memoized result: its key (the string and a snapshot of the fields), its
hash bucket chain and its place in the memo's age list.
================
*/
interface TextMemoEntry<T> {
	readonly hash: number;
	readonly text: string;
	readonly fields: readonly TextKeyField[];
	readonly result: T;
	chain: TextMemoEntry<T> | null;
	older: TextMemoEntry<T> | null;
	newer: TextMemoEntry<T> | null;
}

/*
================
TextMemo

A bounded memo keyed by one string and a fixed number of scalar fields, with
no allocation on a lookup: the caller writes a lookup's fields into probe,
then calls findTextMemo and, on a miss, addTextMemo. Fields compare by
SameValueZero, so two keys share an entry exactly when their joined strings
would have been equal: the joined key's trailing pieces held no separator,
so it read back to one key. The age list runs oldest to newest; with recency
a hit moves to the newest end (least recently used is evicted), without it
the first entry in is the first out. limit bounds the entry count.
================
*/
interface TextMemo<T> {
	readonly limit: number;
	readonly recency: boolean;
	readonly probe: TextKeyField[];
	readonly buckets: Map<number, TextMemoEntry<T>>;
	size: number;
	oldest: TextMemoEntry<T> | null;
	newest: TextMemoEntry<T> | null;
}

/*
================
createTextMemo
================
*/
function createTextMemo<T>( limit: number, fields: number, recency: boolean ): TextMemo<T> {
	return {
		limit,
		recency,
		probe: new Array<TextKeyField>( fields ).fill( undefined ),
		buckets: new Map(),
		size: 0,
		oldest: null,
		newest: null
	};
}

/*
================
hashTextKey

FNV-1a over the string's UTF-16 units, then one word per field. Fields equal
by SameValueZero hash equal (-0 * HASH_SCALE | 0 is 0).
================
*/
function hashTextKey( text: string, fields: readonly TextKeyField[] ): number {
	let hash = HASH_SEED;
	for ( let i = 0; i < text.length; i++ ) hash = Math.imul( hash ^ text.charCodeAt( i ), HASH_PRIME );
	for ( let i = 0; i < fields.length; i++ ) {
		const field = fields[i];
		let word: number;
		if ( typeof field === "number" ) {
			word = field === field ? (field * HASH_SCALE) | 0 : HASH_NAN;
		} else if ( typeof field === "string" ) {
			word = field.length;
			for ( let j = 0; j < field.length; j++ ) word = Math.imul( word ^ field.charCodeAt( j ), HASH_PRIME );
		} else {
			word = HASH_UNDEFINED;
		}
		hash = Math.imul( hash ^ word, HASH_PRIME );
	}
	return hash;
}

/*
================
sameTextKeyFields

SameValueZero per field: === except that NaN equals NaN.
================
*/
function sameTextKeyFields( a: readonly TextKeyField[], b: readonly TextKeyField[] ): boolean {
	for ( let i = 0; i < a.length; i++ ) {
		const x = a[i], y = b[i];
		if ( x !== y && (x === x || y === y) ) return false;
	}
	return true;
}

/*
================
findTextMemo

The entry for text and the probe's fields, or null. With recency a hit
becomes the newest entry.
================
*/
function findTextMemo<T>( memo: TextMemo<T>, text: string ): TextMemoEntry<T> | null {
	const hash = hashTextKey( text, memo.probe );
	let entry = memo.buckets.get( hash ) ?? null;
	while ( entry && (entry.text !== text || !sameTextKeyFields( entry.fields, memo.probe )) ) entry = entry.chain;
	if ( !entry || !memo.recency || entry === memo.newest ) return entry;
	// Unlink, then append at the newest end.
	if ( entry.older ) entry.older.newer = entry.newer;
	else memo.oldest = entry.newer;
	entry.newer!.older = entry.older;
	entry.older = memo.newest;
	entry.newer = null;
	memo.newest!.newer = entry;
	memo.newest = entry;
	return entry;
}

/*
================
addTextMemo

Adds the result for text and the probe's fields, which findTextMemo just
missed. A full memo first evicts its oldest entry.
================
*/
function addTextMemo<T>( memo: TextMemo<T>, text: string, result: T ): void {
	if ( memo.size >= memo.limit && memo.oldest ) evictTextMemo( memo, memo.oldest );
	const hash = hashTextKey( text, memo.probe );
	const entry: TextMemoEntry<T> = {
		hash,
		text,
		fields: memo.probe.slice(),
		result,
		chain: memo.buckets.get( hash ) ?? null,
		older: memo.newest,
		newer: null
	};
	memo.buckets.set( hash, entry );
	if ( memo.newest ) memo.newest.newer = entry;
	else memo.oldest = entry;
	memo.newest = entry;
	memo.size++;
}

/*
================
evictTextMemo
================
*/
function evictTextMemo<T>( memo: TextMemo<T>, entry: TextMemoEntry<T> ): void {
	let link = memo.buckets.get( entry.hash )!;
	if ( link === entry ) {
		if ( entry.chain ) memo.buckets.set( entry.hash, entry.chain );
		else memo.buckets.delete( entry.hash );
	} else {
		while ( link.chain !== entry ) link = link.chain!;
		link.chain = entry.chain;
	}
	if ( entry.older ) entry.older.newer = entry.newer;
	else memo.oldest = entry.newer;
	if ( entry.newer ) entry.newer.older = entry.older;
	else memo.newest = entry.older;
	memo.size--;
}

/*
================
clearTextMemo
================
*/
function clearTextMemo<T>( memo: TextMemo<T> ): void {
	memo.buckets.clear();
	memo.size = 0;
	memo.oldest = null;
	memo.newest = null;
}

/*
================
shiftRun

A cached run quad moved by a whole-pixel offset, frozen (the text owner's
contract: no caller can change a quad another holds). Its clip is formed
again from the caller's own rectangle with layoutGlyphs' expression (a
difference taken in local coordinates rounds its last bit differently):
the caller's clip itself when the text avoids overlap, else its
intersection with the box. The shortened variant is always clipped to the
intersection; the wrapped one keeps the caller's clip, as its lines stay
inside the box's columns. The layout box is the caller's own rectangle.
================
*/
function shiftRun(
	run: UiQuad,
	dx: number,
	dy: number,
	box: UiRect,
	clip: UiRect,
	overflow: NonNullable<GlyphStyle["overflow"]>
): UiQuad {
	const available = Math.max( 0, box[2] ),
		left = Math.max( clip[0], box[0] ),
		right = Math.min( clip[0] + clip[2], box[0] + available );
	const bounded: UiRect = [ left, clip[1], Math.max( 0, right - left ), clip[3] ];
	/*
	================
	moved
	================
	*/
	const moved = ( quad: UiQuad, glyphClip: UiRect ): UiQuad => ({
		...quad,
		rect: [ quad.rect[0] + dx, quad.rect[1] + dy, quad.rect[2], quad.rect[3] ],
		clip: glyphClip
	});
	const shifted = moved( run, overflow === "avoid-overlap" ? clip : bounded );
	if ( !run.textLayout ) return Object.freeze( shifted );
	const { fitted, wrapped } = run.textLayout;
	return Object.freeze( {
		...shifted,
		textLayout: {
			box,
			fitted: fitted ? Object.freeze( moved( fitted, bounded ) ) : fitted,
			...(wrapped ? { wrapped: Object.freeze( moved( wrapped, clip ) ) } : {}),
			...(run.textLayout.label ? { label: true } : {})
		}
	} );
}

/*
================
sameRect

Object.is preserves signed zero and compares scalar snapshots rather than
caller vector identity.
================
*/
function sameRect( a: UiRect, b: UiRect ): boolean {
	return Object.is( a[0], b[0] ) && Object.is( a[1], b[1] ) &&
		Object.is( a[2], b[2] ) && Object.is( a[3], b[3] );
}

/*
================
freezePlacement

A shared placement owns every mutable vector it exposes. The glyph run is
already deeply frozen by textRunQuad; no caller storage is frozen here.
================
*/
function freezePlacement( quad: UiQuad ): UiQuad {
	const paint = {
		...quad,
		rect: Object.freeze( [ ...quad.rect ] ) as UiRect,
		clip: Object.freeze( [ ...quad.clip ] ) as UiRect,
		color: Object.freeze( [ ...quad.color ] ) as UiRect,
		uv: Object.freeze( [ ...quad.uv ] ) as UiRect
	};
	if ( !quad.textLayout ) return Object.freeze( paint );
	const layout = quad.textLayout;
	return Object.freeze( {
		...paint,
		textLayout: Object.freeze( {
			...layout,
			box: Object.freeze( [ ...layout.box ] ) as UiRect,
			fitted: layout.fitted ? freezePlacement( layout.fitted ) : layout.fitted,
			...(layout.wrapped ? { wrapped: freezePlacement( layout.wrapped ) } : {})
		} )
	} );
}

/*
================
emitPlacement

Overlap resolution groups by sidecar identity. Two identical labels are
still two independent emissions, even when their placement geometry is shared.
================
*/
function emitPlacement( quad: UiQuad ): UiQuad[] {
	if ( !quad.textLayout ) return [ quad ];
	return [ Object.freeze( {
		...quad,
		textLayout: Object.freeze( { ...quad.textLayout } )
	} ) ];
}
