/*
===========================================================================

skill-catalog.ts - skill rows the client reads, and training rules

Validates the published skill catalog and answers training questions
(skill and mastery prerequisites, costs). skillMetadataById is the one
id lookup UI code uses.

===========================================================================
*/
import { parsePressAdmit, type PressAdmit } from "./press-admission";
import type { Progression } from "./progression";
/*
================
SkillMetadata
================
*/
export interface SkillMetadata {
	readonly id: number;
	readonly group: number;
	readonly level: number;
	readonly name: string;
	readonly nameSymbol?: string;
	readonly icon?: string;
	readonly buffSecondary?: boolean;
	// hste/hst2 (+0x94/+0x98): 6DE630 lets only the last active one apply.
	readonly speedBuff?: { readonly active: boolean; };
	// hide (+0x1F0) and dttp (+0x1EC), each [mask, level] (8608A0 / 85CE40).
	readonly hide?: StatusLevel;
	readonly detect?: StatusLevel;
	// dtt (+0x1E8): the viewer's own detection (85CC70). detectRange is the
	// radius of a shape-1 efr (+0x64); 0 reaches every distance.
	readonly sight?: StatusLevel;
	readonly detectRange?: number;
	readonly buffCancel?: "blocked" | "direct" | "confirm";
	readonly buffCancelInstance?: boolean;
	readonly spCost: number;
	readonly reqStr?: number;
	readonly reqInt?: number;
	readonly trainable: boolean;
	readonly targetRequired: boolean;
	// Column 26 (TargetGroup_Self) of a target-required row: the caster is an
	// admitted target, so a cast with nothing selected aims at the caster.
	readonly targetSelf?: boolean;
	readonly groundTarget?: boolean;
	readonly cooldownMs: number;
	// Preparation + casting + recovery (columns 11 + 12 + 13): the action
	// actor's lifetime, which holds the caster's action state 2 (cast-motion-lock).
	readonly actionMs?: number;
	// An ordinary cast (activity 2): it stops the caster's walk where it stands
	// (InitiateSkillCast 59B5F6 server side, CICharactor_Action_CastSkill
	// 8E67E0 client side). Instant rows (imbues, speed skills) keep walking.
	readonly haltsWalk?: boolean;
	// Cast gate ao or pw: refused while seated, behind a wall or riding
	// (58E0BF, 0x3009, which the client answers with no notice).
	readonly needsFooting?: boolean;
	// The authored action range (column 21), absent when the weapon sets the
	// reach. A target within it is always in the server's reach, which adds
	// both bodies (cast-prediction.ts).
	readonly range?: number;
	// The authored MP cost: flat plus percent of maximum MP (skillMpCost).
	readonly mp?: number;
	readonly mpPercent?: number;
	// The authored target groups as SKILL_TARGET_* bits (columns 22..33).
	readonly targets?: number;
	// A Force wall's cast: the server never releases its WAIT while the wall
	// stands, so the caster stays in action state 2, rooted (cast-motion-lock).
	readonly holdsCaster?: boolean;
	// The row's 58D8F0 inputs (press-admission.ts): the local press is
	// predicted only when the server will admit it.
	readonly admit?: PressAdmit;
	readonly cooldownGroup?: number;
	readonly masteries: readonly Requirement[];
	readonly prerequisites: readonly Requirement[];
}
/*
================
StatusLevel
================
*/
export interface StatusLevel {
	readonly mask: number;
	readonly level: number;
}
interface Requirement {
	readonly ID: number;
	readonly Level: number;
}
// SkillMetadata.targets bits (server enterworld SkillUiTarget).
export const SKILL_TARGET_SELF = 1;
export const SKILL_TARGET_MONSTER = 4;
export const SKILL_TARGET_DEAD_BODY = 128;
/*
================
skillAdmitsPredictedTarget

Whether a press at target is one the server all but surely starts: the
caster itself for a row that admits its caster, a living monster for a row
that admits monsters, a corpse for a corpse row. Players, NPCs and pets are
the server's call (relations, towns); the native press (6FCD50) sends them
without any local effect, so the client predicts nothing for them.
================
*/
export function skillAdmitsPredictedTarget(
	skill: SkillMetadata,
	target: { readonly gid: number; readonly kind: string; readonly appearanceState?: readonly number[]; },
	localGid: number
): boolean {
	const targets = skill.targets ?? 0;
	if ( target.gid === localGid ) return (targets & SKILL_TARGET_SELF) !== 0;
	if ( target.kind !== "monster" ) return false;
	const dead = target.appearanceState?.[0] === 2;
	return (targets & (dead ? SKILL_TARGET_DEAD_BODY : SKILL_TARGET_MONSTER)) !== 0;
}
/*
================
skillMpCost

The MP a cast of skill takes from a caster with maxMp, before the caster's
consumption rate (parameter 0x8D), which only the server knows: flat plus a
truncated percent of maximum MP (58E20A..58E2B1, skillcost.go
resourceCostAt). A caster whose rate lowers the cost may pay less.
================
*/
export function skillMpCost( skill: SkillMetadata, maxMp: number ): number {
	return (skill.mp ?? 0) + Math.trunc( maxMp * (skill.mpPercent ?? 0) / 100 );
}
export function skillCatalog( value: unknown ): readonly SkillMetadata[] {
	const source = (value as {
		refSkillSnapshot?: unknown[];
	}).refSkillSnapshot ?? [];
	if ( !Array.isArray( source ) || source.length > 65536 ) {
		throw Error( "Invalid skill catalogue" );
	}
	const seen = new Set<number>();
	function uint( value: unknown, max = 0xffffffff ): number {
		if ( typeof value !== "number" || !Number.isSafeInteger( value ) || value < 0 || value > max ) {
			throw Error( "Invalid skill metadata scalar" );
		}
		return value;
	}
	function requirements( value: unknown, count: number ): Requirement[] {
		if ( !Array.isArray( value ) || value.length !== count ) {
			throw Error(
				`Invalid skill requirements: expected ${count} entries, received ${
					Array.isArray( value ) ? value.length : typeof value
				}`
			);
		}
		return value.map( r => ({ ID: uint( r.ID ), Level: uint( r.Level, 255 ) }) );
	}
	const result: SkillMetadata[] = [];
	for ( const raw of source ) {
		const row = raw as {
			id: number;
			group: number;
			level: number;
			ui?: SkillMetadata;
		};
		if ( !row.ui ) {
			continue;
		}
		const id = uint( row.id ), ui = row.ui;
		if (
			!id || seen.has( id ) || typeof ui.name !== "string" || ui.name.length > 256 ||
			![ "trainable", "targetRequired" ].every( k => typeof ui[k as keyof SkillMetadata] === "boolean" )
		) {
			throw Error( "Invalid skill metadata" );
		}
		if ( ui.buffSecondary !== undefined && typeof ui.buffSecondary !== "boolean" ) {
			throw Error( "Invalid buff classification" );
		}
		if (
			ui.buffCancel !== undefined && ![ "blocked", "direct", "confirm" ].includes( ui.buffCancel ) ||
			ui.buffCancelInstance !== undefined && typeof ui.buffCancelInstance !== "boolean"
		) throw Error( "Invalid buff cancellation metadata" );
		if (
			ui.speedBuff !== undefined &&
			(typeof ui.speedBuff !== "object" || ui.speedBuff === null || typeof ui.speedBuff.active !== "boolean")
		) throw Error( "Invalid speed buff metadata" );
		const statusLevel = ( value: StatusLevel | undefined ) =>
			value === undefined ? undefined : { mask: uint( value?.mask ), level: uint( value?.level ) };
		if (
			ui.groundTarget !== undefined && typeof ui.groundTarget !== "boolean" ||
			ui.targetSelf !== undefined && typeof ui.targetSelf !== "boolean" ||
			ui.haltsWalk !== undefined && typeof ui.haltsWalk !== "boolean" ||
			ui.needsFooting !== undefined && typeof ui.needsFooting !== "boolean" ||
			ui.range !== undefined && (typeof ui.range !== "number" || !Number.isFinite( ui.range ) || ui.range < 0)
		) {
			throw Error( "Invalid skill target kind" );
		}
		seen.add( id );
		if ( !Array.isArray( ui.masteries ) || !Array.isArray( ui.prerequisites ) ) {
			throw Error( `Incomplete skill ${id} requirement metadata (${Object.keys( ui ).join( "," )})` );
		}
		for ( const field of [ ui.nameSymbol, ui.icon ] ) {
			if ( field !== undefined && (typeof field !== "string" || field.length > 256) ) {
				throw Error( "Invalid skill presentation metadata" );
			}
		}
		result.push( {
			id,
			group: uint( row.group ),
			level: uint( row.level, 255 ),
			name: ui.name,
			nameSymbol: ui.nameSymbol,
			icon: ui.icon,
			buffSecondary: ui.buffSecondary ?? false,
			speedBuff: ui.speedBuff && { active: ui.speedBuff.active },
			hide: statusLevel( ui.hide ),
			detect: statusLevel( ui.detect ),
			sight: statusLevel( ui.sight ),
			detectRange: uint( ui.detectRange ?? 0 ),
			buffCancel: ui.buffCancel ?? "direct",
			buffCancelInstance: ui.buffCancelInstance,
			spCost: uint( ui.spCost ),
			reqStr: uint( ui.reqStr ?? 0, 65535 ),
			reqInt: uint( ui.reqInt ?? 0, 65535 ),
			trainable: ui.trainable,
			targetRequired: ui.targetRequired,
			targetSelf: ui.targetSelf ?? false,
			groundTarget: ui.groundTarget ?? false,
			cooldownMs: uint( ui.cooldownMs ),
			...(ui.actionMs === undefined ? {} : { actionMs: uint( ui.actionMs ) }),
			haltsWalk: ui.haltsWalk ?? false,
			...(ui.needsFooting ? { needsFooting: true } : {}),
			...(ui.range ? { range: ui.range } : {}),
			...(ui.mp ? { mp: uint( ui.mp ) } : {}),
			...(ui.mpPercent ? { mpPercent: uint( ui.mpPercent, 65535 ) } : {}),
			...(ui.targets ? { targets: uint( ui.targets, 0xffff ) } : {}),
			...(ui.holdsCaster === true ? { holdsCaster: true } : {}),
			...(ui.admit === undefined ? {} : { admit: parsePressAdmit( ui.admit ) }),
			cooldownGroup: uint( ui.cooldownGroup ?? 0, 255 ),
			masteries: requirements( ui.masteries, 2 ),
			prerequisites: requirements( ui.prerequisites, 3 )
		} );
	}
	return result;
}
// UI eligibility is advisory; the server rechecks the same requirements.
// One admitted catalog/learned snapshot supplies all visible slots. Never scan
// the entire catalog again for each icon or prerequisite in a frame.
/*
================
skillMetadataById

One catalog row by skill id. Presentation publishes skillIndex beside the
catalog; a snapshot built without it (a test fixture, a preview) has the
same rows and is searched instead.
================
*/
export function skillMetadataById(
	game: {
		readonly skillCatalog?: readonly SkillMetadata[];
		readonly skillIndex?: ReadonlyMap<number, SkillMetadata>;
	},
	id: number
): SkillMetadata | undefined {
	return game.skillIndex ? game.skillIndex.get( id ) : game.skillCatalog?.find( row => row.id === id );
}
export function createSkillTrainingContext( learned: readonly number[], catalog: readonly SkillMetadata[] ) {
	const ids = new Set( learned ), groups = new Map<number, number>(), byId = new Map<number, SkillMetadata>();
	for ( const ref of catalog ) {
		byId.set( ref.id, ref );
		if ( ids.has( ref.id ) ) groups.set( ref.group, Math.max( groups.get( ref.group ) ?? 0, ref.level ) );
	}
	return {
		skill: ( id: number | undefined ) => id === undefined ? undefined : byId.get( id ),
		learned: ( id: number ) => ids.has( id ),
		hasGroupLevel: ( id: number, level: number ) => groups.has( id ) && groups.get( id )! >= level,
		reason( row: SkillMetadata, progression: Progression ): string | null {
			if ( !row.trainable ) {
				return "This skill cannot be trained";
			}
			if ( ids.has( row.id ) ) {
				return "Already learned";
			}
			if ( row.level !== (groups.get( row.group ) ?? 0) + 1 ) {
				return "Train the preceding level first";
			}
			if (
				row.masteries.some( r =>
					r.ID && !progression.masteries.some( m => m.id === r.ID && m.level >= r.Level )
				)
			) {
				return "Mastery level is too low";
			}
			if ( row.prerequisites.some( r => r.ID && (groups.get( r.ID ) ?? 0) < r.Level ) ) {
				return "Prerequisite skill is missing";
			}
			if ( (row.reqStr ?? 0) > (progression.stats?.strength ?? 0) ) return "Strength is too low";
			if ( (row.reqInt ?? 0) > (progression.stats?.intellect ?? 0) ) return "Intelligence is too low";
			if ( progression.skillPoints === undefined || progression.skillPoints < row.spCost ) {
				return "Insufficient skill points";
			}
			return null;
		}
	};
}
export function skillTrainingReason(
	row: SkillMetadata,
	learned: readonly number[],
	catalog: readonly SkillMetadata[],
	progression: Progression
): string | null {
	return createSkillTrainingContext( learned, catalog ).reason( row, progression );
}
export function trainingRequest( kind: "skill-train" | "mastery-train", id: number ) {
	if ( !Number.isInteger( id ) || id <= 0 || id > 0xffffffff ) {
		throw Error( "Invalid training reference" );
	}
	const payload = new Uint8Array( kind === "mastery-train" ? 5 : 4 );
	new DataView( payload.buffer ).setUint32( 0, id, true );
	if ( payload.length === 5 ) {
		payload[4] = 1;
	}
	return { opcode: kind === "skill-train" ? 0x72cb : 0x7165, payload };
}

