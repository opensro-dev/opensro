/*
===========================================================================

particle-holder-link.ts - linked EFP elements ride their holder every frame

Native CEFEffect_Render (B153A0, IEasyFxEffect slot 1) takes the holder's
turn and move since its last draw, inverse(previous) x current, and runs
CEFEffect_UpdateElementMatrices (AF13D0) over the element tree on every
draw: an element linked to its holder (follow +0x68, position, matrix
+0x6C) moves with it at the frame rate, not only on the 50 ms effect tick.
The particle graph ticks at 20 Hz (particle-graph.ts), so presentation
applies the holder's motion since the graph's last tick to the elements
whose links reach the holder. Drawing them by their last tick velocity
instead, which carried the holder's irregular per-tick steps, made auras
shake and lag behind a moving or turning character.

Presentation only: the tick state is unchanged, and at the next tick the
graph lands where the follow carried the element.

===========================================================================
*/

// rootLink bits, also record flag bits (particle-records.ts). AF13D0
// applies each link of an element in turn: TURN, the position link (+0x68),
// turns the position about the ancestor's previous position; TRANSLATE,
// the follow link (+0x64), moves it by the ancestor's move; MATRIX, the
// matrix link (+0x6C), turns the element frame. A link carries the holder
// when its ancestor is the holder (the graph root) or carries it itself.
export const LINK_TRANSLATE = 4;
export const LINK_TURN = 8;
export const LINK_MATRIX = 16;

// Floats of one holder follow: the holder's turn since the graph's last
// tick (a 4x4 whose translation is the holder's move since then), then the
// holder's position at that tick and a pad.
export const HOLDER_FOLLOW = 20;
export const FOLLOW_PIVOT = 16;

/*
================
LinkNode

What the link rule reads of a graph element: its holder links.
================
*/
export interface LinkNode {
	rootLink: number;
}

/*
================
rootLinkFlags

The holder links one element carries, from the ancestors its own links
name (undefined when the depth names none). A follow link carries the
holder's move when its ancestor is the holder or moves with it; a position
or matrix link carries the holder's turn when its ancestor is the holder
or its frame turns with it.
================
*/
export function rootLinkFlags(
	position: LinkNode | undefined,
	follow: LinkNode | undefined,
	matrix: LinkNode | undefined,
	root: LinkNode
): number {
	let flags = 0;
	if ( position && (position === root || (position.rootLink & LINK_MATRIX) !== 0) ) flags |= LINK_TURN;
	if ( follow && (follow === root || (follow.rootLink & LINK_TRANSLATE) !== 0) ) flags |= LINK_TRANSLATE;
	if ( matrix && (matrix === root || (matrix.rootLink & LINK_MATRIX) !== 0) ) flags |= LINK_MATRIX;
	return flags;
}

