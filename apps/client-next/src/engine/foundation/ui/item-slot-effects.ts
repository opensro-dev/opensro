/*
===========================================================================

item-slot-effects.ts - the animated overlays native item slots draw

CIFSlotWithHelp (5548C0 loads the sheets, CIFSlotWithHelp_OnStateTimer
555110 steps them, CIFControl_RenderIconOverlays 565850 draws them) puts
four sprite-sheet animations over an item icon:

	rare shine     icon_edge_rare.ddj        8x4 cells, loops, 40 ms   (54FA60)
	summoned glow  pt_edge_effect.ddj        9x1 cells, loops, 50 ms   (54FAA0)
	revival flash  pt_life_effect.ddj        8x1 cells, once,  80 ms   (54FAF0)
	changed flash  icon_mall_transgender.ddj 4x4 cells, once,  50 ms   (54FA40)

and a dead companion's summoner item is washed with 0x80004B7E
(CIFWnd_DrawColorOverlay 53F590). The looping counters start at a random
phase when an item is bound; a stable per-slot seed stands in for rand().

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";

const SHEET = "/assets/images/Media_extracted/";
// CSOItemData_IsRare (789340): item +0xA0 (itemdata column 15) == 2.
const RARITY_RARE = 2;
// Summoner rent state (0x3645 flag 0x40, CIFSlot_ApplySummonerRentState
// 54FC80): 2 summoned, 3 dormant, 4 dead.
const RENT_SUMMONED = 2;
const RENT_DEAD = 4;
const RARE_FRAMES = 32, RARE_COLUMNS = 8, RARE_ROWS = 4, RARE_STEP_MS = 40;
const GLOW_FRAMES = 9, GLOW_STEP_MS = 50;
const LIFE_FRAMES = 8, LIFE_STEP_MS = 80;
const CHANGED_FRAMES = 16, CHANGED_COLUMNS = 4, CHANGED_STEP_MS = 50;
// 566B94: the changed flash covers 48 px from 8 px above and left of the slot.
const CHANGED_INSET = 8, CHANGED_SIZE = 48;

/*
================
ItemSlotOverlay

One textured quad over (or around) the slot: its image, UV window and rect.
================
*/
export interface ItemSlotOverlay {
	readonly path: string;
	readonly uv: UiRect;
	readonly rect: UiRect;
}

/*
================
ItemSlotFlash

A one-shot flash the item-state update raised for a slot.
================
*/
export interface ItemSlotFlash {
	readonly kind: "changed" | "life";
	readonly atMs: number;
}

/*
================
slotSeed

A stable stand-in for the native rand() phase, from the slot's identity.
================
*/
export function slotSeed( key: string ): number {
	let hash = 2166136261;
	for ( let i = 0; i < key.length; i++ ) hash = Math.imul( hash ^ key.charCodeAt( i ), 16777619 ) >>> 0;
	return hash;
}

/*
================
loopFrame

555110 decrements the counter each tick and wraps it to the frame count;
565850 draws frame (count - counter). A counter seeded with phase p shows
frame (count - p + ticks) mod count.
================
*/
function loopFrame( nowMs: number, stepMs: number, frames: number, phase: number ): number {
	const ticks = Math.floor( nowMs / stepMs );
	return ((frames - phase + ticks) % frames + frames) % frames;
}

/*
================
itemSlotOverlays

Everything 565850 draws over one slot this frame, in its draw order:
summoned glow, rare shine, then the one-shot flashes.
================
*/
export function itemSlotOverlays(
	item: {
		readonly tooltip?: { readonly fields: Readonly<Record<string, number>>; };
		readonly summon?: { readonly state: number; };
	} | undefined,
	rect: UiRect,
	seed: number,
	nowMs: number,
	flashes: readonly ItemSlotFlash[] = []
): readonly ItemSlotOverlay[] {
	if ( !item ) return [];
	const out: ItemSlotOverlay[] = [];
	if ( item.summon?.state === RENT_SUMMONED ) {
		const frame = loopFrame( nowMs, GLOW_STEP_MS, GLOW_FRAMES, seed % GLOW_FRAMES );
		out.push( {
			path: SHEET + "interface/pet/pt_edge_effect.png",
			uv: [ frame / GLOW_FRAMES, 0, 1 / GLOW_FRAMES, 1 ],
			rect
		} );
	}
	if ( item.tooltip?.fields.rarity === RARITY_RARE ) {
		const frame = loopFrame( nowMs, RARE_STEP_MS, RARE_FRAMES, seed & (RARE_FRAMES - 1) );
		out.push( {
			path: SHEET + "icon/item/etc/icon_edge_rare.png",
			uv: [
				(frame % RARE_COLUMNS) / RARE_COLUMNS,
				Math.floor( frame / RARE_COLUMNS ) / RARE_ROWS,
				1 / RARE_COLUMNS,
				1 / RARE_ROWS
			],
			rect
		} );
	}
	for ( const flash of flashes ) {
		const elapsed = nowMs - flash.atMs;
		if ( flash.kind === "life" ) {
			const frame = Math.floor( elapsed / LIFE_STEP_MS );
			if ( elapsed >= 0 && frame < LIFE_FRAMES ) {
				out.push( {
					path: SHEET + "interface/pet/pt_life_effect.png",
					uv: [ frame / LIFE_FRAMES, 0, 1 / LIFE_FRAMES, 1 ],
					rect
				} );
			}
		} else {
			const frame = Math.floor( elapsed / CHANGED_STEP_MS );
			if ( elapsed >= 0 && frame < CHANGED_FRAMES ) {
				out.push( {
					path: SHEET + "icon/icon_mall_transgender.png",
					uv: [
						(frame % CHANGED_COLUMNS) / CHANGED_COLUMNS,
						Math.floor( frame / CHANGED_COLUMNS ) / CHANGED_COLUMNS,
						1 / CHANGED_COLUMNS,
						1 / CHANGED_COLUMNS
					],
					rect: [ rect[0] - CHANGED_INSET, rect[1] - CHANGED_INSET, CHANGED_SIZE, CHANGED_SIZE ]
				} );
			}
		}
	}
	return out;
}

/*
================
itemSlotWash

The 0x80004B7E wash 54FC80 sets on a dead companion's summoner slot, as
RGBA, or null.
================
*/
export function itemSlotWash(
	item: { readonly summon?: { readonly state: number; }; } | undefined
): readonly [number, number, number, number] | null {
	return item?.summon?.state === RENT_DEAD ? [ 0x00 / 255, 0x4b / 255, 0x7e / 255, 0x80 / 255 ] : null;
}

/*
================
itemSlotFlashKinds

What the 0x3645 item-state update raises for a slot (7654B0), each on its
own timer: a new item type flashes the slot (flag 1); a summoner whose rent
state goes from dead to dormant plays the revival flash (flag 0x40, 54FC80
4 -> 3).
================
*/
export function itemSlotFlashKinds( flags: number, previousRent: number | undefined, nextRent: number | undefined ) {
	const kinds: ItemSlotFlash["kind"][] = [];
	if ( flags & 0x01 ) kinds.push( "changed" );
	if ( flags & 0x40 && previousRent === RENT_DEAD && nextRent === 3 ) kinds.push( "life" );
	return kinds;
}
