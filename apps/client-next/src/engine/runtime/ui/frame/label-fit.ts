/*
===========================================================================

label-fit.ts - where a fitted label or button caption goes

Port-only, not native (Experimental "Fit window labels", off is the
original): the layout half of fitting text inside its art. It chooses a
static's lines inside a measured room (labelFit), a button caption's
face and lines inside its plate (buttonCaptionFit), and the band two
wrapped lines stack in (wrappedLabelBand); it pushes only the fitted
static lines (drawLabelFit). Drawing captions through the authored text
path stays in frame.ts, which re-exports what the coordinator calls.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import type { UiAuthoredFrame } from "./frame-contract";
import { authoredClientRect, authoredRect, type AuthoredControl } from "@/engine/foundation/ui/authored-layout";
import { NARROW_FONT_INDEX } from "@/engine/foundation/rendering/ui-glyphs";
import { CAPTION_MARGIN, captionWords, fitLines } from "@/engine/foundation/ui/text-lines";

// A caption the fit had to shrink or wrap may come this close to the
// plate's edge: inside the bevel, which is about two pixels wide.
const CAPTION_TIGHT_MARGIN = 2;
// The blank between a wrapped label's two lines, below the first line's
// descent: one label reads as one, apart from the rows beside it.
const WRAP_LEADING = 1;

/*
================
wrappedLabelBand

Where a wrapped label's two lines go (the Experimental "Fit window
labels"): the top of the first line's rect, laid at its top (vAlign 0),
and the pitch to the second. A box sized for one line has no room for
two at the font's full extent (two such lines ran 8 pixels into each
neighbouring row, #3673), so the pair stacks at its ink height plus
WRAP_LEADING, and tighter, down to the ink height where the first line's
descent meets the second line's letters, when room ([above, below],
layout labelRoom) needs it. The pair's ink is centred on the box, then
moved inside the band; a pair taller than its band even at the tightest
pitch stays centred on the band. Without room the band is unbounded.
================
*/
export function wrappedLabelBand( box: UiRect, room: readonly [number, number] | undefined, ink: {
	top: number;
	height: number;
} ) {
	const [above, below] = room ?? [ Infinity, Infinity ];
	const bandTop = box[1] - above, bandBottom = box[1] + box[3] + below;
	const pitch = Math.max( ink.height, Math.min( ink.height + WRAP_LEADING, bandBottom - bandTop - ink.height ) );
	const block = pitch + ink.height;
	let inkTop = box[1] + (box[3] - block) / 2;
	if ( block <= bandBottom - bandTop ) inkTop = Math.min( Math.max( inkTop, bandTop ), bandBottom - block );
	else inkTop = bandTop + (bandBottom - bandTop - block) / 2;
	return { top: inkTop - ink.top, pitch };
}

/*
================
fittedLabel

Port-only, not native (Experimental "Fit window labels"): a static's
text laid inside room, a rect its caller measured from the art around
it, in the first of values that fitLines places there (one line, two,
the narrow face, two narrow). The lines keep the node's horizontal
alignment and centre on room's height. False, with nothing drawn, when
the row is off or no value fits: the caller draws the native static.
The lines are pushed now, so a caller whose art comes after them uses
labelFit and drawLabelFit instead (the UI pass draws in push order).
================
*/
export function fittedLabel(
	frame: UiAuthoredFrame,
	args: { node: AuthoredControl; room: UiRect; values: readonly string[]; }
): boolean {
	const fit = labelFit( frame, args );
	if ( fit ) drawLabelFit( frame, fit );
	return !!fit;
}

/*
================
LabelFit

The lines fittedLabel lays, their face and the room and node they keep.
================
*/
export interface LabelFit {
	node: AuthoredControl;
	room: UiRect;
	fontIndex: number;
	lines: readonly string[];
}

