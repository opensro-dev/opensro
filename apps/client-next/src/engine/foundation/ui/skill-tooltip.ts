/*
===========================================================================

skill-tooltip.ts - shared skill-board and hotbar tooltip composition

The bound record owns use and effect rows. Acquisition requirements belong
to an unlearned first rank; otherwise the group's successor owns upgrade
requirements. Chain children contribute effects, never training ranks.

===========================================================================
*/
import type { SkillTooltipRowView, TooltipSkillCatalog } from "./skill-tooltip-data";
import { skillParameterFormatters } from "./skill-tooltip-params";
import { tooltipFormat, type TooltipRow } from "./tooltip-rows";
import type { Progression } from "../gameplay/progression";
const WHITE = 0xffffffff, GOLD = 0xffefdaa4, RED = 0xffff4a4a;
const FIRST_SKILL_RANK = 1;
const FIRST_WEAPON_KIND = 2;
const ATTACK_DAMAGE_MASK = 12;
const PHYSICAL_ATTACK_MASK = 4;
const MULTI_COUNT_PARAMETER = 0x5c;
const BASE_PERCENT = 100;

/*
================
SkillTooltipInput

One published catalog and progression snapshot serve every tooltip surface.
================
*/
export interface SkillTooltipInput {
	readonly id: number;
	readonly catalog: TooltipSkillCatalog;
	readonly learned: readonly number[];
	readonly progression: Progression;
}