/*
================
holderFollow

The holder's motion since the graph's last tick, written at out[offset]:
its turn, current x inverse(tick) of the 3x3s (rootMatrix holds the tick
holder's), with its move since the tick (transform's translation less
rootPosition) as the translation, then rootPosition, the pivot of the
turn. Doubles throughout: only presentation reads it.
================
*/
export function holderFollow(
	rootMatrix: ArrayLike<number>,
	rootPosition: ArrayLike<number>,
	transform: ArrayLike<number>,
	out: Float32Array,
	offset = 0
): void {
	const a = rootMatrix[0]!,
		b = rootMatrix[4]!,
		c = rootMatrix[8]!,
		d = rootMatrix[1]!,
		e = rootMatrix[5]!,
		f = rootMatrix[9]!,
		g = rootMatrix[2]!,
		h = rootMatrix[6]!,
		i = rootMatrix[10]!;
	const det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g);
	// A degenerate tick holder has no turn to carry: identity.
	const singular = Math.abs( det ) < 1e-12;
	// Column-major inverse: column 0 is (i00, i10, i20) and so on.
	const inverse0 = singular ? 1 : (e * i - f * h) / det,
		inverse1 = singular ? 0 : (f * g - d * i) / det,
		inverse2 = singular ? 0 : (d * h - e * g) / det,
		inverse3 = singular ? 0 : (c * h - b * i) / det,
		inverse4 = singular ? 1 : (a * i - c * g) / det,
		inverse5 = singular ? 0 : (b * g - a * h) / det,
		inverse6 = singular ? 0 : (b * f - c * e) / det,
		inverse7 = singular ? 0 : (c * d - a * f) / det,
		inverse8 = singular ? 1 : (a * e - b * d) / det;
	for ( let row = 0; row < 3; row++ ) {
		const t0 = singular ? (row === 0 ? 1 : 0) : transform[row]!,
			t1 = singular ? (row === 1 ? 1 : 0) : transform[4 + row]!,
			t2 = singular ? (row === 2 ? 1 : 0) : transform[8 + row]!;
		out[offset + row] = t0 * inverse0 + t1 * inverse1 + t2 * inverse2;
		out[offset + 4 + row] = t0 * inverse3 + t1 * inverse4 + t2 * inverse5;
		out[offset + 8 + row] = t0 * inverse6 + t1 * inverse7 + t2 * inverse8;
		out[offset + 12 + row] = transform[12 + row]! - rootPosition[row]!;
		out[offset + FOLLOW_PIVOT + row] = rootPosition[row]!;
	}
	out[offset + 3] = out[offset + 7] = out[offset + 11] = 0;
	out[offset + 15] = 1;
	out[offset + FOLLOW_PIVOT + 3] = 0;
}

/*
================
rootTickShift

The part of an element's last tick motion its holder links caused: the
holder's last tick turn (delta about origin) and move (origin to
position) applied to the element's start-of-tick position, less that
position. The follow carries the holder from the tick on, so the drawn
extrapolation keeps only the element's own motion.
================
*/
export function rootTickShift(
	flags: number,
	previous: ArrayLike<number>,
	delta: ArrayLike<number>,
	origin: ArrayLike<number>,
	position: ArrayLike<number>,
	out: number[]
): void {
	const x = previous[0]! - origin[0]!, y = previous[1]! - origin[1]!, z = previous[2]! - origin[2]!;
	for ( let axis = 0; axis < 3; axis++ ) {
		let shift = 0;
		if ( flags & LINK_TURN ) {
			shift += delta[axis]! * x + delta[4 + axis]! * y + delta[8 + axis]! * z + origin[axis]! - previous[axis]!;
		}
		if ( flags & LINK_TRANSLATE ) shift += position[axis]! - origin[axis]!;
		out[axis] = shift;
	}
}

/*
================
followMatrix

Carry one drawn element matrix (at out[offset]) by its holder links, in
AF13D0's order: the position turned about the holder's tick position,
then moved by the holder's move, and the frame turned. The particle
shader mirrors this.
================
*/
export function followMatrix(
	flags: number,
	follow: ArrayLike<number>,
	followAt: number,
	out: Float32Array,
	offset: number
): void {
	if ( flags & LINK_TURN ) {
		const x = out[offset + 12]! - follow[followAt + FOLLOW_PIVOT]!,
			y = out[offset + 13]! - follow[followAt + FOLLOW_PIVOT + 1]!,
			z = out[offset + 14]! - follow[followAt + FOLLOW_PIVOT + 2]!;
		for ( let row = 0; row < 3; row++ ) {
			out[offset + 12 + row] = follow[followAt + row]! * x + follow[followAt + 4 + row]! * y +
				follow[followAt + 8 + row]! * z + follow[followAt + FOLLOW_PIVOT + row]!;
		}
	}
	if ( flags & LINK_TRANSLATE ) {
		for ( let row = 0; row < 3; row++ ) out[offset + 12 + row]! += follow[followAt + 12 + row]!;
	}
	if ( flags & LINK_MATRIX ) {
		for ( let column = 0; column < 3; column++ ) {
			const x = out[offset + column * 4]!, y = out[offset + column * 4 + 1]!, z = out[offset + column * 4 + 2]!;
			for ( let row = 0; row < 3; row++ ) {
				out[offset + column * 4 + row] = follow[followAt + row]! * x + follow[followAt + 4 + row]! * y +
					follow[followAt + 8 + row]! * z;
			}
		}
	}
}
