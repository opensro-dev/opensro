/*
===========================================================================

world-entry-gate.ts - what the world entry waits on, by name and by time

The loading screen holds after world entry until nine conditions are all
true at once (runtime.ts worldReady): the scene, its visible groups, their
admission, their textures, the local character, navigation, audio and the
character and interface baselines. The diagnostics said only "loading
none" while it held, so a 21 s entry could not say which condition cost
it. This owner records, for one entry, when each condition last became
true (the moment it stopped blocking) and names the ones still false.

It decides nothing: runtime.ts computes the conditions and still sends
world-ready itself. This module only keeps the timeline.

===========================================================================
*/
import type { WorldEntryCondition, WorldEntryReport } from "@/engine/contracts/runtime";

/*
================
WORLD_ENTRY_CONDITIONS

The conditions in runtime.ts's worldReady, in its order.
================
*/
export const WORLD_ENTRY_CONDITIONS = [
	"scene",
	"visible groups",
	"group admission",
	"textures",
	"local character",
	"navigation",
	"audio",
	"character baseline",
	"interface baseline"
] as const satisfies readonly WorldEntryCondition[];

export type WorldEntryConditions = Readonly<Record<WorldEntryCondition, boolean>>;

// The slowest conditions the diagnostics line names once the entry is done.
const SLOWEST_LISTED = 3;

/*
================
createWorldEntryGate
================
*/
export function createWorldEntryGate() {
	let start: number | null = null, finished: number | null = null, now = 0;
	let held: WorldEntryConditions | null = null;
	const settled: Partial<Record<WorldEntryCondition, number>> = {};
	return {
		/*
		================
		step

		One frame. entering is true from the frame the session is in the
		world until world-ready is sent; a new entry (entering rising again)
		starts a new timeline.
		================
		*/
		step( entering: boolean, conditions: WorldEntryConditions, at: number ) {
			if ( !entering ) {
				if ( start !== null && finished === null ) finished = at;
				return;
			}
			if ( start === null || finished !== null ) {
				start = at;
				finished = null;
				held = null;
				for ( const name of WORLD_ENTRY_CONDITIONS ) delete settled[name];
			}
			now = at;
			for ( const name of WORLD_ENTRY_CONDITIONS ) {
				if ( conditions[name] && !held?.[name] ) settled[name] = at - start;
				if ( !conditions[name] ) delete settled[name];
			}
			held = conditions;
			if ( WORLD_ENTRY_CONDITIONS.every( name => conditions[name] ) ) finished = at;
		},
		/*
		================
		report

		Null before the first entry.
		================
		*/
		report(): WorldEntryReport | null {
			if ( start === null ) return null;
			const end = finished ?? now;
			return {
				ready: finished !== null && WORLD_ENTRY_CONDITIONS.every( name => settled[name] !== undefined ),
				elapsedMs: end - start,
				waiting: WORLD_ENTRY_CONDITIONS.filter( name => settled[name] === undefined ),
				settledMs: { ...settled }
			};
		}
	};
}

/*
================
worldEntryLine

The diagnostics line: what is still waited on, or the slowest conditions
of a finished entry.
================
*/
export function worldEntryLine( report: WorldEntryReport | null ): string {
	if ( !report ) return "World entry: none";
	const seconds = ( ms: number ) => (ms / 1000).toFixed( 1 ) + " s";
	if ( report.waiting.length ) {
		return `World entry: waiting ${seconds( report.elapsedMs )} on ${report.waiting.join( ", " )}`;
	}
	const slowest = Object.entries( report.settledMs ).sort( ( a, b ) => b[1]! - a[1]! ).slice( 0, SLOWEST_LISTED );
	return `World entry: ready in ${seconds( report.elapsedMs )}; last ${
		slowest.map( ( [name, ms] ) => `${name} ${seconds( ms! )}` ).join( ", " )
	}`;
}
