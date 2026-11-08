/*
===========================================================================

damage-text.ts - native floating hit numbers and linked pet damage colour

===========================================================================
*/
import type { DamageText } from "@/engine/contracts/damage-text";
import type { CastImpact } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import type { UiQuad } from "@/engine/contracts/ui";
const ROOT = "/assets/images/Media_extracted/interface/hitcount/";
/*
================
damageTextAssets
================
*/
export function damageTextAssets() {
	return [ ...Array.from( { length: 10 }, ( _, i ) => `hitcount_${i}` ), "critical", "blocking", "resist" ].flatMap(
		name => [ ROOT + name + "_shadow.png", ROOT + name + ".png" ]
	);
}
// CIDamageText 8D3920: copy the impact position, not the target identity.
/*
================
damageText
================
*/
export function damageText(
	target: EntityState,
	impact: CastImpact,
	started: number,
	sourceNotCharacter = false,
	linked = false
): DamageText {
	let kind: DamageText["kind"] = impact.type === 2 ?
		2 :
		((impact.type ?? 0) === 0 || impact.type === 4) && (impact.flags & 2) ?
		1 :
		0;
	if ( (impact.flags & 0x10) && !impact.secondaryAmount ) kind = 3;
	const player = target.kind === "local-player" || target.kind === "player";
	return {
		targetGid: target.gid,
		anchor: { regionId: target.regionId, x: target.x, y: target.y + 20, z: target.z },
		started,
		kind,
		damage: impact.damage,
		color: linked ?
			[ 1, 211 / 255, 58 / 255 ] :
			sourceNotCharacter ?
			(player ? [ 1, 211 / 255, 58 / 255 ] : [ 1, 113 / 255, 58 / 255 ]) :
			player ?
			[ 1, 58 / 255, 58 / 255 ] :
			[ 1, 1, 1 ]
	};
}
// 85E160 selects the lowest existing text's kind, shifts every text on that
// target, then inserts the new text. 8D2B80 restarts only the rise interpolator;
// opacity and the three-second object lifetime keep their original clock.
/*
================
damageTextRise
================
*/
export function damageTextRise( row: DamageText, now: number ): number {
	const rise = row.rise ?? { at: row.started, from: 0, to: 50 };
	return rise.from + (rise.to - rise.from) * Math.min( 1, Math.max( 0, now - rise.at ) );
}
/*
================
appendDamageText
================
*/
export function appendDamageText( rows: readonly DamageText[], next: DamageText, now: number ): DamageText[] {
	let lowest: DamageText | undefined, minimum = Infinity;
	for ( const row of rows ) {
		if ( row.targetGid !== next.targetGid || now - row.started > 3 ) continue;
		const rise = damageTextRise( row, now );
		if ( rise < minimum ) {
			minimum = rise;
			lowest = row;
		}
	}
	const gap = lowest?.kind === 0 ? 40 : lowest?.kind === 1 ? 80 : lowest?.kind === 2 ? 50 : 0;
	return [
		...rows.map( row =>
			row.targetGid === next.targetGid && now - row.started <= 3 ?
				{ ...row, rise: { at: now, from: damageTextRise( row, now ) + gap, to: (row.rise?.to ?? 50) + gap } } :
				row
		),
		next
	];
}
// 8D27A0 / 8EB700: 1600x1200 reference, one-second rise/alpha,
// shadow then face. These are ordinary GPU UI quads, with no DOM pool.
/*
================
damageTextQuads
================
*/
export function damageTextQuads( rows: readonly DamageText[], now: number, width: number, height: number ): UiQuad[] {
	const result: UiQuad[] = [], sx = width / 1600, sy = height / 1200;
	for ( const row of rows ) {
		const age = Math.max( 0, now - row.started ),
			alpha = Math.trunc( 255 * Math.min( 1, Math.max( 0, 2 * (1 - age) ) ) ) / 255;
		if ( !alpha ) continue;
		const scale = row.kind >= 2 ? 2 : 1, rise = damageTextRise( row, now ), clip = [ 0, 0, width, height ] as const;
		const glyph = ( name: string, x: number, y: number, w: number, h: number ) => {
			const rect = [
				(x - w / 2) * scale * sx,
				(-rise + (y - h / 2) * scale) * sy,
				w * scale * sx,
				h * scale * sy
			] as const;
			for ( const shadow of [ true, false ] ) {
				result.push( {
					worldAnchor: row.anchor,
					occlusion: "none",
					rect,
					clip,
					uv: [ 0, 0, 1, 1 ],
					texture: ROOT + name + (shadow ? "_shadow" : "") + ".png",
					color: shadow ? [ 1, 1, 1, alpha ] : [ ...row.color, alpha ]
				} );
			}
		};
		if ( row.kind ) {
			glyph(
				[ "", "critical", "blocking", "resist" ][row.kind]!,
				0,
				row.kind === 1 ? 45 : 0,
				row.kind === 1 ? 96 : 60,
				row.kind === 1 ? 24 : 28
			);
		}
		if ( row.kind < 2 && row.damage > 0 ) {
			const digits = String( row.damage >>> 0 ), critical = row.kind === 1;
			for ( let i = 0; i < digits.length; i++ ) {
				glyph(
					"hitcount_" + digits[i],
					(i - (digits.length - 1) / 2) * (critical ? 33 : 22),
					0,
					critical ? 36 : 24,
					critical ? 60 : 40
				);
			}
		}
	}
	return result;
}
