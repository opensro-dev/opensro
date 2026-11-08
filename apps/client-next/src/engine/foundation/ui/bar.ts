/*
===========================================================================

bar.ts - CIFBarWnd chrome: natural end caps and a repeated middle sprite

Every piece samples nearest, like the stretch-ring chrome. The pieces tile
edge to edge, and the area sampler counts the part of an output pixel that
falls outside a quad as transparent, so a tiled bar sampled the ordinary
way composites to well under full opacity at every seam: at a fractional
UI scale the world streaks through the bar between its tiles (#2437).

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";

/*
================
barChrome

The middle sprite repeats at its natural width, the last repeat clipped
to the tail the caps leave, with its UV cut by the same fraction so the
tail texels are never stretched.
================
*/
export function barChrome(
	r: UiRect,
	prefix: string,
	size: ( p: string ) => readonly [number, number] | undefined,
	clip: UiRect
) {
	const paths = [ "left", "mid", "right" ].map( n => prefix + n + ".png" );
	const [l, m, rr] = paths.map( size );
	const quads: UiQuad[] = [];

	if ( l && m && rr ) {
		const piece = ( i: number, x: number, width: number, naturalWidth: number, height: number ) => {
			if ( width > 0 ) {
				quads.push( {
					rect: [ x, r[1], width, height ],
					clip,
					sampling: "nearest",
					texture: paths[i]!,
					uv: [ 0, 0, width / naturalWidth, 1 ],
					color: [ 1, 1, 1, 1 ]
				} );
			}
		};

		piece( 0, r[0], l[0], l[0], l[1] );
		piece( 2, r[0] + r[2] - rr[0], rr[0], rr[0], rr[1] );
		for ( let dx = l[0]; dx < r[2] - rr[0]; dx += m[0] ) {
			piece( 1, r[0] + dx, Math.min( m[0], r[2] - rr[0] - dx ), m[0], m[1] );
		}
	}
	return { paths, quads };
}
