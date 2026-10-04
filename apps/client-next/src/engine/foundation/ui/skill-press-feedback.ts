/*
===========================================================================

skill-press-feedback.ts - what a shortcut slot shows for a held or denied
skill press

A press the client holds for its cooldown (skill-queue.ts) outlines its slot
with a pulsing gold border until it fires. A denied press (too long to wait)
tints the slot red and shakes it for DENIAL_FLASH_MS, beside the warning
sound, instead of sending a press the server would refuse. Presentation
only: a deliberate addition to the original's slots, which show neither.

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
	readonly skillQueue?: { readonly skill: number; readonly fireAtMs: number; };
	readonly skillDenied?: { readonly skill: number; readonly atMs: number; };
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
	if ( game?.skillQueue?.skill === skill ) {
		const pulse = 0.6 + 0.4 * Math.sin( now / PULSE_MS * 2 * Math.PI );
		const color: UiQuad["color"] = [ QUEUED_R, QUEUED_G, QUEUED_B, pulse ];
		const [x, y, w, h] = r;
		for (
			const edge of [
				[ x, y, w, OUTLINE_PX ],
				[ x, y + h - OUTLINE_PX, w, OUTLINE_PX ],
				[ x, y, OUTLINE_PX, h ],
				[ x + w - OUTLINE_PX, y, OUTLINE_PX, h ]
			] as const
		) quads.push( { rect: edge, clip, color, texture: "", uv: [ 0, 0, 1, 1 ] } );
	}
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
