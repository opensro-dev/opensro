/*
===========================================================================

debug-clock.ts - the debug console's local clock (command.txt ids 100-109)

CGInterface_OnCommandMessage (690C40) drives the interface's own timer
for the time family, never the server's: timeoff holds it (+0x7AD),
timeon lets it run again, /time re-seeds it from an atof hour, day and
night set the day fraction, and the step rows move it while held - with
the native sign quirk that "/+" steps back. The port's clock is a worker
seed sampled on the main thread (world-clock.ts owns the seed math), so
this state lives with the UI and rewrites the seed the sampler sees.

===========================================================================
*/
import type { WorldClockSeed } from "@/engine/contracts/gameplay";
import { worldClockWallTime } from "./world-clock";

// One game day in minutes; a held clock steps by a twentieth of it.
const MINUTES_PER_DAY = 1440;
export const DEBUG_CLOCK_STEP_MINUTES = 72;
// One game second per 20 real milliseconds (world-clock.ts).
const MS_PER_GAME_SECOND = 20;

/*
================
DebugClockTime

A wall time on the world clock, seconds included so a hold keeps the
sub-minute phase the sampler would have shown.
================
*/
export interface DebugClockTime {
	readonly day: number;
	readonly hour: number;
	readonly minute: number;
	readonly second: number;
}

/*
================
DebugClock

The interface timer's debug state: held where timeoff left it, or based
where /time (or a resume) re-seeded it. Neither is the live server clock.
================
*/
export interface DebugClock {
	readonly held: boolean;
	readonly heldAt: DebugClockTime | null;
	readonly base: (DebugClockTime & { readonly setAtMs: number; }) | null;
}

/*
================
createDebugClock

The live clock: no hold, no re-seed.
================
*/
export function createDebugClock(): DebugClock {
	return { held: false, heldAt: null, base: null };
}

/*
================
normalizeDebugClockTime

A wall time folded into one day, the day count carrying the overflow.
================
*/
function normalizeDebugClockTime( day: number, minutes: number, second: number ): DebugClockTime {
	const days = Math.floor( minutes / MINUTES_PER_DAY );
	const within = minutes - days * MINUTES_PER_DAY;
	return {
		day: day + days,
		hour: Math.floor( within / 60 ),
		minute: within % 60,
		second
	};
}

/*
================
debugClockWallTimeNow

The wall time the sampler currently shows: the held time while held,
else the re-seeded clock, else the server's seed. Null before any clock.
================
*/
function debugClockWallTimeNow(
	state: DebugClock,
	seed: WorldClockSeed | undefined,
	now: number
): DebugClockTime | null {
	if ( state.held && state.heldAt ) {
		return state.heldAt;
	}
	if ( state.base ) {
		return worldClockWallTime(
			{
				day: state.base.day,
				hour: state.base.hour,
				minute: state.base.minute,
				receivedAtMs: state.base.setAtMs - state.base.second * MS_PER_GAME_SECOND
			},
			now
		);
	}
	return worldClockWallTime( seed, now );
}

/*
================
debugClockHold

timeoff: freeze the clock where it stands. With no clock yet there is
nothing to freeze; the hold lands on the first clock that arrives.
================
*/
export function debugClockHold(
	state: DebugClock,
	seed: WorldClockSeed | undefined,
	now: number
): DebugClock {
	if ( state.held ) {
		return state;
	}
	return { held: true, heldAt: debugClockWallTimeNow( state, seed, now ), base: state.base };
}

/*
================
debugClockResume

timeon: the clock runs again from where the hold left it.
================
*/
export function debugClockResume( state: DebugClock, now: number ): DebugClock {
	if ( !state.held ) {
		return state;
	}
	if ( !state.heldAt ) {
		return { held: false, heldAt: null, base: state.base };
	}
	return { held: false, heldAt: null, base: { ...state.heldAt, setAtMs: now } };
}

/*
================
debugClockSetMinutes

/time and the day/night setters: re-seed the clock at a minute of the
current day. The re-seed writes the timer's value; the hold is a
separate flag, so a held clock shows the new time still frozen and a
running one runs from it. /time reads an atof hour (hour truncated,
the fraction carried into minutes); day sets fraction 0.5, night 1.0.
================
*/
export function debugClockSetMinutes(
	state: DebugClock,
	seed: WorldClockSeed | undefined,
	now: number,
	minutes: number
): DebugClock {
	const current = debugClockWallTimeNow( state, seed, now );
	const time = normalizeDebugClockTime( current?.day ?? 0, minutes, 0 );
	if ( state.held ) {
		return { ...state, heldAt: time };
	}
	return { held: false, heldAt: null, base: { ...time, setAtMs: now } };
}

/*
================
debugClockSetHours

/time <hour>: the atof hour clamped to [0, 24], re-seeded as above.
================
*/
export function debugClockSetHours(
	state: DebugClock,
	seed: WorldClockSeed | undefined,
	now: number,
	hours: number
): DebugClock {
	const clamped = Math.min( 24, Math.max( 0, hours ) );
	const hour = Math.trunc( clamped );
	const minute = Math.trunc( (60 * clamped) % 60 );
	return debugClockSetMinutes( state, seed, now, hour * 60 + minute );
}

/*
================
debugClockSetFraction

The day/night rows: set the day fraction (0.5, 1.0) as minutes.
================
*/
export function debugClockSetFraction(
	state: DebugClock,
	seed: WorldClockSeed | undefined,
	now: number,
	fraction: number
): DebugClock {
	return debugClockSetMinutes( state, seed, now, Math.trunc( fraction * MINUTES_PER_DAY ) );
}

/*
================
debugClockStep

The step rows move a held clock by a twentieth of a day (the caller
passes the signed step; the native signs are swapped, "/+" back and
"/-" forward). A running clock ignores them.
================
*/
export function debugClockStep( state: DebugClock, deltaMinutes: number ): DebugClock {
	if ( !state.held || !state.heldAt ) {
		return state;
	}
	const minutes = state.heldAt.hour * 60 + state.heldAt.minute + deltaMinutes;
	return { ...state, heldAt: normalizeDebugClockTime( state.heldAt.day, minutes, state.heldAt.second ) };
}

/*
================
debugClockSeed

The seed the sampler sees: while held, the held time re-based at every
sample so it never advances (its seconds ride along as the sample's
phase); after a re-seed, the base time running at the native rate;
otherwise the server's seed untouched.
================
*/
export function debugClockSeed(
	state: DebugClock,
	seed: WorldClockSeed | undefined,
	now: number
): WorldClockSeed | undefined {
	if ( state.held && state.heldAt ) {
		return {
			day: state.heldAt.day,
			hour: state.heldAt.hour,
			minute: state.heldAt.minute,
			receivedAtMs: now - state.heldAt.second * MS_PER_GAME_SECOND
		};
	}
	if ( state.base ) {
		return {
			day: state.base.day,
			hour: state.base.hour,
			minute: state.base.minute,
			receivedAtMs: state.base.setAtMs - state.base.second * MS_PER_GAME_SECOND
		};
	}
	return seed;
}
