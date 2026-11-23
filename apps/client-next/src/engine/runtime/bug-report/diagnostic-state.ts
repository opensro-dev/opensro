/*
===========================================================================

diagnostic-state.ts - state.json row shaping for bug reports

Owns the compact rows a report carries for actors (#2997) and owned COS
(#4222). Separated from bug-report.ts so the reporter's composition stays
under the source-size limit: these shapes are one responsibility (what
state.json names) and the reporter is another (when and how it files).

===========================================================================
*/

/*
================
DiagnosticState
================
*/
export interface DiagnosticState {
	readonly session: import("@/engine/contracts/session").SessionState | null;
	readonly gameplay: import("@/engine/contracts/gameplay").GameplayState | null;
	readonly target: import("@/engine/contracts/world").EntityState | undefined;
	readonly entities: number;
	// A thunk, not a list: state() runs on every HUD assembly (refusal) and
	// each timeline sample, and only a composed report needs the sorted poses.
	readonly actors: () => readonly DiagnosticActor[];
}

/*
================
DiagnosticCompanion

One owned COS as state.json carries it for pet/combat reports (#4222):
compact rows that survive the per-field size omit of fat cosRecords
(inventories). assistTarget is the GID the owner last ordered this
attack pet onto.
================
*/
export interface DiagnosticCompanion {
	readonly gid: number;
	readonly band: number;
	readonly hp: number;
	readonly status: number;
	readonly dead: boolean;
	readonly commandMode?: number;
	readonly assistTarget?: number;
}

/*
================
DiagnosticActor

One presented actor as state.json carries it: gid, kind, name and the
pose the client drew, so a pose complaint (a ground item or a pet
reported sunk, #2997) can be checked against the surface at its own XZ
instead of parked for want of a capture. Staff diagnostics only: the
shared archive (archive.ts) selects its state.json fields by name and
never selects actors, so names stay out of it.
================
*/
export interface DiagnosticActor {
	readonly gid: number;
	readonly kind: string;
	readonly name: string;
	readonly region: string;
	readonly x: number;
	readonly y: number;
	readonly z: number;
}

// How many actor poses one report carries, nearest the local player
// first: the actors a pose complaint is about stand next to the
// reporter, and the bound keeps state.json small next to the replay.
const MAX_DIAGNOSTIC_ACTORS = 512;

/*
================
diagnosticActors

The presented entities shaped for DiagnosticState: nearest the local
pose first (presented order when there is no local pose), capped at
MAX_DIAGNOSTIC_ACTORS, coordinates at millimetre precision so a
partial sink of well under a unit survives the rounding.
================
*/
export function diagnosticActors(
	entities: readonly import("@/engine/contracts/world").EntityState[],
	local: { readonly x: number; readonly z: number; } | null | undefined
): DiagnosticActor[] {
	const ordered = [ ...entities ];
	if ( local ) {
		ordered.sort( ( a, b ) =>
			((a.x - local.x) ** 2 + (a.z - local.z) ** 2) - ((b.x - local.x) ** 2 + (b.z - local.z) ** 2)
		);
	}
	return ordered.slice( 0, MAX_DIAGNOSTIC_ACTORS ).map( entity => ({
		gid: entity.gid,
		kind: entity.kind,
		name: entity.name,
		region: "0x" + entity.regionId.toString( 16 ).toUpperCase(),
		x: +entity.x.toFixed( 3 ),
		y: +entity.y.toFixed( 3 ),
		z: +entity.z.toFixed( 3 )
	}) );
}

/*
================
diagnosticCompanions

Owned COS rows for state.json (#4222): no inventories, so the pet
survives the per-field size omit that drops fat cosRecords.
================
*/
export function diagnosticCompanions(
	records: readonly import("@/engine/contracts/gameplay").CosRecord[] | null | undefined
): DiagnosticCompanion[] {
	if ( !records?.length ) return [];
	return records.map( record => ({
		gid: record.gid,
		band: record.band,
		hp: record.hp,
		status: record.status,
		dead: record.dead,
		...(record.commandMode !== undefined ? { commandMode: record.commandMode } : {}),
		...(record.assistTarget ? { assistTarget: record.assistTarget } : {})
	}) );
}