/*
================
skillTooltip

55FE60 / 806EE0: preserve native row order and select exactly one set of
training requirements after composing the bound skill's effects.
================
*/
export function skillTooltip(
	input: SkillTooltipInput,
	text: ( symbol: string ) => string,
	masteryName: ( id: number ) => string
): readonly TooltipRow[] {
	const { id, catalog, learned, progression } = input;
	const row = catalog.get( id );
	if ( !row ) return [];
	const white = WHITE, gold = GOLD, red = RED, rows: TooltipRow[] = [];
	/*
 ================
 add

 Omit absent localization rows while preserving native heading ornaments.
 ================
 */
	const add = ( value: string, color = gold, heading = false ) => {
		if ( value.trim() ) {
			rows.push( { value, color, heading, ...(heading ? { ornament: "diamond" as const } : {}) } );
		}
	};
	const levels = new Map<number, number>();
	for ( const id of learned ) {
		const r = catalog.get( id );
		if ( r ) levels.set( r.groupId, Math.max( levels.get( r.groupId ) ?? 0, r.basicLevel ) );
	}
	/*
 ================
 groupRecord

 Resolve training ranks by relation group rather than effect-chain links.
 ================
 */
	const groupRecord = ( group: number, level: number ) => catalog.groups.get( group + ":" + level );
	/*
 ================
 conditions

 Evaluate the chosen rank against the same live progression snapshot.
 ================
 */
	const conditions = ( r: SkillTooltipRowView, heading: string ) => {
		const requirements: TooltipRow[] = [];
		/*
  ================
  requirement

  Mark only unmet requirements red; affordability never selects another rank.
  ================
  */
		const requirement = ( value: string, met: boolean ) => {
			if ( value.trim() ) requirements.push( { value, color: met ? gold : red } );
		};
		if ( r.masteryId ) {
			requirement(
				`${text( "PARAM_MASTERY_LEVEL" )} : ${masteryName( r.masteryId )} Lv ${r.reqMasteryLevel}`,
				(progression.masteries.find( m => m.id === r.masteryId )?.level ?? 0) >= r.reqMasteryLevel
			);
		}
		if ( r.reqStr ) {
			requirement( `${text( "PARAM_STR" )} : ${r.reqStr}`, (progression.stats?.strength ?? 0) >= r.reqStr );
		}
		if ( r.reqInt ) {
			requirement( `${text( "PARAM_INT" )} : ${r.reqInt}`, (progression.stats?.intellect ?? 0) >= r.reqInt );
		}
		let index = 0;
		for ( const req of r.reqGroups ) {
			if ( !req.groupId || req.groupId === r.groupId ) continue;
			const prior = groupRecord( req.groupId, req.level );
			if ( !prior ) throw Error( "Missing tooltip prerequisite" );
			requirement(
				`${text( "PARAM_REQ_PREV_SKILL" )}${++index} : ${text( prior.nameSymbol )} Lv ${req.level}`,
				(levels.get( req.groupId ) ?? 0) >= req.level
			);
		}
		if ( r.reqLearnSp ) {
			requirement(
				`${text( "PARAM_REQ_SP" )} : ${r.reqLearnSp}`,
				(progression.skillPoints ?? 0) >= r.reqLearnSp
			);
		}
		if ( requirements.length ) {
			add( text( heading ), white, true );
			rows.push( ...requirements );
		}
	};
	add( `${text( row.nameSymbol )} Lv ${row.basicLevel}\n `, white );
	add( text( row.basicActivity === 0 ? "PARAM_PASSIVE_SKILL" : "PARAM_ACTIVE_SKILL" ) );
	for (
		const [symbol, value] of [ [ "PARAM_REQ_HP", row.requiredHp ], [ "PARAM_REQ_MP", row.requiredMp ], [
			"PARAM_REQ_HP_RATIO",
			row.requiredHpRatio
		], [ "PARAM_REQ_MP_RATIO", row.requiredMpRatio ] ] as const
	) if ( value > 0 ) add( tooltipFormat( text( symbol ), value ) );
	const weapons = [
		"PARAM_WEAPON_SWORD",
		"PARAM_WEAPON_BLADE",
		"PARAM_WEAPON_SPEAR",
		"PARAM_WEAPON_TBLADE",
		"PARAM_WEAPON_BOW",
		...[
			"ONEHANDSWORD",
			"TWOHANDSWORD",
			"DUELAXE",
			"DARKSTAFF",
			"TWOHANDSTAFF",
			"CROSSBOW",
			"DAGGER",
			"HARP",
			"ONEHANDSTAFF"
		].map( s => "UIO_NEWCHAR_STT_EU_" + s )
	];
	const names = row.requiredWeaponKinds.flatMap( kind =>
		weapons[kind - FIRST_WEAPON_KIND] ? [ text( weapons[kind - FIRST_WEAPON_KIND]! ) ] : []
	);
	if ( names.length ) add( `${text( "PARAM_REQ_WEAPON_TYPE" )}: ${names.join( ", " )}` );
	add( text( row.tooltipDescriptionSymbol ), white );
	const format = skillParameterFormatters( text );
	if ( !row.chainNextSkillId ) { for ( const value of format.direct( row ) ) add( value ); }
	else {
		const chain: SkillTooltipRowView[] = [], seen = new Set<number>();
		let cursor: SkillTooltipRowView | undefined = row;
		while ( cursor ) {
			if ( seen.has( cursor.id ) ) throw Error( "Cyclic tooltip skill chain" );
			seen.add( cursor.id );
			chain.push( cursor );
			cursor = cursor.chainNextSkillId ? catalog.get( cursor.chainNextSkillId ) : undefined;
		}
		const attacks = chain.filter( r => r.attack.present ), last = chain.at( -1 )!;
		if ( attacks.length ) {
			const flags = attacks.at( -1 )!.attack.flags;
			if ( flags & ATTACK_DAMAGE_MASK ) {
				/*
================
average

Native chain attacks display their truncated arithmetic mean.
================
				*/
				const average = ( key: "minimum" | "maximum" | "percent" ) =>
					Math.trunc( attacks.reduce( ( sum, r ) => sum + r.attack[key], 0 ) / attacks.length );
				add(
					`${text( flags & PHYSICAL_ATTACK_MASK ? "PARAM_PA" : "PARAM_MA" )} ${average( "minimum" )}~${
						average( "maximum" )
					} (${average( "percent" )}%)`
				);
			}
		}
		for ( const value of format.aggregate( chain ) ) add( value );
		const count = chain.reduce(
			( sum, r ) =>
				sum + (format.block( row, MULTI_COUNT_PARAMETER ) ?
					format.block( r, MULTI_COUNT_PARAMETER )?.values[1] ?? 0 :
					1),
			0
		);
		if ( count ) {
			add( `${
				chain.some( r => r.directTooltipParams.knockout || r.directTooltipParams.knockback ) ?
					text( "PARAM_MAX" ) + " " :
					""
			}${text( "PARAM_MC_COUNT" )} ${count}${text( "UIIT_STT_COUNT" )}` );
		}
		const area = format.area( last, true );
		if ( area ) add( area );
		const p = last.directTooltipParams;
		if ( p.downAttackRatio !== null && p.downAttackRatio !== BASE_PERCENT ) {
			add( `${text( "PARAM_DA" )} ${p.downAttackRatio - BASE_PERCENT}% ${text( "PARAM_INCREASE" )}` );
		}
		if ( p.criticalFlat ) add( `${text( "PARAM_CRITICAL" )} ${p.criticalFlat} ${text( "PARAM_INCREASE" )}` );
		if ( p.criticalRatio ) add( `${text( "PARAM_CRITICAL" )} ${p.criticalRatio}% ${text( "PARAM_INCREASE" )}` );
		if ( p.tauntFlat || p.tauntRatio ) {
			const base = row.directTooltipParams;
			if ( base.tauntFlat ) add( `${text( "UIIT_STT_EU_TAUNT" )} ${base.tauntFlat}` );
			if ( base.tauntRatio ) add( `${text( "PARAM_AGGRO" )} ${base.tauntRatio}% ${text( "PARAM_INCREASE" )}` );
		}
	}
	// 5602CF tests Basic_Level, not Basic_Activity. The learned lookup also
	// accepts a higher rank of the same group (850057..85006D).
	if ( row.basicLevel === FIRST_SKILL_RANK && (levels.get( row.groupId ) ?? 0) < row.basicLevel ) {
		conditions( row, "PARAM_CONDITION_OF_LEARN" );
		// 56039E exits after this list: a first acquisition must not advertise
		// the successor's mastery or SP cost beside the current rank's name.
		return rows;
	}
	const next = groupRecord( row.groupId, row.basicLevel + 1 );
	if ( next ) conditions( next, "PARAM_CONDITION_OF_NEXT_LEVEL" );
	return rows;
}