// 5841D0 / 55AB40: price CURRENT mastery level, with a free zero-level branch.
export function masteryTrainingReason(
	id: number,
	progression: Progression,
	costs: Readonly<Record<number, number>>
): string | null {
	const row = progression.masteries.find( m => m.id === id );
	if ( !row ) return "Mastery is not available";
	if ( progression.level === undefined || row.level >= progression.level || row.level >= 120 ) {
		return "Character level is too low";
	}
	if ( row.level === 0 ) return null;
	const cost = costs[row.level];
	if ( cost === undefined ) return "Mastery price is unavailable";
	return progression.skillPoints === undefined || progression.skillPoints < cost ? "Insufficient skill points" : null;
}
export function masteryCosts( value: unknown ): Readonly<Record<number, number>> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) throw Error( "Invalid mastery cost table" );
	const result: Record<number, number> = {};
	for ( const [key, row] of Object.entries( value ) ) {
		const level = Number( key ), cost = (row as { masteryTrainSpCost?: unknown; })?.masteryTrainSpCost;
		if (
			!Number.isInteger( level ) || level < 1 || level > 255 || typeof cost !== "number" ||
			!Number.isSafeInteger( cost ) || cost < 0
		) throw Error( "Invalid mastery cost row" );
		result[level] = cost;
	}
	return result;
}