/*
================
labelFit

fittedLabel's choice, drawing nothing: undefined when the row is off
or no value fits. Lets a caller leave the native static out of art it
draws first and lay the fitted lines over that art afterwards.
================
*/
export function labelFit(
	frame: UiAuthoredFrame,
	{ node, room, values }: { node: AuthoredControl; room: UiRect; values: readonly string[]; }
): LabelFit | undefined {
	if ( !frame.fitWindowLabels() ) return undefined;
	const measure = ( line: string, face: number ) => frame.text.run( line, 0, face ).width;
	for ( const value of values ) {
		const fit = fitLines( value, room[2], node.fontIndex ?? 0, measure );
		if ( fit ) return { node, room, fontIndex: fit.fontIndex, lines: fit.lines };
	}
	return undefined;
}

/*
================
drawLabelFit

Pushes a labelFit's lines, centred on its room's height.
================
*/
export function drawLabelFit( frame: UiAuthoredFrame, { node, room, fontIndex, lines }: LabelFit ): void {
	const text = frame.text,
		style = { fontIndex, hAlign: node.hAlign, vAlign: 0 },
		lineHeight = text.extentHeight( fontIndex ),
		ink = text.lineInk( fontIndex );
	const { top, pitch } = lines.length === 1 ?
		{ top: room[1] + (room[3] - ink.height) / 2 - ink.top, pitch: 0 } :
		wrappedLabelBand( room, undefined, ink );
	lines.forEach( ( line, index ) =>
		frame.quads.push(
			...text.quads( line, [ room[0], top + index * pitch, room[2], lineHeight ], frame.full, node.color, style )
		)
	);
}

// A button caption as buttonCaptionFit lays it out.
export interface CaptionFit {
	label: string;
	fontIndex: number;
	lines: string[];
}

/*
================
buttonCaptionFit

How a button's caption is drawn inside its plate and client box. Port-only, not native
(Experimental "Fit window labels", off is the original, which draws
every caption whole in its own face and lets it run past the plate,
#3675). A caption wider than its room is, in order of preference:

  the whole caption in the narrow face, NARROW_FONT_INDEX ("Dismiss" on the party
  window's 44-pixel plate, "Block Removal", the Item Mall's "Buy Item
  Worn" in its 77-pixel client box);
  the whole caption on two lines inside the plate (fitLines), in its
  own face and then the narrow one ("Block" / "Registration" on an
  88 x 24 plate);
  the leading words that fit ("Delete message" -> "Delete", "Check for
  duplicate entries" -> "Check").

The first two keep every word, which matters where two buttons share a
leading word ("Block Registration" beside "Block Removal").
================
*/
export function buttonCaptionFit(
	frame: UiAuthoredFrame,
	{ node, ox, oy, caption }: { node: AuthoredControl; ox: number; oy: number; caption: string; }
): CaptionFit {
	const own = node.fontIndex, whole = { label: caption, fontIndex: own, lines: [ caption ] };
	if ( !frame.fitWindowLabels() || caption === "" ) return whole;
	// The caption keeps to its client box (the Item Mall's category
	// buttons inset it past their icon) and inside the plate's bevel.
	const plate = authoredRect( node, ox, oy ), box = authoredClientRect( node, ox, oy ), text = frame.text;
	const comfortable = Math.min( box[2], plate[2] - 2 * CAPTION_MARGIN ),
		room = Math.min( box[2], plate[2] - 2 * CAPTION_TIGHT_MARGIN );
	if ( text.run( caption, 0, own ).width <= comfortable ) return whole;
	const measure = ( line: string, fontIndex: number ) => text.run( line, 0, fontIndex ).width;
	if ( measure( caption, NARROW_FONT_INDEX ) <= room ) {
		return { label: caption, fontIndex: NARROW_FONT_INDEX, lines: [ caption ] };
	}
	// Two lines, own face first (fitLines); on a plate the pair must also
	// fit its height. The pair's ink may fill it: a line's ink counts its
	// descent, which most captions leave empty.
	for ( const face of [ own, NARROW_FONT_INDEX ] ) {
		const fit = fitLines( caption, room, face, measure );
		if ( fit?.lines.length === 2 && 2 * text.lineInk( fit.fontIndex ).height + WRAP_LEADING <= plate[3] ) {
			return { label: caption, fontIndex: fit.fontIndex, lines: fit.lines };
		}
	}
	const words = captionWords( caption, comfortable, line => text.run( line, 0, own ).width );
	return { label: words, fontIndex: own, lines: [ words ] };
}
