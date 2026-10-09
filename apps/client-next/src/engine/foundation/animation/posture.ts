/*
===========================================================================

posture.ts - an actor's held posture: knockdown, recovery, emote, stall

Pure projection state, retained only by the character presentation owner.
A posture is a clip the actor holds over its locomotion: the knockdown fall
and its held wait, the wake-up, a one-shot emote, or the stall owner's
vendor loop (action state 0xF, CICharactor_Action_OpenVendor 8E6B50).

===========================================================================
*/
import type { CharacterLayer } from "@/engine/contracts/character";

// The 200 ms entry and exit blend (8E6B50 passes 0xC8, 0xC8).
const POSTURE_BLEND_SECONDS = .2;
// An emote's exit outlasts its clip by two blends before it releases.
const EMOTE_RELEASE_SECONDS = .4;
// A knockdown holds for its recovery time plus half a second.
const DOWN_RECOVERY_PAD_MS = 500;

export type Posture =
	| { readonly kind: "down"; readonly started: number; readonly recoverAt: number; }
	| { readonly kind: "recover"; readonly started: number; }
	| { readonly kind: "emote"; readonly started: number; readonly clip: string; }
	| { readonly kind: "vendor"; readonly started: number; };

export type PostureEvent =
	| { readonly kind: "down"; readonly at: number; readonly recoveryMs: number; }
	| { readonly kind: "emote"; readonly at: number; readonly action: number; }
	| { readonly kind: "vendor"; readonly at: number; readonly open: boolean; }
	| { readonly kind: "cancel"; }
	| { readonly kind: "tick"; readonly at: number; readonly duration: number; };

/*
================
transitionPosture

A stall (CICharactor_SetStallState 86A880) enters action state 0xF while it
is open and state 3 when it closes; it replaces an emote, never a knockdown.
The vendor loop has no end of its own, so a tick never releases it.
================
*/
export function transitionPosture( state: Posture | undefined, event: PostureEvent ): Posture | undefined {
	switch ( event.kind ) {
		case "cancel":
			return undefined;
		case "down":
			if ( !Number.isInteger( event.recoveryMs ) || event.recoveryMs < 0 ) {
				throw Error( "Missing native recovery duration" );
			}
			// SetMotionState does not restart a state that is already active.
			if ( state?.kind === "down" ) return state;
			return {
				kind: "down",
				started: event.at,
				recoverAt: event.at + (event.recoveryMs + DOWN_RECOVERY_PAD_MS) / 1000
			};
		case "emote":
			if ( state ) return state;
			return { kind: "emote", started: event.at, clip: `emote${event.action}` };
		case "vendor":
			if ( !event.open ) return state?.kind === "vendor" ? undefined : state;
			if ( state?.kind === "down" || state?.kind === "recover" || state?.kind === "vendor" ) return state;
			return { kind: "vendor", started: event.at };
		case "tick":
			if ( !state || state.kind === "vendor" ) return state;
			if ( state.kind === "down" ) {
				return event.at > state.recoverAt ? { kind: "recover", started: event.at } : state;
			}
			if ( state.kind === "emote" ) {
				return event.at - state.started >= event.duration + EMOTE_RELEASE_SECONDS ? undefined : state;
			}
			return event.at - state.started >= event.duration + POSTURE_BLEND_SECONDS ? undefined : state;
	}
}

/*
================
postureClip

The clip a posture plays, for its duration lookups.
================
*/
export function postureClip( state: Posture ): string {
	switch ( state.kind ) {
		case "down":
			return "down";
		case "recover":
			return "wakeup";
		case "emote":
			return state.clip;
		case "vendor":
			return "vendor";
	}
}

/*
================
postureLayers
================
*/
export function postureLayers( state: Posture, now: number, duration: number ): CharacterLayer[] {
	const age = Math.max( 0, now - state.started );
	if ( state.kind === "down" ) {
		const loop: CharacterLayer = { clip: "downwait", time: age, loop: true, weight: 1, lane: "timed" };
		if ( age >= duration ) return [ loop ];
		return [ { clip: "down", time: age, loop: false, weight: 1, lane: "event" }, loop ];
	}
	if ( state.kind === "vendor" ) {
		return [ {
			clip: "vendor",
			time: age,
			loop: true,
			weight: Math.min( 1, age / POSTURE_BLEND_SECONDS ),
			lane: "timed"
		} ];
	}
	// An emote holds its cursor through the entry blend; the wake-up enters at once.
	const entry = state.kind === "emote" ? POSTURE_BLEND_SECONDS : 0, cursor = Math.max( 0, age - entry );
	const exit = Math.max( 0, 1 - (cursor - duration) / POSTURE_BLEND_SECONDS );
	return [ {
		clip: postureClip( state ),
		time: Math.min( cursor, duration ),
		loop: false,
		weight: Math.min( 1, entry ? age / entry : 1, exit ),
		lane: "event"
	} ];
}
