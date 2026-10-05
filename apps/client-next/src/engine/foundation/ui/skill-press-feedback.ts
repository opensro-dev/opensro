/*
===========================================================================

skill-press-feedback.ts - what a shortcut slot shows for a held or denied
skill press

The skill that casts next (skill-queue.ts: held by the client for its
cooldown, or by the server behind its open command) outlines its slot with a
pulsing gold border, and a small chip above the main shortcut bar shows its
icon wherever it was pressed from (the skill window, a hidden bar page). A
held press also fills a thin strip under the chip until it goes out. A
denied press (too long to wait) shakes its slot for DENIAL_FLASH_MS,
silently and without a tint (a red flash on every early press annoyed), instead of sending a press the server would
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
// The next-skill chip: a small icon (about half a slot) centred over shortcut
// slot 1, the gap above the slot, the backing's padding, the fade-in and the
// rise it makes while fading in, and the held press's progress strip.
const CHIP_PX = 18;
const CHIP_GAP_PX = 5;
const CHIP_PAD_PX = 2;
const CHIP_FADE_MS = 140;
const CHIP_RISE_PX = 4;
const CHIP_STRIP_PX = 2;
// The held press's gold.
const QUEUED_R = 1;
const QUEUED_G = 0.82;
const QUEUED_B = 0.25;

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
skillQueueChipReach

How far the chip's backing reaches above slot 1, in UI pixels (the
onboarding tour lights that area).
================
*/
export function skillQueueChipReach() {
	return CHIP_GAP_PX + 2 * CHIP_PAD_PX + CHIP_PX;
}

/*
================
skillQueueChip

The chip for the skill that casts next, centred above the slot rect anchor
(shortcut slot 1); null when nothing waits. It fades in while rising
CHIP_RISE_PX into place, on a dark backing with a breathing gold edge.
================
*/
export function skillQueueChip(
	game: SkillPressFeedbackState | null | undefined,
	anchor: UiRect,
	clip: UiRect,
	now: number
): SkillQueueChip | null {
	const queue = game?.skillQueue;
	if ( !queue ) return null;
	const alpha = Math.min( 1, Math.max( 0, (now - queue.sinceMs) / CHIP_FADE_MS ) );
	// Ease out: quick at first, settling into place.
	const rise = Math.round( CHIP_RISE_PX * (1 - alpha) * (1 - alpha) );
	const x = Math.round( anchor[0] + (anchor[2] - CHIP_PX) / 2 ),
		y = anchor[1] - CHIP_GAP_PX - CHIP_PAD_PX - CHIP_PX + rise;
	const icon: UiRect = [ x, y, CHIP_PX, CHIP_PX ];
	const backing: UiRect = [ x - CHIP_PAD_PX, y - CHIP_PAD_PX, CHIP_PX + 2 * CHIP_PAD_PX, CHIP_PX + 2 * CHIP_PAD_PX ];
	const under: UiQuad[] = [ {
		rect: backing,
		clip,
		color: [ 0, 0, 0, 0.6 * alpha ],
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
