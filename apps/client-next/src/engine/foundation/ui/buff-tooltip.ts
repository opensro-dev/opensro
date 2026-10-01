/*
===========================================================================

buff-tooltip.ts - native status and attached-effect help for every buff owner

The local board receives private abnormal power, grade and duration records.
Target and pet viewers receive only their own public grades; party viewers
have no character record. Keep those sources separate when formatting help.

===========================================================================
*/
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { UiHelpSource } from "@/engine/contracts/ui";
import { effectRemainingMs } from "../gameplay/attached-effects";
import type { TooltipSkillCatalog } from "./skill-tooltip-data";
import type { TooltipRow } from "./tooltip-rows";
import { skillParameterFormatters } from "./skill-tooltip-params";
import { statusCodes } from "../animation/status-presentation";

const TOOLTIP_WHITE = 0xffffffff;
const INFINITE_ABNORMAL_BIT = 24;
const MILLISECONDS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;
const SECONDS_PER_HOUR = 3600;
const SECONDS_PER_DAY = 86400;
const HOURS_PER_DAY = 24;
const RESTRICTION_MASK = 0x1c3;
const WEAKENING_MASK = 0x1e281c;
const STUN_BIT = 14;

/*
================
abnormalTooltip

6DE6F0 uses the code/category variants of 6E0010, not the texture name.
The value in a viewer heading is a grade, never elemental power.
================
*/
export function abnormalTooltip( bit: number, grade: number, text: ( key: string ) => string ): readonly TooltipRow[] {
	const code = statusCodes()[bit];
	if ( !code ) return [];
	const category = (RESTRICTION_MASK & (2 ** bit)) !== 0 ?
		"PARAM_RESTRICTION" :
		(WEAKENING_MASK & (2 ** bit)) !== 0 ?
		"PARAM_WEAKLY" :
		bit === STUN_BIT ?
		"PARAM_ETC" :
		"PARAM_CURSING";
	const name = text( "PARAM_" + code );
	const description = text( "DE_UIIT_MSG_STATE_SKILL_CURSING_" + code );
	if ( !name ) return [];
	return [
		{ value: name + (grade ? " " + grade + text( "UIIT_STT_GRADE" ) : ""), color: TOOLTIP_WHITE },
		{ value: text( category ) + " " + text( "UIIT_STT_MACROPOTION_ABNORMAL" ), color: TOOLTIP_WHITE },
		...(description ? [ { value: "\n" + description, color: TOOLTIP_WHITE } ] : [])
	];
}

/*
================
appendRemainingTime

Use one duration formatter for the local board's abnormal and skill rows.
Zero or expired durations have no time line.
================
*/
function appendRemainingTime( rows: TooltipRow[], remainingMs: number, text: ( key: string ) => string ): void {
	const TIME_UNITS = [ "PARAM_DAY", "PARAM_HOUR", "PARAM_MINUTE", "PARAM_SECOND" ];
	const seconds = Math.floor( remainingMs / MILLISECONDS_PER_SECOND );
	if ( seconds <= 0 ) return;
	const values = [
		Math.floor( seconds / SECONDS_PER_DAY ),
		Math.floor( seconds / SECONDS_PER_HOUR ) % HOURS_PER_DAY,
		Math.floor( seconds / SECONDS_PER_MINUTE ) % SECONDS_PER_MINUTE,
		seconds % SECONDS_PER_MINUTE
	];
	const first = values.findIndex( value => value > 0 );
	rows.push( {
		value: "\n" + text( "UIIT_STT_REMAIN_TIME" ) + " " +
			values.slice( first ).map( ( value, index ) => value + text( TIME_UNITS[first + index]! ) ).join( " " ),
		color: TOOLTIP_WHITE
	} );
}

