/*
===========================================================================

quickslot-cooldown.ts - native cooldown sweeps and numeric counters

Inventory and quickslots resolve the same receipt-owned cooldown rows.

===========================================================================
*/
import { itemCooldown, type ItemCooldown } from "@/engine/foundation/gameplay/item-cooldowns";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import type { SkillCooldown } from "@/engine/foundation/gameplay/skill-cooldowns";
const ROOT = "/assets/images/Media_extracted/";
/*
================
quickslotTimerPaths
================
*/
export function quickslotTimerPaths() {
	return [
		ROOT + "interface/skill/skill_delay.png",
		ROOT + "interface/skill/skill_charge.png",
		...Array.from( { length: 10 }, ( _, i ) => ROOT + "effect/icon/cool_time_" + i + ".png" )
	];
}
// 554800 binds the atlases; 565850 uses trunc(float(239 - float(ratio*239)))
// in a 16-column atlas, then a 16-frame completion flash lasting 500 ms.
/*
================
quickslotCooldownQuads
================
*/
export function quickslotCooldownQuads(
	rows: readonly SkillCooldown[],
	skill: number,
	group: number,
	now: number,
	r: UiRect,
	clip: UiRect
): UiQuad[] {
	const row = rows.find( c =>
		(c.skill === skill || !!group && c.group === group) && now < c.startedAtMs + c.durationMs + 500
	);
	if ( !row ) return [];
	return cooldownQuads( row.startedAtMs, row.durationMs, now, r, clip );
}
/*
================
quickslotItemCooldownQuads
================
*/
export function quickslotItemCooldownQuads(
	rows: readonly ItemCooldown[],
	target: Parameters<typeof itemCooldown>[1],
	now: number,
	r: UiRect,
	clip: UiRect
): UiQuad[] {
	const row = itemCooldown( rows, target, now );
	return row ? cooldownQuads( row.startedAtMs, row.durationMs, now, r, clip ) : [];
}
// 566648: inventory message 0x46 shares the sweep; only quickslot 0x0C
// with linked kind 0x46 calls the numeric countdown renderer.
/*
================
inventoryItemCooldownQuads
================
*/
export function inventoryItemCooldownQuads(
	rows: readonly ItemCooldown[],
	target: Parameters<typeof itemCooldown>[1],
	now: number,
	r: UiRect,
	clip: UiRect
): UiQuad[] {
	const row = itemCooldown( rows, target, now );
	return row ? cooldownQuads( row.startedAtMs, row.durationMs, now, r, clip, false ) : [];
}
/*
================
cooldownQuads
================
*/
function cooldownQuads(
	startedAtMs: number,
	durationMs: number,
	now: number,
	r: UiRect,
	clip: UiRect,
	showRemaining = true
): UiQuad[] {
	const timerPaths = quickslotTimerPaths();
	const remaining = Math.max( 0, startedAtMs + durationMs - now ),
		active = remaining > 0,
		ratio = Math.fround(
			Math.min( 1, active ? remaining / durationMs : (startedAtMs + durationMs + 500 - now) / 500 )
		),
		count = active ? 239 : 15,
		columns = active ? 16 : 4;
	const frame = Math.max( 0, Math.min( count, Math.trunc( Math.fround( count - Math.fround( ratio * count ) ) ) ) ),
		quads: UiQuad[] = [ {
			rect: r,
			clip,
			color: [ 1, 1, 1, 1 ],
			texture: timerPaths[active ? 0 : 1]!,
			uv: [ frame % columns / columns, Math.floor( frame / columns ) / columns, 1 / columns, 1 / columns ]
		} ];
	if ( active && showRemaining ) {
		const seconds = Math.floor( remaining / 1000 ),
			value = seconds > 3600 ? Math.floor( seconds / 3600 ) : seconds > 60 ? Math.floor( seconds / 60 ) : seconds,
			color: UiQuad["color"] = seconds > 3600 ?
				[ 0, 12 / 255, 169 / 255, 1 ] :
				seconds > 60 ?
				[ 225 / 255, 231 / 255, 0, 1 ] :
				[ 1, 1, 1, 1 ],
			digits = String( value );
		for ( let i = 0; i < digits.length; i++ ) {
			quads.push( {
				rect: [
					r[0] + Math.floor( r[2] / 2 ) - digits.length * 4 + i * 8,
					r[1] + Math.floor( r[3] / 2 ) - 6,
					8,
					12
				],
				clip,
				color,
				texture: timerPaths[2 + Number( digits[i] )]!,
				uv: [ 0, 0, 1, 1 ]
			} );
		}
	}
	return quads;
}
