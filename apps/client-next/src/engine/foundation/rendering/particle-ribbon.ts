/*
===========================================================================

particle-ribbon.ts - linked particle trails: point chains to camera strips

A ribbon is drawn through its emitter's live elements, newest first. The
native renderers smooth the chain (RenderLinkPipe, AF9020) or draw it raw
(LinkDPipe AF8E80, LinkObj AF73A0), then widen it into a strip facing the
camera (AF80E0).

Ribbons are rebuilt every frame, so a chain is a reusable struct of arrays
(RibbonChain) and the strip is written straight into the caller's vertex
streams: nothing here allocates per point.

===========================================================================
*/

// Coincident neighbours: AF9020 drops a point unless an axis moved more
// than this; AF8E80 and AF73A0 keep it when an axis moved this much.
const COINCIDENT = 1e-6;
const MIN_LENGTH = 1e-12;

/*
================
RibbonChain

Points in order: position xyz, color rgba and width. count is the live
length; the arrays grow and are kept.
================
*/
export interface RibbonChain {
	positions: Float64Array;
	colors: Float64Array;
	widths: Float64Array;
	count: number;
}

/*
================
RibbonStreams

The vertex streams a strip is written into (two vertices a point).
================
*/
export interface RibbonStreams {
	readonly positions: Float32Array;
	readonly colors: Float32Array;
	readonly uvs: Float32Array;
	readonly indices: Uint32Array;
}

/*
================
createRibbonChain
================
*/
export function createRibbonChain( capacity = 16 ): RibbonChain {
	return {
		positions: new Float64Array( capacity * 3 ),
		colors: new Float64Array( capacity * 4 ),
		widths: new Float64Array( capacity ),
		count: 0
	};
}

/*
================
reserve

Room for count points, keeping the points already there.
================
*/
function reserve( chain: RibbonChain, count: number ): void {
	if ( count <= chain.widths.length ) return;
	const capacity = Math.max( count, chain.widths.length * 2 );
	const positions = new Float64Array( capacity * 3 ), colors = new Float64Array( capacity * 4 );
	const widths = new Float64Array( capacity );
	positions.set( chain.positions );
	colors.set( chain.colors );
	widths.set( chain.widths );
	chain.positions = positions;
	chain.colors = colors;
	chain.widths = widths;
}

/*
================
pushRibbonPoint
================
*/
export function pushRibbonPoint(
	chain: RibbonChain,
	position: ArrayLike<number>,
	positionAt: number,
	color: ArrayLike<number>,
	colorAt: number,
	alpha: number,
	width: number
): void {
	reserve( chain, chain.count + 1 );
	const at = chain.count++;
	for ( let axis = 0; axis < 3; axis++ ) chain.positions[at * 3 + axis] = position[positionAt + axis]!;
	for ( let c = 0; c < 4; c++ ) chain.colors[at * 4 + c] = color[colorAt + c]!;
	chain.colors[at * 4 + 3] = color[colorAt + 3]! * alpha;
	chain.widths[at] = width;
}

/*
================
copyPoint
================
*/
function copyPoint( from: RibbonChain, at: number, to: RibbonChain ): void {
	reserve( to, to.count + 1 );
	const into = to.count++;
	for ( let axis = 0; axis < 3; axis++ ) to.positions[into * 3 + axis] = from.positions[at * 3 + axis]!;
	for ( let c = 0; c < 4; c++ ) to.colors[into * 4 + c] = from.colors[at * 4 + c]!;
	to.widths[into] = from.widths[at]!;
}

/*
================
distinctPoints

input's points into out without coincident neighbours. strict: an axis
must move by more than COINCIDENT (AF9020), else by at least it.
================
*/
function distinctPoints( input: RibbonChain, out: RibbonChain, strict: boolean ): void {
	out.count = 0;
	for ( let i = 0; i < input.count; i++ ) {
		const last = out.count - 1;
		let moved = last < 0;
		for ( let axis = 0; axis < 3 && !moved; axis++ ) {
			const step = Math.abs( input.positions[i * 3 + axis]! - out.positions[last * 3 + axis]! );
			moved = strict ? step > COINCIDENT : step >= COINCIDENT;
		}
		if ( moved ) copyPoint( input, i, out );
	}
	if ( out.count < 2 ) out.count = 0;
}

