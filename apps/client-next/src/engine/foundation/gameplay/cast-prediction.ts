/*
===========================================================================

cast-prediction.ts - the local cast's animation starts at the press

A deliberate deviation from the original, which starts a cast only when the
server's cast-start answer (B245) arrives, one round trip after the press.
When the client is all but certain the press will be accepted (gameplay.ts
predictCast: a ready skill, a target within its authored range, no other
cast in flight), it opens a prediction: a stand-in cast that only the
character presentation animates. Combat results, effects, sounds and
cooldowns still come from the server's cast alone.

The server's matching cast (same caster and skill) adopts the prediction:
it carries predictedToken, and the presentation moves the running action
clock to it, so the animation never restarts. A refusal, a queued answer,
another local cast opening first, or no answer by the deadline cancels the
prediction instead; it stays published for
ROLLBACK_MS with cancelledAtMs so the action blends out through the
ordinary cancellation path, never a snap.

Prediction tokens count down from PREDICTION_TOKEN_BASE: server tokens are
positive and -1 is a sentinel elsewhere (effects.ts).

===========================================================================
*/
import type { CastState } from "@/engine/contracts/gameplay";

const PREDICTION_TOKEN_BASE = -0x10000;
// How long a cancelled prediction stays published to blend out.
const ROLLBACK_MS = 500;

/*
================
Prediction
================
*/
interface Prediction {
	readonly cast: CastState;
	readonly deadlineMs: number;
}

/*
================
createCastPrediction

The local player's one open prediction.
================
*/
export function createCastPrediction() {
	let current: Prediction | null = null, serial = 0;
	/*
	================
	cancel
	================
	*/
	function cancel( now: number ): boolean {
		if ( !current || current.cast.cancelledAtMs !== undefined ) return false;
		current = { cast: { ...current.cast, cancelledAtMs: now }, deadlineMs: now + ROLLBACK_MS };
		return true;
	}
	return {
		/*
		================
		predict

		Open a prediction for caster's press of skill at target (0: none),
		answered by deadlineMs at the latest. Replaces an open one.
		================
		*/
		predict( caster: number, skill: number, target: number, now: number, deadlineMs: number ): CastState {
			serial = (serial + 1) % 0x10000;
			const cast: CastState = {
				token: PREDICTION_TOKEN_BASE - serial,
				caster,
				skill,
				target,
				damage: 0,
				fatal: false,
				receivedAtMs: now
			};
			current = { cast, deadlineMs };
			return cast;
		},
		/*
		================
		adopt

		The server's cast of skill by caster: the token of the prediction it
		takes over, removed from the plane, or undefined.
		================
		*/
		adopt( caster: number, skill: number ): number | undefined {
			const cast = current?.cast;
			if ( !cast || cast.cancelledAtMs !== undefined || cast.caster !== caster || cast.skill !== skill ) {
				return undefined;
			}
			current = null;
			return cast.token;
		},
		/*
		================
		cancel

		The press will not start its cast now: the server refused it (B245
		[2, code]), queued it behind an open command, or another cast of the
		caster opened first.
		================
		*/
		cancel,
		/*
		================
		step

		Cancel an unanswered prediction at its deadline; drop a cancelled one
		once it has blended out. True when the plane changed.
		================
		*/
		step( now: number ): boolean {
			if ( !current || now < current.deadlineMs ) return false;
			if ( current.cast.cancelledAtMs === undefined ) return cancel( now );
			current = null;
			return true;
		},
		state: (): CastState | undefined => current?.cast,
		open: (): boolean => !!current && current.cast.cancelledAtMs === undefined,
		/*
		================
		clear
		================
		*/
		clear() {
			current = null;
		}
	};
}