/*
================
buffTooltip

77C110 decodes power and grade into distinct fields. 6E488C uses only grade
in the local heading; 6E4A75 appends power under PARAM_POWER. Other viewers
read record+2 on their own character (6DF8C1), never the local snapshot.
================
*/
export function buffTooltip(
	source: UiHelpSource,
	game: GameplayState,
	timeMs: number,
	catalog: TooltipSkillCatalog,
	text: ( key: string ) => string
): readonly TooltipRow[] {
	if ( source.kind === "abnormal" ) {
		const vital = game.vitals.find( value => value.gid === source.gid );
		const mask = (2 ** source.bit) >>> 0;
		if ( !((vital?.abnormal ?? 0) & mask) ) return [];
		if ( source.unlevelled ) return abnormalTooltip( source.bit, 0, text );

		const local = source.gid === game.localGid && !source.viewer;
		const record = local ? game.abnormalRecords?.find( item => item.bit === source.bit ) : undefined;
		const grade = record?.grade ?? vital?.abnormalLevels?.find( row => row.bit === mask )?.level ?? 0;
		const rows = [ ...abnormalTooltip( source.bit, grade, text ) ];
		if ( !local || rows.length === 0 ) return rows;
		if ( grade ) {
			rows[0] = {
				value: text( "PARAM_" + statusCodes()[source.bit] ) + " " + grade + " " + text( "UIIT_STT_GRADE" ),
				color: TOOLTIP_WHITE
			};
		}
		if ( !record ) return rows;
		if ( record.level > 0 ) {
			rows.push( { value: "\n" + text( "PARAM_POWER" ) + " " + record.level, color: TOOLTIP_WHITE } );
		}
		if ( source.bit !== INFINITE_ABNORMAL_BIT ) {
			const elapsed = record.elapsedMs + Math.max( 0, timeMs - record.receivedAtMs );
			appendRemainingTime( rows, record.durationMs - elapsed, text );
		}
		return rows;
	}

	// The 0xB419/spawn status byte is only a flag (776450 / 85FB20);
	// every attached effect is a displayable decoration.
	const effect = game.attachedEffects?.find( value =>
		value.gid === source.gid && value.token === source.token && value.skill === source.skill
	);
	const row = catalog.get( source.skill );
	if ( !effect || !row ) return [];
	// 4FC730 returns one on equality. Soldier and pet exceptions use only a name.
	const nameOnlySkills = [
		"SN_SKILL_ASSAULTING_SOILDER",
		"SN_SKILL_ELITE_ASSAULTING_SOILDER",
		"SN_SKILL_CENTURION",
		"SN_SKILL_ASSAULTING_LEADER",
		"SN_SKILL_ELITE_IMPERIAL_GUARD",
		"SN_SKILL_COMBAT_COMMANDER",
		"SN_SKILL_MALL_PET_SKILL_COLD",
		"SN_SKILL_MALL_PET_SKILL_FIRE",
		"SN_SKILL_MALL_PET_SKILL_LIGHTNING",
		"SN_SKILL_MALL_PET_WATCH_CHULHYEN",
		"SN_SKILL_MALL_PET_WATCH_AGOL",
		"SN_SKILL_MALL_PET_GROWTH_POTION"
	];

	const rows: TooltipRow[] = [ {
		value: text( row.nameSymbol ) + (nameOnlySkills.includes( row.nameSymbol ) ? "" : " Lv " + row.basicLevel) +
			"\n ",
		color: TOOLTIP_WHITE
	} ];
	const description = text( row.tooltipDescriptionSymbol );
	if ( description ) rows.push( { value: description + "\n ", color: TOOLTIP_WHITE } );
	for ( const value of skillParameterFormatters( text ).direct( row ) ) rows.push( { value, color: TOOLTIP_WHITE } );
	const remaining = source.viewer ? null : effectRemainingMs( effect, timeMs );
	if ( remaining !== null ) appendRemainingTime( rows, remaining, text );
	// 6DE6F0 appends the subject name for a named detection entry.
	if ( source.viewer && effect.subject?.name ) {
		rows.push( { value: text( "UIIT_STT_TARGETTING" ) + " : " + effect.subject.name, color: TOOLTIP_WHITE } );
	}
	return rows;
}