/*
================
ribbonSpline

AF9020: remove coincident neighbours, duplicate the endpoint controls, and
sample the uniform cubic B-spline at thirds; colour and width interpolate
linearly between the segment's own points, the colour quantized to 8 bits.
The last point closes the chain as is. work holds the distinct points.
================
*/
export function ribbonSpline( input: RibbonChain, out: RibbonChain, work: RibbonChain ): void {
	distinctPoints( input, work, true );
	out.count = 0;
	const n = work.count;
	if ( n < 2 ) return;
	reserve( out, (n - 1) * 3 + 1 );
	// Controls are [ p0, p0..pn-1, pn-1 ]: control k is point clamp(k - 1).
	const control = ( k: number ) => Math.min( n - 1, Math.max( 0, k - 1 ) );
	for ( let i = 0; i < n - 1; i++ ) {
		const c0 = control( i ), c1 = control( i + 1 ), c2 = control( i + 2 ), c3 = control( i + 3 );
		for ( let step = 0; step < 3; step++ ) {
			const t = step / 3, t2 = t * t, t3 = t2 * t;
			const w0 = (1 - t) ** 3 / 6,
				w1 = (3 * t3 - 6 * t2 + 4) / 6,
				w2 = (-3 * t3 + 3 * t2 + 3 * t + 1) / 6,
				w3 = t3 / 6;
			const at = out.count++, p = work.positions;
			for ( let axis = 0; axis < 3; axis++ ) {
				out.positions[at * 3 + axis] = 0 + p[c0 * 3 + axis]! * w0 + p[c1 * 3 + axis]! * w1 +
					p[c2 * 3 + axis]! * w2 + p[c3 * 3 + axis]! * w3;
			}
			for ( let c = 0; c < 4; c++ ) {
				out.colors[at * 4 + c] = Math.trunc(
					(work.colors[c1 * 4 + c]! * (1 - t) + work.colors[c2 * 4 + c]! * t) * 255
				) / 255;
			}
			out.widths[at] = work.widths[c1]! * (1 - t) + work.widths[c2]! * t;
		}
	}
	copyPoint( work, n - 1, out );
}

/*
================
ribbonPolyline

AF8E80 (LinkDPipe) / AF73A0 (LinkObj): the raw element chain, dropping a
point only when no axis moved by COINCIDENT or more; no spline.
================
*/
export function ribbonPolyline( input: RibbonChain, out: RibbonChain ): void {
	distinctPoints( input, out, false );
}

/*
================
ribbonStrip

AF80E0: each point's projected neighbour tangent gives a camera-space
perpendicular; the strip spans the point's world-space half-width either
side of it. Rizin AF8103..AF811E confirms the UV step 1/(count-1). The
tangent is NDC, not pixels: AF80E0 calls D3DXVec3Project with a NULL
viewport, which stops at normalized device coordinates.

Writes 2 vertices a point at vertex and 6 indices a segment at index;
work holds the projections.
================
*/
export function ribbonStrip(
	points: RibbonChain,
	view: Float32Array,
	streams: RibbonStreams,
	vertex: number,
	index: number,
	work: RibbonChain
): void {
	const n = points.count;
	const rx = view[0]!, ry = view[4]!, rz = view[8]!, ux = view[1]!, uy = view[5]!, uz = view[9]!;
	const rl = Math.hypot( rx, ry, rz ), ul = Math.hypot( ux, uy, uz );
	if ( rl < MIN_LENGTH || ul < MIN_LENGTH ) throw Error( "Invalid ribbon camera" );
	reserve( work, n );
	const projected = work.positions;
	for ( let i = 0; i < n; i++ ) {
		const x = points.positions[i * 3]!, y = points.positions[i * 3 + 1]!, z = points.positions[i * 3 + 2]!;
		const w = (view[3]! * x + view[7]! * y + view[11]! * z + view[15]!) || 1e-12;
		projected[i * 2] = (view[0]! * x + view[4]! * y + view[8]! * z + view[12]!) / w;
		projected[i * 2 + 1] = (view[1]! * x + view[5]! * y + view[9]! * z + view[13]!) / w;
	}
	const { positions, colors, uvs, indices } = streams;
	for ( let i = 0; i < n; i++ ) {
		const a = Math.max( 0, i - 1 ), b = Math.min( n - 1, i + 1 );
		const dx = projected[a * 2]! - projected[b * 2]!, dy = projected[a * 2 + 1]! - projected[b * 2 + 1]!;
		const sx = rx / rl * dy - ux / ul * dx, sy = ry / rl * dy - uy / ul * dx, sz = rz / rl * dy - uz / ul * dx;
		const length = Math.hypot( sx, sy, sz ), width = points.widths[i]!, at = (vertex + i * 2) * 3;
		for ( let axis = 0; axis < 3; axis++ ) {
			const side = axis === 0 ? sx : axis === 1 ? sy : sz;
			const offset = length > MIN_LENGTH ? side / length * width : 0, p = points.positions[i * 3 + axis]!;
			positions[at + axis] = p + offset;
			positions[at + 3 + axis] = p - offset;
		}
		for ( let c = 0; c < 4; c++ ) {
			colors[(vertex + i * 2) * 4 + c] = colors[(vertex + i * 2 + 1) * 4 + c] = points.colors[i * 4 + c]!;
		}
		const u = i / Math.max( 1, n - 1 ), uv = (vertex + i * 2) * 2;
		uvs[uv] = u;
		uvs[uv + 1] = 0;
		uvs[uv + 2] = u;
		uvs[uv + 3] = 1;
		if ( i ) {
			const first = vertex + i * 2 - 2, to = index + (i - 1) * 6;
			indices[to] = first;
			indices[to + 1] = first + 1;
			indices[to + 2] = first + 2;
			indices[to + 3] = first + 2;
			indices[to + 4] = first + 1;
			indices[to + 5] = first + 3;
		}
	}
}
