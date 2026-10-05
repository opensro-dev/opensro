/*
===========================================================================

texture-factor-pulse.ts - a BSR material modifier's TEXTUREFACTOR pulse

CRTModMtrl_AdvancePulseChannel (AECAB0) runs every material tick: it packs
the truncated current value into the top byte of +0x90 (the word
CRTModMtrl_BeginStates, AED240, sets as D3DRS_TEXTUREFACTOR), then moves
the value by rate * delta * 0.01 (delta in integer milliseconds, the
modifier clock), down while +0x98 is 0 and up otherwise, and turns at the
bounds: below the low byte (+0x6a) it clamps there and rises, above the
high byte (+0x6b) it clamps there and falls. CRTModMtrl_Ctor (AECD00)
starts the value at 0 rising, with the packed word 0.

The factor is ARGB (byte << 24): its RGB stays 0 and only alpha pulses.
Arithmetic follows the x87 path: the step is formed in extended
precision and the value stored as a float.

===========================================================================
*/

import { createModifierDelta } from "./modifier-delta";

// AECAB0's step scale, a float32 constant (rounded where it is used).
const PULSE_SCALE = 0.01;

/*
================
TextureFactorPulseSource
================
*/
export interface TextureFactorPulseSource {
	readonly low: number;
	readonly high: number;
	readonly rate: number;
}

/*
================
createTextureFactorPulse

factor is the TEXTUREFACTOR rgba the next draw uses. step takes the frame
time in seconds and reports whether factor changed.
================
*/
export function createTextureFactorPulse( source: TextureFactorPulseSource ) {
	const factor = new Float32Array( 4 ), deltaFor = createModifierDelta(), rate = Math.fround( source.rate );
	const scale = Math.fround( PULSE_SCALE );
	let value = 0, rising = true;
	/*
	================
	stepDelta
	================
	*/
	const stepDelta = ( delta: number ): boolean => {
		const packed = (Math.trunc( value ) & 0xff) / 255, changed = factor[3] !== Math.fround( packed );
		factor[3] = packed;
		const step = rate * delta * scale;
		value = Math.fround( rising ? step + value : value - step );
		if ( source.low > value ) {
			rising = true;
			value = source.low;
		} else if ( source.high < value ) {
			rising = false;
			value = source.high;
		}
		return changed;
	};
	return { factor, stepDelta, step: ( seconds: number ) => stepDelta( deltaFor( seconds ) ) };
}
