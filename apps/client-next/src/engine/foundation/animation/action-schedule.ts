/*
===========================================================================

action-schedule.ts - skill phase callbacks and character animation layers

Owns READY/WAIT/SHOT presentation clocks. Server release and cancellation
remain authoritative; one-shot clip time excludes the native entry blend.

===========================================================================
*/
import { actionCursor, actionStageEvents } from "./action-time";
import type { AnimationMetadata } from "./animation-metadata";
import type { CharacterLayer } from "@/engine/contracts/character";
import { animationActivation, type AnimationActivation } from "./animation-activation";
const ACTION_BLEND_MS = 200;
const ACTION_BLEND_SECONDS = 0.2;

/*
================
ActionPhase
================
*/
export interface ActionPhase {
	readonly clip: string;
	readonly definition: AnimationMetadata;
}
/*
================
ActionSchedule
================
*/
export interface ActionSchedule {
	readonly phases: readonly (ActionPhase | null)[];
	phase: number;
	started: number;
	previous: number;
	entered: boolean;
	cancelledAt?: number;
	activation?: AnimationActivation;
	outgoing?: {
		phase: ActionPhase;
		started: number;
		stopped: number;
		loop: boolean;
		activation: AnimationActivation;
	}[];
}
/*
================
actionPhaseTime

AE0890..AE0905 consumes the entry countdown before integrating a one-shot
cursor. The cyclic WAIT cursor advances during its blend (AE05D0).
================
*/
function actionPhaseTime( age: number, loop: boolean ): number {
	return Math.max( 0, (age * 1000 - (loop ? 0 : ACTION_BLEND_MS)) / 1000 );
}

/*
================
actionSampleTime

ADF3A0 clamps one-shot sampling to length - 1, including its natural exit.
================
*/
function actionSampleTime( phase: ActionPhase, age: number, loop: boolean ): number {
	const cursor = actionPhaseTime( age, loop );
	return loop ? cursor : Math.min( cursor, (phase.definition.durationMs - 1) / 1000 );
}

/*
================
actionLayers

Pose sampling and key dispatch must use the same held clip cursor.
================
*/
export function actionLayers( clock: ActionSchedule, now: number ): CharacterLayer[] {
	clock.outgoing = clock.outgoing?.filter( row => now * 1000 < row.stopped * 1000 + ACTION_BLEND_MS );
	const layers: CharacterLayer[] = [];
	const phase = clock.phases[clock.phase];
	if ( phase && clock.cancelledAt === undefined ) {
		const age = Math.max( 0, now - clock.started ), weight = Math.min( 1, age / ACTION_BLEND_SECONDS );
		if ( weight > 0 ) {
			layers.push( {
				clip: phase.clip,
				time: actionSampleTime( phase, age, clock.phase === 1 ),
				loop: clock.phase === 1,
				weight,
				lane: "event",
				activation: clock.activation ??= animationActivation( clock.started )
			} );
		}
	}
	for ( const row of clock.outgoing ?? [] ) {
		const weight = Math.min( 1, Math.max( 0, row.stopped - row.started ) / ACTION_BLEND_SECONDS ) *
			Math.max( 0, 1 - (now * 1000 - row.stopped * 1000) / ACTION_BLEND_MS );
		if ( weight ) {
			layers.push( {
				clip: row.phase.clip,
				time: actionSampleTime( row.phase, now - row.started, row.loop ),
				loop: row.loop,
				weight,
				lane: "event",
				activation: row.activation
			} );
		}
	}
	return layers;
}
/*
================
advanceAction

8DF480/8DF430 dispatch stage zero on entry, even with no motion. Authored
keys and completion follow the one-shot entry hold; B505 can still release
WAIT or cancel before a natural animation boundary.
================
*/
export function advanceAction( clock: ActionSchedule, now: number, shotAt?: number, cancelledAt?: number ) {
	const events: { phase: string; event: number; at: number; }[] = [];
	/*
    ================
    retire
    ================
    */
	function retire( phase: ActionPhase, stopped: number ) {
		(clock.outgoing ??= []).push( {
			phase,
			started: clock.started,
			stopped,
			loop: clock.phase === 1,
			activation: clock.activation ??= animationActivation( clock.started )
		} );
	}
	if ( clock.cancelledAt !== undefined ) {
		return {
			events,
			phase: clock.phases[clock.phase],
			loop: false,
			time: actionPhaseTime( clock.cancelledAt - clock.started, clock.phase === 1 )
		};
	}
	const cancel = cancelledAt !== undefined && cancelledAt <= now ? cancelledAt : undefined;
	if ( cancel !== undefined ) now = Math.max( clock.started, cancel );
	while ( clock.phase < 3 ) {
		const phase = clock.phases[clock.phase],
			name = clock.phase === 0 ? "READY" : clock.phase === 1 ? "WAIT" : "SHOT";
		const release = clock.phase < 2 && clock.phases[1] && shotAt !== undefined && shotAt <= now ?
			Math.max( clock.started, shotAt ) :
			undefined;
		const until = release ?? now;
		if ( !clock.entered ) {
			events.push( { phase: name, event: 0, at: clock.started } );
			clock.entered = true;
		}
		if ( phase && clock.phase !== 1 ) {
			const cursor = actionCursor(
				actionPhaseTime( until - clock.started, false ) * 1000,
				phase.definition.durationMs
			);
			const marks = phase.definition.trackEvents.filter( row => row.eventCode === 1 );
			for (
				const event of actionStageEvents(
					phase.definition,
					clock.previous,
					Math.min( cursor, phase.definition.durationMs - 1 ),
					false
				)
			) {
				events.push( {
					phase: name,
					event,
					at: clock.started + (ACTION_BLEND_MS + marks[event - 1]!.cursorMs) / 1000
				} );
			}
			clock.previous = cursor;
			if ( cursor < phase.definition.durationMs && release === undefined ) break;
		} else if ( phase && release === undefined ) break;
		if ( phase ) {
			retire( phase, release ?? clock.started + (ACTION_BLEND_MS + phase.definition.durationMs) / 1000 );
		}
		if ( release !== undefined ) {
			clock.phase = 2;
			clock.started = release;
		} else {
			if ( phase ) clock.started += (ACTION_BLEND_MS + phase.definition.durationMs) / 1000;
			clock.phase++;
		}
		clock.previous = 0;
		clock.entered = false;
		clock.activation = undefined;
	}
	const phase = clock.phases[clock.phase];
	if ( cancel !== undefined ) {
		if ( phase ) retire( phase, cancel );
		clock.cancelledAt = cancel;
	}
	return {
		events,
		phase,
		loop: clock.phase === 1,
		time: phase ? actionPhaseTime( now - clock.started, clock.phase === 1 ) : 0
	};
}
