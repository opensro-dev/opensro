/*
===========================================================================

skill-press-feedback.ts - what a shortcut slot shows for a held or denied
skill press

The skill that casts next (skill-queue.ts: held by the client for its
cooldown, or by the server behind its open command) outlines its slot with a
pulsing gold border, and a small chip above the main shortcut bar shows its
icon wherever it was pressed from (the skill window, a hidden bar page). A
held press also fills a thin strip under the chip until it goes out. A
denied press (too long to wait) tints its slot red and shakes it for
DENIAL_FLASH_MS, silently, instead of sending a press the server would
refuse. Presentation only: a deliberate addition to the original's HUD,
which shows none of it.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";

// How long a denied slot flashes and shakes.
export const DENIAL_FLASH_MS = 180;
// The queued outline's width and pulse period.
const OUTLINE_PX = 2;
const PULSE_MS = 600;
// The denial shake's amplitude and number of swings.
const SHAKE_PX = 2;
const SHAKE_SWINGS = 3;
// The next-skill chip: icon size, gap above the bar, backing padding, the
// fade-in and the held press's progress strip.
const CHIP_PX = 26;
const CHIP_GAP_PX = 6;
const CHIP_PAD_PX = 2;
const CHIP_FADE_MS = 120;
const CHIP_STRIP_PX = 2;
// Gold for a held press, red for a denied one.
const QUEUED_R = 1;
const QUEUED_G = 0.82;
const QUEUED_B = 0.25;
const DENIED_R = 1;
const DENIED_G = 0.15;
const DENIED_B = 0.1;

/*
================
SkillPressFeedbackState

The two fields of the gameplay plane this reads.
================
*/
export interface SkillPressFeedbackState {
	readonly skillQueue?: { readonly skill: number; readonly sinceMs: number; readonly fireAtMs?: number; };
	readonly skillDenied?: { readonly skill: number; readonly atMs: number; };
}

/*
================
pulse

The queued gold's breathing alpha at now.
================
*/
function pulse( now: number ): number {
	return 0.6 + 0.4 * Math.sin( now / PULSE_MS * 2 * Math.PI );
}

/*
================
outline

A gold border of width px around r.
================
*/
function outline( r: UiRect, clip: UiRect, alpha: number, px: number ): UiQuad[] {
	const [x, y, w, h] = r, color: UiQuad["color"] = [ QUEUED_R, QUEUED_G, QUEUED_B, alpha ];
	return [
		[ x, y, w, px ],
		[ x, y + h - px, w, px ],
		[ x, y, px, h ],
		[ x + w - px, y, px, h ]
	].map( edge => ({ rect: edge as unknown as UiRect, clip, color, texture: "", uv: [ 0, 0, 1, 1 ] }) );
}

/*
================
skillPressFeedback

The slot's horizontal shake offset and the quads drawn over its icon at
now (the gameplay clock the cooldowns use).
================
*/
export function skillPressFeedback(
	game: SkillPressFeedbackState | null | undefined,
	skill: number,
	r: UiRect,
	clip: UiRect,
	now: number
): { readonly offsetX: number; readonly quads: UiQuad[]; } {
	const quads: UiQuad[] = [];
	let offsetX = 0;
	if ( game?.skillQueue?.skill === skill ) quads.push( ...outline( r, clip, pulse( now ), OUTLINE_PX ) );
	const denied = game?.skillDenied;
	if ( denied?.skill === skill && now >= denied.atMs && now - denied.atMs < DENIAL_FLASH_MS ) {
		const t = (now - denied.atMs) / DENIAL_FLASH_MS;
		offsetX = Math.round( SHAKE_PX * (1 - t) * Math.sin( t * SHAKE_SWINGS * 2 * Math.PI ) );
		quads.push( {
			rect: [ r[0] + offsetX, r[1], r[2], r[3] ],
			clip,
			color: [ DENIED_R, DENIED_G, DENIED_B, 0.45 * (1 - t) ],
			texture: "",
			uv: [ 0, 0, 1, 1 ]
		} );
	}
	return { offsetX, quads };
}

/*
================
skillPressFeedbackActive

Whether the slots still animate: a held press, or a denial still flashing.
================
*/
export function skillPressFeedbackActive( game: SkillPressFeedbackState | null | undefined, now: number ): boolean {
	return !!game?.skillQueue || !!game?.skillDenied && now - game.skillDenied.atMs < DENIAL_FLASH_MS;
}

/*
================
SkillQueueChip

The next-skill chip: under is drawn before the icon, over after it.
================
*/
export interface SkillQueueChip {
	readonly under: UiQuad[];
	readonly icon: UiRect;
	readonly alpha: number;
	readonly over: UiQuad[];
}

/*
================
skillQueueChip

The chip for the skill that casts next, centred above the bar whose top
edge runs from barLeft to barRight at barTop; null when nothing waits.
================
*/
export function skillQueueChip(
	game: SkillPressFeedbackState | null | undefined,
	bar: { readonly left: number; readonly right: number; readonly top: number; },
	clip: UiRect,
	now: number
): SkillQueueChip | null {
	const queue = game?.skillQueue;
	if ( !queue ) return null;
	const alpha = Math.min( 1, Math.max( 0, (now - queue.sinceMs) / CHIP_FADE_MS ) );
	const x = Math.round( (bar.left + bar.right - CHIP_PX) / 2 ), y = bar.top - CHIP_GAP_PX - CHIP_PX;
	const icon: UiRect = [ x, y, CHIP_PX, CHIP_PX ];
	const backing: UiRect = [ x - CHIP_PAD_PX, y - CHIP_PAD_PX, CHIP_PX + 2 * CHIP_PAD_PX, CHIP_PX + 2 * CHIP_PAD_PX ];
	const under: UiQuad[] = [ {
		rect: backing,
		clip,
		color: [ 0, 0, 0, 0.55 * alpha ],
		texture: "",
		uv: [ 0, 0, 1, 1 ]
	} ];
	const over = outline( backing, clip, pulse( now ) * alpha, 1 );
	if ( queue.fireAtMs !== undefined && queue.fireAtMs > queue.sinceMs ) {
		const done = Math.min( 1, Math.max( 0, (now - queue.sinceMs) / (queue.fireAtMs - queue.sinceMs) ) );
		over.push( {
			rect: [ backing[0], backing[1] + backing[3] + 1, Math.round( backing[2] * done ), CHIP_STRIP_PX ],
			clip,
			color: [ QUEUED_R, QUEUED_G, QUEUED_B, alpha ],
			texture: "",
			uv: [ 0, 0, 1, 1 ]
		} );
	}
	return { under, icon, alpha, over };
}
