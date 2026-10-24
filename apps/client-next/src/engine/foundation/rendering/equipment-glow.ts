/*
===========================================================================

equipment-glow.ts - the reinforcement glow on a weapon or shield

itemtypenumber / CRTModProgEquipPow (AEF510, AEF610, AEF8E0): a reinforced
item adds a scrolling option texture, tinted between two colours, onto its
own texture. 8EA960 picks the row through 915490 by the item's plus.
Image indices belong to the admitted weapon model, never a global texture
cache. Port-only, not native: rows marked enhanced come from the later
client's tiers and draw only with the Experimental "Enhanced item glow" row.

===========================================================================
*/

/*
================
EquipmentGlow

One itemtypenumber row: the lowest plus, option image, UV rate, colour
pair, ping-pong period, stage-0 gain and alpha test.
================
*/
export interface EquipmentGlow {
	readonly threshold: number;
	readonly image: number;
	readonly uv: readonly [number, number];
	readonly color1: readonly [number, number, number];
	readonly color2: readonly [number, number, number];
	readonly period: number;
	readonly gain: 1 | 2;
	readonly alphaTest: boolean;
	// Port-only, not native: a row from the later client's tiers, drawn only
	// with the Experimental "Enhanced item glow" row on.
	readonly enhanced?: boolean;
}

/*
================
validateEquipmentGlows
================
*/
export function validateEquipmentGlows( value: Record<string, readonly EquipmentGlow[]> | undefined, images: number ) {
	if ( value === undefined ) return;
	for ( const [id, rows] of Object.entries( value ) ) {
		if ( !/^\d+$/.test( id ) || !Array.isArray( rows ) || rows.length > 256 ) {
			throw Error( "Invalid equipment glow catalog" );
		}
		for ( const r of rows ) {
			if (
				!r || !Number.isInteger( r.threshold ) || r.threshold < 0 || r.threshold > 255 ||
				!Number.isInteger( r.image ) || r.image < 0 || r.image >= images || !Array.isArray( r.uv ) ||
				r.uv.length !== 2 || ![ ...r.uv ].every( Number.isFinite ) || ![ r.color1, r.color2 ].every( c =>
					Array.isArray( c ) && c.length === 3 && c.every( v =>
						Number.isFinite( v ) && v >= 0 && v <= 1
					)
				) || !Number.isInteger( r.period ) || r.period < 0 || r.period > 0xffffffff ||
				![ 1, 2 ].includes( r.gain ) || typeof r.alphaTest !== "boolean" ||
				(r.enhanced !== undefined && typeof r.enhanced !== "boolean")
			) throw Error( "Invalid equipment glow row" );
		}
	}
}

/*
================
selectEquipmentGlow

915490: the highest threshold at or below the plus, the first row winning
equal thresholds; +0 never installs a modifier. With enhanced, an item
that carries the later client's rows uses only those; otherwise only the
native rows count.
================
*/
export function selectEquipmentGlow( rows: readonly EquipmentGlow[] | undefined, plus: number, enhanced = false ) {
	if ( !Number.isInteger( plus ) || plus < 0 || plus > 255 ) throw Error( "Invalid equipment enhancement" );
	if ( !plus ) return undefined;
	let best: EquipmentGlow | undefined;
	const useEnhanced = enhanced && (rows ?? []).some( row => row.enhanced === true );
	for ( const row of rows ?? [] ) {
		if (
			(row.enhanced === true) === useEnhanced && row.threshold <= plus &&
			(!best || row.threshold > best.threshold)
		) best = row;
	}
	return best;
}

/*
================
createEquipmentGlowClock

AEF610: the colour ping-pongs over the period and the UV scrolls by rate.
================
*/
export function createEquipmentGlowClock( row: EquipmentGlow ) {
	const start = Float32Array.from( row.color1 ),
		end = Float32Array.from( row.color2 ),
		rates = Float32Array.from( row.uv );
	let elapsed = 0, direction = 1;
	const color = new Float32Array( 3 ), uv = new Float32Array( 2 );
	const pack = ( fraction: number ) => {
		for ( let i = 0; i < 3; i++ ) {
			color[i] = (Math.trunc(
				Math.fround( start[i]! + Math.fround( Math.fround( end[i]! - start[i]! ) * fraction ) ) * 255
			) & 255) / 255;
		}
	};
	pack( 0 );
	return {
		color,
		uv,
		step( delta: number, enabled: boolean ) {
			if ( !Number.isInteger( delta ) || delta < 0 || delta > 3000 ) {
				throw Error( "Invalid equipment glow delta" );
			}
			if ( !enabled ) return;
			if ( row.period ) {
				elapsed += delta;
				let fraction: number;
				if ( elapsed >= row.period ) {
					fraction = direction >= 0 ? 1 : 0;
					elapsed = 0;
					direction = -direction;
				} else {
					fraction = Math.fround( elapsed / row.period );
					if ( direction < 0 ) fraction = Math.fround( 1 - fraction );
				}
				pack( fraction );
			}
			// ASM/LLIL are authoritative: AEF7BD..AEF81F divides BOTH rates by 20.
			// HLIL incorrectly aliases x87 temporaries in the V calculation.
			for ( let i = 0; i < 2; i++ ) uv[i] = Math.fround( uv[i]! + Math.fround( rates[i]! * delta / 20 ) );
		}
	};
}
