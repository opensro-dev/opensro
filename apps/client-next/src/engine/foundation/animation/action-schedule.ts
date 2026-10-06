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
	caster?: number;
	wait?: ActionInstallation;
	initialized?: boolean;
	readonly phases: readonly (ActionPhase | null)[];
	phase: number;
	started: number;
	previous: number;
	entered: boolean;
	// The actor rate changes independently; an installation captures it once.
	animationRate?: number;
	phaseRate?: number;
	phaseSuperseded?: boolean;
	cancelledAt?: number;
	activation?: AnimationActivation;
	outgoing?: ActionInstallation[];
}
/*
================
ActionInstallation

The animation mixer owns an installed clip independently of the skill's
command stage. A WAIT is installed before READY (8E06E0).
================
*/
interface ActionInstallation {
	phase: ActionPhase;
	started: number;
	stopped: number;
	loop: boolean;
	rate: number;
	exitRate?: number;
	activation: AnimationActivation;
}
/*
================
reconcileActionInstallations

ADECF0 resets an existing installation rather than inserting a duplicate.
An actor's motion binding owns that identity; cast tokens do not. Retire the
replaced producer permanently so it cannot return after the new clip ends.
================
*/
export function reconcileActionInstallations( clocks: Iterable<ActionSchedule> ): void {
	const owners = new Map<number | undefined, Map<string, { started: number; remove: () => void; }>>();
	for ( const clock of clocks ) {
		let actor = owners.get( clock.caster );
		if ( !actor ) owners.set( clock.caster, actor = new Map() );
		/*
		================
		install
		================
		*/
		function install( clip: string, started: number, remove: () => void ) {
			const previous = actor!.get( clip );
			if ( previous && previous.started > started ) {
				remove();
				return;
			}
			previous?.remove();
			actor!.set( clip, { started, remove } );
		}
		const phase = clock.phases[clock.phase];
		if (
			phase && clock.phase !== 1 && clock.entered && !clock.phaseSuperseded && clock.cancelledAt === undefined
		) {
			install( phase.clip, clock.started, () => {
				clock.phaseSuperseded = true;
			} );
		}
		const wait = clock.wait;
		if ( wait ) {
			install( wait.phase.clip, wait.started, () => {
				clock.wait = undefined;
			} );
		}
		for ( const row of clock.outgoing ?? [] ) {
			install( row.phase.clip, row.started, () => {
				clock.outgoing = clock.outgoing?.filter( candidate => candidate !== row );
			} );
		}
	}
}
/*
================
actionPhaseTime

AE0890..AE0905 consumes the entry countdown before integrating a one-shot
cursor. The cyclic WAIT cursor advances during its blend (AE05D0).
================
*/
function actionPhaseTime( age: number, loop: boolean, rate = 1 ): number {
	return Math.max( 0, (age * 1000 - (loop ? 0 : ACTION_BLEND_MS)) * rate / 1000 );
}

/*
================
actionSampleTime

ADF3A0 clamps one-shot sampling to length - 1, including its natural exit.
================
*/
function actionSampleTime( phase: ActionPhase, age: number, loop: boolean, rate = 1 ): number {
	const cursor = actionPhaseTime( age, loop, rate );
	return loop ? cursor : Math.min( cursor, (phase.definition.durationMs - 1) / 1000 );
}

