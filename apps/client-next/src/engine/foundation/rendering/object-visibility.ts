/*
===========================================================================

object-visibility.ts - the object fade by eye distance (SWorld 0x8C4C60)

An object fades in when its eye distance less its radius comes inside its
range and out when it leaves it. State belongs to the world renderer,
never to GPU resources.

===========================================================================
*/

export interface ObjectFade {
	state: 0 | 1 | 2 | 3;
	alpha: number;
	lastFrame: number;
}

/*
================
advanceObjectFade

States: 0 out, 1 fading in, 2 in, 3 fading out. An object not advanced
for 50 frames drops straight out instead of fading.
================
*/
export function advanceObjectFade(
	previous: ObjectFade,
	distance: number,
	radius: number,
	range: number,
	dt: number,
	frame: number,
	output?: ObjectFade
): ObjectFade {
	let { state, alpha } = previous;
	const adjusted = Math.fround( distance - radius );
	if ( adjusted >= range ) {
		if ( ((previous.lastFrame + 50) >>> 0) < frame ) {
			state = 0;
			alpha = 0;
		} else {
			switch ( state ) {
				case 0:
					alpha = 0;
					break;
				case 1:
					state = 3;
					break;
				case 2:
					state = 3;
					alpha = 255;
					break;
				case 3:
					alpha = Math.fround( alpha - Math.fround( dt ) * 512 );
					if ( alpha <= 0 ) {
						state = 0;
						alpha = 0;
					}
					break;
			}
		}
	} else {
		switch ( state ) {
			case 0:
				state = adjusted * 1.5 <= range ? 2 : 1;
				alpha = state === 2 ? 255 : 0;
				break;
			case 1:
				alpha = Math.fround( alpha + Math.fround( dt ) * 512 );
				if ( alpha >= 255 ) {
					state = 2;
					alpha = 255;
				}
				break;
			case 2:
				break;
			case 3:
				state = 1;
				break;
		}
	}
	// The renderer owns the destination. Read the complete previous state before
	// committing so an owner can reuse its slot without changing transition rules.
	if ( output ) {
		output.state = state;
		output.alpha = alpha;
		output.lastFrame = frame;
		return output;
	}
	return { state, alpha, lastFrame: frame };
}
