/*
===========================================================================

skill-cooldowns.ts - the local player's skill cooldowns on the server's clock

776830 -> 67ADD0 -> 84AAC0. UI references query the same manager by skill or
nonzero cool-time group (84B010/84B0E0); cast retirement is independent.

Every row is placed where the server's cooldown runs, in local time. The
cast-start answer (B245) arrives one delivery after the server started the
cooldown, so an accepted row starts that delivery earlier. Between a press
and its answer a provisional row stands in: it starts when the press reaches
the server, so a re-press in that round trip is held or denied locally
instead of reaching a server whose cooldown already runs. The answer
replaces it; a refusal or a missing answer removes it.

===========================================================================
*/
import type { SkillMetadata } from "./skill-catalog";

// How long a finished row is kept for the slot's completion flash.
const RETAIN_AFTER_MS = 500;
const MAX_ROWS = 4096;

/*
================
SkillCooldown

provisionalUntilMs marks a stand-in for an unanswered press, dropped at
that time.
================
*/
export interface SkillCooldown {
	readonly skill: number;
	readonly group: number;
	readonly startedAtMs: number;
	readonly durationMs: number;
	readonly provisionalUntilMs?: number;
}

/*
================
skillCooldown

The running cooldown of skill (or its nonzero group) at now, or null.
================
*/
export function skillCooldown(
	rows: readonly SkillCooldown[],
	skill: number,
	group: number,
	now: number
): { remainingMs: number; fraction: number; } | null {
	const row = rows.find( r =>
		(r.skill === skill || !!group && r.group === group) && now < r.startedAtMs + r.durationMs
	);
	if ( !row ) return null;
	const remainingMs = Math.max( 0, row.startedAtMs + row.durationMs - now );
	return { remainingMs, fraction: Math.min( 1, remainingMs / row.durationMs ) };
}

/*
================
createSkillCooldowns
================
*/
export function createSkillCooldowns() {
	let rows: readonly SkillCooldown[] = [];
	/*
	================
	place

	Replace the row of skill and its group with one starting at startedAtMs.
	================
	*/
	function place( skill: SkillMetadata, startedAtMs: number, now: number, provisionalUntilMs?: number ) {
		if ( !skill.cooldownMs ) return;
		const group = skill.cooldownGroup ?? 0;
		const retained = rows.filter( r =>
			now < r.startedAtMs + r.durationMs + RETAIN_AFTER_MS && r.skill !== skill.id &&
			(!group || r.group !== group)
		);
		if ( retained.length >= MAX_ROWS ) throw Error( "Skill cooldown capacity exceeded" );
		rows = [ ...retained, {
			skill: skill.id,
			group,
			startedAtMs,
			durationMs: skill.cooldownMs,
			...(provisionalUntilMs === undefined ? {} : { provisionalUntilMs })
		} ];
	}
	return {
		/*
		================
		accepted

		The server started the cooldown at startedAtMs (local time).
		================
		*/
		accepted( skill: SkillMetadata, startedAtMs: number, now: number ) {
			place( skill, startedAtMs, now );
		},
		/*
		================
		pressed

		A sent press: the server starts the cooldown when the press reaches
		it, at arrivesAtMs. The row stands in until untilMs.
		================
		*/
		pressed( skill: SkillMetadata, arrivesAtMs: number, untilMs: number, now: number ) {
			place( skill, arrivesAtMs, now, untilMs );
		},
		/*
		================
		refused

		The server refused a press (B245 [2, code] names no skill): drop every
		stand-in.
		================
		*/
		refused() {
			const next = rows.filter( r => r.provisionalUntilMs === undefined );
			const changed = next.length !== rows.length;
			rows = next;
			return changed;
		},
		state: () => rows,
		/*
		================
		step

		Expire finished rows and unanswered stand-ins.
		================
		*/
		step( now: number ) {
			const next = rows.filter( r =>
				now < r.startedAtMs + r.durationMs + RETAIN_AFTER_MS &&
				(r.provisionalUntilMs === undefined || now < r.provisionalUntilMs)
			);
			if ( next.length === rows.length ) return false;
			rows = next;
			return true;
		},
		/*
		================
		clear
		================
		*/
		clear() {
			rows = [];
		}
	};
}