/*
================
actionLayers

Pose sampling and key dispatch must use the same held clip cursor.
================
*/
export function actionLayers( clock: ActionSchedule, now: number ): CharacterLayer[] {
	clock.outgoing = clock.outgoing?.filter( row =>
		now * 1000 < row.stopped * 1000 + ACTION_BLEND_MS / (row.exitRate ?? (row.loop ? row.rate : 1))
	);
	const layers: CharacterLayer[] = [];
	const phase = clock.phases[clock.phase];
	if ( phase && clock.phase !== 1 && !clock.phaseSuperseded && clock.cancelledAt === undefined ) {
		const age = Math.max( 0, now - clock.started ), weight = Math.min( 1, age / ACTION_BLEND_SECONDS );
		if ( weight > 0 ) {
			layers.push( {
				clip: phase.clip,
				time: actionSampleTime( phase, age, clock.phase === 1, clock.phaseRate ),
				loop: clock.phase === 1,
				rate: clock.phaseRate ?? 1,
				weight,
				lane: "event",
				activation: clock.activation ??= animationActivation( clock.started )
			} );
		}
	}
	for ( const row of [ ...(clock.wait ? [ clock.wait ] : []), ...(clock.outgoing ?? []) ] ) {
		const weight = Math.min(
			1,
			Math.max( 0, Math.min( now, row.stopped ) - row.started ) * (row.loop ? row.rate : 1) /
				ACTION_BLEND_SECONDS
		) *
			Math.min(
				1,
				Math.max(
					0,
					1 - (now * 1000 - row.stopped * 1000) * (row.exitRate ?? (row.loop ? row.rate : 1)) /
							ACTION_BLEND_MS
				)
			);
		if ( weight ) {
			layers.push( {
				clip: row.phase.clip,
				time: actionSampleTime( row.phase, now - row.started, row.loop, row.rate ),
				loop: row.loop,
				rate: row.rate,
				weight,
				lane: row.loop ? "timed" : "event",
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
	if ( !clock.initialized ) {
		clock.initialized = true;
		const wait = clock.phases[1];
		if ( wait ) {
			clock.wait = {
				phase: wait,
				started: clock.started,
				stopped: Infinity,
				loop: true,
				rate: clock.animationRate ?? 1,
				activation: animationActivation( clock.started )
			};
		}
	}
	/*
    ================
    retire
    ================
    */
	function retire( phase: ActionPhase, stopped: number, cancelled = false ) {
		if ( clock.phase === 1 || clock.phaseSuperseded ) return;
		// 8D97D0 retires WAIT by ID, then installs SHOT. ADECF0 leaves a
		// different earlier one-shot in the mixer's event list.
		const rate = clock.phaseRate ?? 1;
		let exitRate = 1;
		// AE08E4 changes back to state 4 when the entry countdown expires,
		// even if ADF370 requested state 9 during that countdown.
		if ( clock.phase === 0 || cancelled && (stopped - clock.started) * 1000 < ACTION_BLEND_MS ) {
			stopped = clock.started + (ACTION_BLEND_MS + phase.definition.durationMs / rate) / 1000;
		} else if ( cancelled ) {
			// AE0B5F consumes the integer cursor advance, then multiplies by
			// rate again. Natural exit uses the unscaled wall-clock countdown.
			exitRate = rate * rate;
		}
		(clock.outgoing ??= []).push( {
			phase,
			started: clock.started,
			stopped,
			loop: clock.phase === 1,
			rate,
			exitRate,
			activation: clock.activation ??= animationActivation( clock.started )
		} );
	}
	if ( clock.cancelledAt !== undefined ) {
		return {
			events,
			phase: clock.phases[clock.phase],
			loop: false,
			time: actionPhaseTime( clock.cancelledAt - clock.started, clock.phase === 1, clock.phaseRate )
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
		if ( clock.phaseSuperseded && release === undefined ) break;
		if ( !clock.entered ) {
			clock.phaseRate = clock.animationRate ?? 1;
			events.push( { phase: name, event: 0, at: clock.started } );
			clock.entered = true;
		}
		if ( phase && clock.phase !== 1 && !clock.phaseSuperseded ) {
			const cursor = actionCursor(
				actionPhaseTime( until - clock.started, false, clock.phaseRate ) * 1000,
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
					at: clock.started + (ACTION_BLEND_MS + marks[event - 1]!.cursorMs / (clock.phaseRate ?? 1)) / 1000
				} );
			}
			clock.previous = cursor;
			if ( cursor < phase.definition.durationMs && release === undefined ) break;
		} else if ( phase && release === undefined ) break;
		if ( phase ) {
			retire(
				phase,
				release ??
					clock.started + (ACTION_BLEND_MS + phase.definition.durationMs / (clock.phaseRate ?? 1)) / 1000
			);
		}
		if ( release !== undefined ) {
			if ( clock.wait ) {
				clock.wait.stopped = release;
				(clock.outgoing ??= []).push( clock.wait );
				clock.wait = undefined;
			}
			clock.phase = 2;
			clock.started = release;
		} else {
			if ( phase ) {
				clock.started += (ACTION_BLEND_MS + phase.definition.durationMs / (clock.phaseRate ?? 1)) / 1000;
			}
			clock.phase++;
		}
		clock.previous = 0;
		clock.entered = false;
		clock.phaseSuperseded = false;
		clock.activation = undefined;
	}
	const phase = clock.phases[clock.phase];
	if ( cancel !== undefined ) {
		if ( clock.wait ) {
			clock.wait.stopped = cancel;
			(clock.outgoing ??= []).push( clock.wait );
			clock.wait = undefined;
		}
		if ( phase ) retire( phase, cancel, true );
		clock.cancelledAt = cancel;
	}
	return {
		events,
		phase,
		loop: clock.phase === 1,
		time: phase ? actionPhaseTime( now - clock.started, clock.phase === 1, clock.phaseRate ) : 0
	};
}
