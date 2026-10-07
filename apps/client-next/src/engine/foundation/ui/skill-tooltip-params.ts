/*
===========================================================================

skill-tooltip-params.ts - the skill tooltip's native parameter rows

Ports the v1.150 client's CSkillParameters tooltip formatters: each native
parameter block (by its RefSkill offset) and each projected set-value code
becomes the localized rows the original prints, in the original's order.

===========================================================================
*/
import type { SkillTooltipRowView } from "./skill-tooltip-data";
/*
================
skillParameterFormatters
================
*/
export function skillParameterFormatters( text: ( symbol: string ) => string ) {
	/*
================
format
================
	*/
	function format( template: string, ...values: Array<string | number> ): string {
		let index = 0;
		return template.replace( /%%|%[sd]/g, ( token ) => {
			if ( token === "%%" ) return "%";
			const value = values[index++];
			return value === undefined ? token : String( value );
		} );
	}

	/*
================
projectedAreaBlockRow
================
	*/
	function projectedAreaBlockRow(
		row: SkillTooltipRowView,
		area: NonNullable<SkillTooltipRowView["directTooltipParams"]["area"]>,
		includeZeroCount: boolean
	): string | null {
		if (
			area.gate === 3 &&
			(row.nameSymbol === "SN_SKILL_EU_WIZARD_FIREA_TRAP_A" ||
				row.nameSymbol === "SN_SKILL_EU_WIZARD_FIREA_TRAP_B")
		) {
			return null;
		}

		const radius = ((area.radiusDeci >>> 0) / 10).toFixed( 1 );
		const withCount = ( rangeSymbol: string, distanceSymbol: string, countSymbol: string ): string => {
			const prefix = `${text( rangeSymbol )} ${text( distanceSymbol )} ${radius}m`;
			return includeZeroCount || area.targetCount > 0 ?
				`${prefix} (${text( countSymbol )} ${area.targetCount})` :
				prefix;
		};

		switch ( area.kind ) {
			case 1:
				return withCount( "UIIT_STT_NEAR_RANGE", "UIIT_STT_RADIUS", "UIIT_STT_SAME_TIME_ATTACK" );
			case 2:
				return withCount( "UIIT_STT_FRONT_RANGE", "UIIT_STT_RADIUS", "UIIT_STT_SAME_TIME_ATTACK" );
			case 3:
			case 4: {
				const prefix = `${text( "UIIT_STT_PIERCING_RANGE" )} (${
					text( "UIIT_STT_PIERCING_NUM" )
				} ${area.targetCount})`;
				return (area.pierceDecrease >>> 0) > 0 ?
					`${prefix}, ${text( "UIIT_STT_DAMAGE" )} ${text( "PARAM_DECREASE" )} -${area.pierceDecrease}%` :
					prefix;
			}
			case 6:
				return withCount( "UIIT_STT_TRANSITION_RANGE", "UIIT_STT_DISTANCE", "UIIT_STT_TRANSITION_COUNT" );
			default:
				// Kind 5 is the native one-space spacer. The helper-bubble row host
				// already owns vertical separation, so it produces no semantic row.
				return null;
		}
	}

	/*
================
projectedAreaRow
================
	*/
	function projectedAreaRow(
		row: SkillTooltipRowView,
		includeZeroCount: boolean
	): string | null {
		const area = row.directTooltipParams.area;
		return area ? projectedAreaBlockRow( row, area, includeZeroCount ) : null;
	}

	/** Exact `setv` subtype frontier reachable from the shipped CH/EU board. */
	/*
================
CSkillData_IsProjectedSetValueCodeSupported
================
	*/
	function CSkillData_IsProjectedSetValueCodeSupported( code: number ): boolean {
		switch ( code >>> 0 ) {
			case 0x45315341:
			case 0x45325341:
			case 0x45324141:
			case 0x43424154:
			case 0x44474154:
			case 0x45414154:
			case 0x434f4154:
			case 0x46494154:
			case 0x4c494154:
			case 0x44544154:
			case 0x424c4154:
			case 0x4d554154:
			case 0x484c4154:
			case 0x44474141:
			case 0x45324148:
			case 0x44474852:
			case 0x57494d44:
			case 0x42444d44:
			case 0x484c4d44:
			case 0x57495255:
			case 0x43425241:
			case 0x52504455:
			case 0x52505455:
			case 0x52504255:
			case 0x53544455:
			case 0x53545350:
			case 0x42534850:
			case 0x53414141:
			case 0x54524141:
			case 0x4d554552:
			case 0x44534552:
			case 0x4d554352:
			case 0x44534352:
			case 0x44544452:
			case 0x484c5255:
			case 0x484c4653:
			case 0x484c4d49:
			case 0x484c4250:
			case 0x484c534d:
				return true;
			default:
				return false;
		}
	}

	/*
================
projectedSetValueRow
================
	*/
	function projectedSetValueRow( code: number, value: number ): string | null {
		const increase = text( "PARAM_INCREASE" );
		const decrease = text( "PARAM_DECREASE" );
		const amount = ( symbol: string ): string => `${text( symbol )} ${value} ${increase}`;
		const percent = ( symbol: string ): string => `${text( symbol )} ${value}% ${increase}`;
		const meters = ( symbol: string ): string =>
			`${text( symbol )} ${((value >>> 0) / 10).toFixed( 1 )}m ${increase}`;
		const blessing = ( symbol: string ): string =>
			`${text( "UIIT_STT_EU_WHEN_A_BLESSING" )} ${text( symbol )} ${value} ${increase}`;

		switch ( code >>> 0 ) {
			// Physical-attack ratio families.
			case 0x45315341: // E1SA
			case 0x45325341: // E2SA
			case 0x45324141: // E2AA
			case 0x43424154: // CBAT
			case 0x44474154: // DGAT
				return percent( "PARAM_PA" );

			// Magical-attack ratio families.
			case 0x45414154: // EAAT
			case 0x434f4154: // COAT
			case 0x46494154: // FIAT
			case 0x4c494154: // LIAT
			case 0x44544154: // DTAT
			case 0x424c4154: // BLAT
			case 0x4d554154: // MUAT
			case 0x484c4154: // HLAT
				return percent( "PARAM_MA" );

			case 0x44474141: // DGAA
				return amount( "PARAM_PA" );
			case 0x45324148: // E2AH
			case 0x44474852: // DGHR
				return amount( "PARAM_HR" );

			case 0x57494d44: // WIMD
			case 0x42444d44: // BDMD
			case 0x484c4d44: // HLMD
				return `${text( "PARAM_PSKILL_CONSUME_MP" )} ${value}% ${decrease}`;
			case 0x57495255: // WIRU
			case 0x43425241: // CBRA
				return meters( "PARAM_RU" );
			case 0x52504455: // RPDU
				return amount( "PARAM_POISON_DAMAGE" );
			case 0x52505455: // RPTU
				return `${text( "PARAM_PS" )} ${text( "PARAM_POWER" )} ${value} ${increase}`;
			case 0x52504255: // RPBU
			case 0x53544455: // STDU
				return `${text( "PARAM_DURA" )} ${((value >>> 0) / 1_000).toFixed( 1 )}${
					text( "PARAM_SECOND" )
				} ${increase}`;
			case 0x44544452: // DTDR
				return percent( "PARAM_DURA" );
			case 0x53545350: // STSP
				return percent( "PARAM_HSTE" );
			case 0x42534850: // BSHP
				return amount( "PARAM_LIFE_STEAL" );
			case 0x53414141: // SAAA
				return amount( "UIIT_STT_EU_CRUALSPELL_ATTACK" );
			case 0x54524141: // TRAA
				return amount( "UIIT_STT_EU_TRAP_DEMAGE" );
			case 0x4d554552: // MUER
			case 0x44534552: // DSER
				return meters( "PARAM_EFF_RANGE" );
			case 0x4d554352: // MUCR
			case 0x44534352: // DSCR
				return amount( "PARAM_RESIST" );
			case 0x484c5255: // HLRU
				return percent( "PARAM_HEAL_HP" );
			case 0x484c4653: // HLFS
				return blessing( "PARAM_STR" );
			case 0x484c4d49: // HLMI
				return blessing( "PARAM_INT" );
			case 0x484c4250: // HLBP
				return blessing( "PARAM_PD" );
			case 0x484c534d: // HLSM
				return blessing( "PARAM_MD" );
			default:
				return null;
		}
	}

	type NativeParamBlock = SkillTooltipRowView["directTooltipParams"]["nativeParamBlocks"][number];

	/*
================
nativeParamBlock
================
	*/
	function nativeParamBlock( row: SkillTooltipRowView, offset: number ): NativeParamBlock | null {
		let result: NativeParamBlock | null = null;
		for ( const block of row.directTooltipParams.nativeParamBlocks ) {
			if ( block.offset === offset ) result = block;
		}
		return result;
	}

	const PROJECTED_NATIVE_PARAM_OFFSETS = new Set<number>( [
		0x004,
		0x008,
		0x010,
		0x014,
		0x018,
		0x01c,
		0x024,
		0x028,
		0x02c,
		0x034,
		0x038,
		0x03c,
		0x040,
		0x044,
		0x04c,
		0x050,
		0x054,
		0x058,
		0x05c,
		0x060,
		0x064,
		0x068,
		0x070,
		0x080,
		0x084,
		0x088,
		0x08c,
		0x090,
		0x094,
		0x098,
		0x09c,
		0x0ac,
		0x0b0,
		0x0b4,
		0x0b8,
		0x0bc,
		0x0c8,
		0x0cc,
		0x0d4,
		0x0d8,
		0x0dc,
		0x0e0,
		0x0e4,
		0x0e8,
		0x0ec,
		0x0f0,
		0x0f4,
		0x0f8,
		0x0fc,
		0x100,
		0x104,
		0x108,
		0x10c,
		0x110,
		0x114,
		0x118,
		0x13c,
		0x140,
		0x144,
		0x15c,
		0x160,
		0x164,
		0x168,
		0x16c,
		0x170,
		0x198,
		0x19c,
		0x1a0,
		0x1a4,
		0x1ac,
		0x1b0,
		0x1b8,
		0x1c4,
		0x1c8,
		0x1cc,
		0x1d0,
		0x1d8,
		0x1dc,
		0x1e4,
		0x1e8,
		0x1ec,
		0x1f0,
		0x1f8,
		0x1fc,
		0x200,
		0x204,
		0x208,
		0x20c,
		0x210,
		0x214,
		0x218,
		0x21c,
		0x220,
		0x224,
		0x228,
		0x22c,
		0x230,
		0x234,
		0x238,
		0x23c,
		0x240,
		0x244,
		0x268,
		0x26c,
		0x278,
		0x27c,
		0x280,
		0x284,
		0x288,
		0x28c,
		0x290,
		0x294,
		0x298,
		0x2a4
	] );

	/** Reachability ledger used by the CH/EU asset audit. */
	/*
================
CSkillData_IsProjectedNativeParamOffsetSupported
================
	*/
	function CSkillData_IsProjectedNativeParamOffsetSupported( offset: number ): boolean {
		return PROJECTED_NATIVE_PARAM_OFFSETS.has( offset >>> 0 );
	}

	/*
================
positiveAmountRows
================
	*/
	function positiveAmountRows( values: number[], symbol: string ): string[] {
		const rows: string[] = [];
		if ( (values[0] ?? 0) > 0 ) rows.push( `${text( symbol )} ${values[0]} ${text( "PARAM_INCREASE" )}` );
		if ( (values[1] ?? 0) > 0 ) rows.push( `${text( symbol )} ${values[1]}% ${text( "PARAM_INCREASE" )}` );
		return rows;
	}

	/*
================
damagePairRows
================
	*/
	function damagePairRows( values: number[], suffixSymbol: string, percent: boolean ): string[] {
		const rows: string[] = [];
		const suffix = text( suffixSymbol );
		if ( (values[0] ?? 0) > 0 ) {
			rows.push( `${text( "UIIT_STT_PHY_DAMAGE" )} ${values[0]}${percent ? "%" : ""} ${suffix}` );
		}
		if ( (values[1] ?? 0) > 0 ) {
			rows.push( `${text( "UIIT_STT_MAG_DAMAGE" )} ${values[1]}${percent ? "%" : ""} ${suffix}` );
		}
		return rows;
	}

	/*
================
projectedPowerStatusRow
================
	*/
	function projectedPowerStatusRow( values: number[], symbol: string, hasLevel: boolean ): string {
		const tail = hasLevel ? `, lv ${values[2] ?? 0}` : "";
		return `${text( symbol )}${text( "UIIT_STT_PROBABILITY" )} ${values[1] ?? 0}% (${text( "PARAM_POWER" )}${
			values[0] ?? 0
		}${tail})`;
	}

	/*
================
projectedTimedStatusRow
================
	*/
	function projectedTimedStatusRow( values: number[], symbol: string ): string {
		return `${text( symbol )} ${values[2] ?? 0}${text( "UIIT_STT_GRADE" )} (` +
			`${text( "UIIT_STT_PROBABILITY" )}${values[1] ?? 0}%, ${((values[0] ?? 0) / 1_000).toFixed( 1 )}${
				text( "PARAM_SECOND" )
			})`;
	}

	/*
================
projectedCurePowerRows
================
	*/
	function projectedCurePowerRows( values: number[], prefixSymbol: string ): string[] {
		const rows: string[] = [];
		const flags = values[0] ?? 0;
		const power = values[1] ?? 0;
		const families: Array<[number, string]> = [
			[ 0x04, "PARAM_ES" ],
			[ 0x08, "PARAM_BU" ],
			[ 0x10, "PARAM_PS" ],
			[ 0x01, "PARAM_FZ" ],
			[ 0x02, "PARAM_FB" ],
			[ 0x20, "PARAM_CURSING" ]
		];
		if ( (flags & 0x1c) === 0 ) families.unshift( [ 0, "PARAM_WEAKLY" ] );
		if ( (flags & 0x03) === 0 ) families.unshift( [ 0, "PARAM_RESTRICTION" ] );
		for ( const [mask, family] of families ) {
			if ( mask === 0 || (flags & mask) !== 0 ) {
				rows.push( `${text( family )}${text( prefixSymbol )} (${text( "PARAM_POWER" )} ${power})` );
			}
		}
		return rows;
	}

	/*
================
projectedCureGradeRows
================
	*/
	function projectedCureGradeRows( values: number[], prefixSymbol: string ): string[] {
		const rows: string[] = [];
		const flags = values[0] ?? 0;
		const grade = values[1] ?? 0;
		const families: Array<[number, string]> = [
			[ 0x000001, "PARAM_STONE" ],
			[ 0x000002, "PARAM_STUN" ],
			[ 0x000004, "PARAM_BLOOD" ],
			[ 0x000008, "PARAM_DN" ],
			[ 0x000010, "PARAM_CURSE_PD" ],
			[ 0x000020, "PARAM_CURSE_MD" ],
			[ 0x000040, "PARAM_CURSE_STR" ],
			[ 0x000080, "PARAM_CURSE_INT" ],
			[ 0x000100, "PARAM_WEAKLY" ],
			[ 0x000200, "PARAM_SLEEP" ],
			[ 0x000400, "PARAM_ROOT" ],
			[ 0x000800, "PARAM_SLOW" ],
			[ 0x001000, "PARAM_RESTRICTION" ],
			[ 0x002000, "PARAM_ZB" ],
			[ 0x004000, "PARAM_FEAR" ],
			[ 0x008000, "PARAM_MYOPIA" ],
			[ 0x010000, "PARAM_DISEASE" ],
			[ 0x020000, "PARAM_CHAOS" ],
			[ 0x040000, "PARAM_CURSE_HP" ],
			[ 0x080000, "PARAM_CURSIE_MP" ],
			[ 0x100000, "PARAM_CUNTROL" ],
			[ 0x200000, "PARAM_TIME_BOMB" ],
			[ 0x400000, "PARAM_CURSING" ]
		];
		for ( const [mask, family] of families ) {
			if ( (flags & mask) !== 0 ) {
				rows.push( `${text( family )}${text( prefixSymbol )} (${grade} ${text( "UIIT_STT_GRADE" )})` );
			}
		}
		return rows;
	}

	/*
================
projectedNativeRowsForBlock
================
	*/
	function projectedNativeRowsForBlock( row: SkillTooltipRowView, block: NativeParamBlock ): string[] {
		const v = block.values;
		const increase = text( "PARAM_INCREASE" );
		const decrease = text( "PARAM_DECREASE" );
		const amount = ( symbol: string, value: number ): string => `${text( symbol )} ${value} ${increase}`;
		const percent = ( symbol: string, value: number ): string => `${text( symbol )} ${value}% ${increase}`;

		switch ( block.offset ) {
			case 0x054: {
				const durationMs = v[0] ?? 0;
				if ( durationMs === 0 ) return [];
				if ( (durationMs >>> 0) < 10_000 ) {
					return [
						`${text( "PARAM_DURA" )} ${((durationMs >>> 0) / 1_000).toFixed( 1 )} ${text( "PARAM_SECOND" )}`
					];
				}
				if ( (durationMs >>> 0) > 86_400_000 ) {
					return [
						`${text( "PARAM_DURA" )} ${Math.floor( (durationMs >>> 0) / 86_400_000 )} ${
							text( "PARAM_DAY" )
						}`
					];
				}
				return [
					`${text( "PARAM_DURA" )} ${Math.floor( (durationMs >>> 0) / 1_000 )} ${text( "PARAM_SECOND" )}`
				];
			}
			case 0x144: {
				const rows: string[] = [];
				if ( (v[1] ?? 0) > 0 ) {
					rows.push( `${text( "UIIT_STT_EU_LINK_INTERVAL" )} ${Math.floor( (v[1] ?? 0) / 10 )}m` );
				}
				if ( (v[2] ?? 0) > 0 ) rows.push( `${text( "UIIT_STT_EU_LINK_AMOUNT" )} ${v[2]}` );
				return rows;
			}
			case 0x004: {
				if ( (v[2] ?? 0) === 0 || (v[3] ?? 0) === 0 || ((v[0] ?? 0) & 0x0c) === 0 ) return [];
				return [ `${text( (v[0] ?? 0) & 4 ? "PARAM_PA" : "PARAM_MA" )} ${v[2]}~${v[3]} (${v[1]}%)` ];
			}
			case 0x008:
				return [ `${text( "UIIT_STT_DAMAGE" )} ${text( "UIIT_STT_EU_ABSOLUTE" )} ${v[0] ?? 0}` ];
			case 0x010:
				return [ `${text( "PARAM_KO" )} ${v[1] ?? 0}%(lv ${v[0] ?? 0})` ];
			case 0x014:
				return (v[0] ?? 100) !== 100 ? [ `${text( "PARAM_DA" )} ${(v[0] ?? 100) - 100}% ${increase}` ] : [];
			case 0x018: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( amount( "PARAM_CRITICAL", v[0]! ) );
				if ( (v[1] ?? 0) !== 0 ) rows.push( percent( "PARAM_CRITICAL", v[1]! ) );
				return rows;
			}
			case 0x01c:
				return (v[0] ?? 0) !== 0 ? [ `${text( "PARAM_CK" )} ${v[0]}%` ] : [];
			case 0x028:
				return [ `${text( "PARAM_RU" )} ${((v[0] ?? 0) / 10).toFixed( 1 )}m ${increase}` ];
			case 0x02c:
				return [ `${text( "PARAM_KB" )} ${v[0] ?? 0}%` ];
			case 0x034: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( amount( "PARAM_PD", v[0]! ) );
				if ( (v[1] ?? 0) > 0 ) rows.push( amount( "PARAM_MD", v[1]! ) );
				if ( (v[2] ?? 0) > 0 ) rows.push( format( text( "UIIT_STT_EU_PSKILL_LIMIT_APPLY" ), v[2]! ) );
				return rows;
			}
			case 0x038: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( percent( "PARAM_PD", v[0]! ) );
				if ( (v[1] ?? 0) > 0 ) rows.push( percent( "PARAM_MD", v[1]! ) );
				return rows;
			}
			case 0x03c: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( amount( "PARAM_PR", v[0]! ) );
				if ( (v[1] ?? 0) > 0 ) rows.push( amount( "PARAM_MR", v[1]! ) );
				return rows;
			}
			case 0x040:
				return damagePairRows( v, "PARAM_DECREASE_RATIO", false );
			case 0x044:
				return damagePairRows( v, "PARAM_ABSORB", true );
			case 0x1a4:
				return damagePairRows( v, "PARAM_INCREASE", true );
			case 0x04c:
				return (v[1] ?? 0) !== 0 ? [ percent( "PARAM_BLOCKING", v[1]! ) ] : [];
			case 0x024:
				return positiveAmountRows( v, "PARAM_HR" );
			case 0x050:
				return positiveAmountRows( v, "PARAM_ER" );
			case 0x058:
				return (v[1] ?? 0) !== 0 ?
					[ format( text( "PARAM_ONFF_CONSUME_MP" ), Math.floor( (v[0] ?? 0) / 1_000 ), v[1]! ) ] :
					[];
			case 0x05c:
				return v[0] === 2 && (v[1] ?? 0) >= 2 ?
					[ `${text( "PARAM_MC_COUNT" )} ${v[1]}${text( "UIIT_STT_COUNT" )}` ] :
					[];
			case 0x060:
			case 0x064:
			case 0x068: {
				const area = {
					gate: v[0] ?? 0,
					kind: v[1] ?? 0,
					radiusDeci: v[2] ?? 0,
					targetCount: v[3] ?? 0,
					pierceDecrease: v[4] ?? 0,
					reserved: v[5] ?? 0
				};
				const value = projectedAreaBlockRow( row, area, false );
				return value ? [ value ] : [];
			}
			case 0x088: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) !== 0 ) rows.push( `${text( "PARAM_MAX" )}HP ${v[0]} ${increase}` );
				if ( (v[1] ?? 0) !== 0 ) rows.push( `${text( "PARAM_MAX" )}HP ${v[1]}% ${increase}` );
				return rows;
			}
			case 0x08c: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) !== 0 ) rows.push( `${text( "PARAM_MAX" )}MP ${v[0]} ${increase}` );
				if ( (v[1] ?? 0) !== 0 ) rows.push( `${text( "PARAM_MAX" )}MP ${v[1]}% ${increase}` );
				return rows;
			}
			case 0x080: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) !== 0 ) {
					rows.push(
						`${text( "PARAM_HP" )} ${text( "UIIT_STT_EU_HEAL" )} ${text( "UIIT_STT_EU_RECEIVE_AMOUNT" )} ${
							v[0]
						}% ${increase}`
					);
				}
				if ( (v[1] ?? 0) !== 0 ) rows.push( `${text( "PARAM_PSKILL_CONSUME_MP" )} ${v[1]}% ${increase}` );
				return rows;
			}
			case 0x084:
				return (v[0] ?? 0) !== 0 ? [ `${text( "PARAM_PSKILL_CONSUME_MP" )} ${v[0]}% ${decrease}` ] : [];
			case 0x090: {
				const rows: string[] = [];
				const flags = v[0] ?? 0;
				if ( (v[1] ?? 0) > 0 ) rows.push( `HP ${v[1]}` );
				if ( (v[2] ?? 0) > 0 ) {
					if ( flags & 4 ) rows.push( `${text( "PARAM_PD" )} ${v[2]}` );
					if ( flags & 8 ) rows.push( `${text( "PARAM_MD" )} ${v[2]}` );
				}
				if ( (v[3] ?? 0) > 0 ) {
					if ( flags & 4 ) rows.push( `${text( "PARAM_PR" )} ${v[3]}` );
					if ( flags & 8 ) rows.push( `${text( "PARAM_MR" )} ${v[3]}` );
				}
				return rows;
			}
			case 0x094:
			case 0x098:
			case 0x09c:
				return (v[0] ?? 0) > 0 ? [ percent( "PARAM_HSTE", v[0]! ) ] : [];
			case 0x0ac: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) !== 0 ) rows.push( `HP ${text( "PARAM_IRGC" )} ${v[0]}% ${increase}` );
				if ( (v[1] ?? 0) !== 0 ) rows.push( `MP ${text( "PARAM_IRGC" )} ${v[1]}% ${increase}` );
				return rows;
			}
			case 0x0b0: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( percent( "PARAM_PA", v[0]! ) );
				if ( (v[1] ?? 0) > 0 ) rows.push( percent( "PARAM_MA", v[1]! ) );
				return rows;
			}
			case 0x0b4: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( amount( "PARAM_PA", v[0]! ) );
				if ( (v[1] ?? 0) > 0 ) rows.push( amount( "PARAM_MA", v[1]! ) );
				return rows;
			}
			case 0x1b0: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( `${text( "PARAM_SPD" )} ${v[0]}% ${decrease}` );
				if ( (v[1] ?? 0) > 0 ) rows.push( amount( "PARAM_PA", v[1]! ) );
				return rows;
			}
			case 0x0b8:
				return (v[0] ?? 0) > 0 ? [ `${text( "PARAM_ER" )} ${v[0]} ${decrease}` ] : [];
			case 0x0bc:
				return (v[0] ?? 0) > 0 ? [ `${text( "PARAM_HR" )} ${v[0]} ${decrease}` ] : [];
			// Client 7FC6CE..7FC731: the tele row is gated on the first argument and
			// prints the SECOND (fild [edi+4]) as "%s %.1fm": the travel distance
			// the server also uses (5862E0). Ghost Walk Shadow 4 reads 23.0m, not 50.0m.
			case 0x0c8:
				return (v[0] ?? 0) > 0 ? [ `${text( "PARAM_TELE" )} ${((v[1] ?? 0) / 10).toFixed( 1 )}m` ] : [];
			case 0x0cc:
				return (v[1] ?? 0) !== 0 ? [ `${text( "PARAM_TELE" )} ${(v[1]! / 10).toFixed( 1 )}m` ] : [];
			case 0x0e0:
				return (v[1] ?? 0) > 0 ? [ `${text( "PARAM_POLA" )}(lv ${v[1]})` ] : [];
			case 0x0e4: {
				const rows: string[] = [];
				if ( !row.directTooltipParams.durationMs ) {
					rows.push(
						`${text( "PARAM_DURA" )} ${Math.floor( (v[0] ?? 0) / 1_000 )}${text( "PARAM_SECOND" )}`
					);
				}
				if ( (v[3] ?? 0) !== 0 ) rows.push( `${text( "PARAM_PA" )} ${v[3]}` );
				if ( (v[4] ?? 0) !== 0 ) rows.push( `${text( "PARAM_MA" )} ${v[4]}` );
				return rows;
			}
			case 0x0e8:
				return [ projectedPowerStatusRow( v, "PARAM_FZ", false ) ];
			case 0x0ec:
				return [ projectedPowerStatusRow( v, "PARAM_FB", false ) ];
			case 0x0f0:
				return [ projectedPowerStatusRow( v, "PARAM_ES", false ) ];
			case 0x0f4:
				return [ projectedPowerStatusRow( v, "PARAM_BU", true ) ];
			case 0x0f8:
				return [ projectedPowerStatusRow( v, "PARAM_PS", false ) ];
			case 0x0fc:
				return [ projectedPowerStatusRow( v, "PARAM_ZB", false ) ];
			case 0x1f8:
				return [ projectedTimedStatusRow( v, "PARAM_SLEEP" ) ];
			case 0x1fc:
				return [ projectedTimedStatusRow( v, "PARAM_ROOT" ) ];
			case 0x200:
				return [ projectedTimedStatusRow( v, "PARAM_SLOW" ) ];
			case 0x204:
				return [ projectedTimedStatusRow( v, "PARAM_FEAR" ) ];
			case 0x208:
				return [ projectedTimedStatusRow( v, "PARAM_MYOPIA" ) ];
			case 0x20c:
				return row.directTooltipParams.multiCount !== null && row.directTooltipParams.multiCount >= 2 ?
					[
						`${text( "PARAM_BLOOD" )} ${v[2] ?? 0}${text( "UIIT_STT_GRADE" )} (` +
						`${text( "UIIT_STT_PROBABILITY" )}${v[1] ?? 0}% ${row.directTooltipParams.multiCount}${
							text( "UIIT_STT_COUNT" )
						}, ` +
						`${((v[0] ?? 0) / 1_000).toFixed( 1 )}${text( "PARAM_SECOND" )})`
					] :
					[ projectedTimedStatusRow( v, "PARAM_BLOOD" ) ];
			case 0x210:
				return [ projectedTimedStatusRow( v, "PARAM_DN" ) ];
			case 0x214:
				return [ projectedTimedStatusRow( v, "PARAM_STUN" ) ];
			case 0x218:
				return [ projectedTimedStatusRow( v, "PARAM_DISEASE" ) ];
			case 0x21c:
				return [ projectedTimedStatusRow( v, "PARAM_CHAOS" ) ];
			case 0x220:
				return [ projectedTimedStatusRow( v, "PARAM_CURSE_STR" ) ];
			case 0x224:
				return [ projectedTimedStatusRow( v, "PARAM_CURSE_INT" ) ];
			case 0x228:
				return [ projectedTimedStatusRow( v, "PARAM_CURSE_PD" ) ];
			case 0x22c:
				return [ projectedTimedStatusRow( v, "PARAM_CURSE_MD" ) ];
			case 0x230:
				return [ projectedTimedStatusRow( v, "PARAM_CURSE_HP" ) ];
			case 0x234:
				return [ projectedTimedStatusRow( v, "PARAM_CURSIE_MP" ) ];
			case 0x238:
				return [
					`${text( "PARAM_TIME_BOMB" )} ${v[2] ?? 0}${text( "UIIT_STT_GRADE" )} (${
						text( "UIIT_STT_PROBABILITY" )
					}${v[1] ?? 0}%)`
				];
			case 0x23c:
				return damagePairRows( v, "PARAM_ABSORB", true );
			case 0x240:
				return (v[0] ?? 0) !== 0 ? [ `${text( "PARAM_AGGRO" )} ${v[0]}% ${text( "PARAM_ABSORB" )}` ] : [];
			case 0x244:
				return (v[0] ?? 0) !== 0 ?
					[ `${text( "UIIT_STT_DAMAGE" )} ${text( "UIIT_STT_EU_DISPERSION_RATIO" )} ${v[0]}%` ] :
					[];
			case 0x280:
			case 0x288:
				return (v[0] ?? 0) !== 0 ?
					[ `${text( "PARAM_WEAPON_PA_RATIO" )} ${v[0]}% ${text( "PARAM_REFLECT" )}` ] :
					[];
			case 0x284:
			case 0x28c:
			case 0x290:
			case 0x294:
			case 0x298:
				return (v[0] ?? 0) !== 0 ?
					[ `${text( "PARAM_WEAPON_MA_RATIO" )} ${v[0]}% ${text( "PARAM_REFLECT" )}` ] :
					[];
			case 0x10c:
			case 0x110: {
				const symbol = block.offset === 0x10c ? "PARAM_HP" : "PARAM_MP";
				const rows: string[] = [];
				if ( (v[1] ?? 0) > 0 ) rows.push( `${text( "PARAM_MAX" )}${text( symbol )} ${v[1]} ${decrease}` );
				if ( (v[2] ?? 0) > 0 ) rows.push( `${text( "PARAM_MAX" )}${text( symbol )} ${v[2]}% ${decrease}` );
				return rows;
			}
			case 0x114: {
				const rows: string[] = [];
				if ( (v[1] ?? 0) > 0 ) rows.push( `${text( "PARAM_MAX" )}${text( "PARAM_PD" )} ${v[1]}% ${decrease}` );
				if ( (v[2] ?? 0) > 0 ) rows.push( `${text( "PARAM_MAX" )}${text( "PARAM_MD" )} ${v[2]}% ${decrease}` );
				return rows;
			}
			case 0x118:
				return damagePairRows( [ v[1] ?? 0, v[2] ?? 0 ], "PARAM_DECREASE", true );
			case 0x1dc:
				return (v[0] ?? 0) !== 0 ?
					[ `${text( "PARAM_MP" )} ${v[0]}% ${text( "UIIT_STT_DAMAGE" )} ${text( "PARAM_ABSORB" )}` ] :
					[];
			case 0x13c:
				return [
					`${text( "PARAM_HP" )} ${text( "UIIT_STT_EU_END_BUFF" )} ${text( "PARAM_TO_DECREASE_RATIO" )}`
				];
			case 0x140:
				return [
					`${text( "PARAM_MP" )} ${text( "UIIT_STT_EU_END_BUFF" )} ${text( "PARAM_TO_DECREASE_RATIO" )}`
				];
			case 0x100: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( `HP ${v[0]} ${text( "PARAM_RECOVERY" )}` );
				if ( (v[1] ?? 0) > 0 ) rows.push( `HP ${v[1]}% ${text( "PARAM_RECOVERY" )}` );
				if ( (v[2] ?? 0) > 0 ) rows.push( `MP ${v[2]} ${text( "PARAM_RECOVERY" )}` );
				if ( (v[3] ?? 0) > 0 ) rows.push( `MP ${v[3]}% ${text( "PARAM_RECOVERY" )}` );
				return rows;
			}
			case 0x104: {
				if ( (v[1] ?? 0) === 0 ) {
					return (v[0] ?? 0) > 0 ?
						[ `${text( "PARAM_RESU" )}(lv ${v[0]})` ] :
						[ text( "PARAM_RESU" ) ];
				}
				return [
					`${text( "PARAM_RESU" )}(lv ${v[0] ?? 0}, ${text( "PARAM_LOSE_EXP" )} ${v[1]}% ${
						text( "PARAM_RESTORE" )
					})`
				];
			}
			case 0x108:
				return (v[0] ?? 0) > 0 ? [ `${text( "UIIT_STT_CURE_ALL" )} ${v[0]}` ] : [];
			case 0x1ac:
			case 0x1b8: {
				const symbol = block.offset === 0x1ac ? "PARAM_STR" : "PARAM_INT";
				const rows: string[] = [];
				if ( (v[0] ?? 0) !== 0 ) rows.push( amount( symbol, v[0]! ) );
				if ( (v[1] ?? 0) !== 0 ) rows.push( format( text( "UIIT_STT_EU_PSKILL_LIMIT_APPLY" ), v[1]! ) );
				return rows;
			}
			case 0x1c4:
				return [ `${text( "PARAM_HP" )} ${text( "PARAM_ABSORB" )} ${v[0] ?? 0}` ];
			case 0x1c8: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( `${text( "PARAM_HP" )} ${text( "PARAM_TRANS" )} ${v[0]}` );
				if ( (v[1] ?? 0) > 0 ) rows.push( `${text( "PARAM_MP" )} ${text( "PARAM_TRANS" )} ${v[1]}` );
				return rows;
			}
			case 0x1cc:
				return projectedCurePowerRows( v, "PARAM_CURE" );
			case 0x1d0:
				return projectedCureGradeRows( v, "PARAM_CURE" );
			case 0x0d4:
			case 0x0d8:
				return projectedCurePowerRows( v, "PARAM_DECREASE" );
			case 0x0dc:
				return projectedCureGradeRows( v, "PARAM_DECREASE" );
			case 0x1d8: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) {
					rows.push(
						`${text( "PARAM_RETURN" )} ${text( "UIIT_STT_DAMAGE" )} ${text( "UIIT_STT_EU_RATIO" )} ${v[0]}%`
					);
				}
				if ( (v[3] ?? 0) > 0 ) {
					rows.push(
						`${text( "PARAM_RETURN" )} ${text( "UIIT_STT_DAMAGE" )} ${text( "UIIT_STT_RADIUS" )} ${
							(v[3]! / 10).toFixed( 1 )
						}m`
					);
				}
				return rows;
			}
			case 0x1e4:
				return [ `${text( "UIIT_STT_EU_ABNORMAL_VALID_RANGE" )} ${Math.floor( (v[0] ?? 0) / 10 )}m` ];
			case 0x1e8:
			case 0x1ec: {
				const kind = (v[0] ?? 0) === 1 ? "PARAM_STEALTH" : "PARAM_INVISIBLE";
				return [ `${text( "PARAM_DETECT" )} ${text( kind )} ${v[1] ?? 0} ${text( "UIIT_STT_GRADE" )}` ];
			}
			case 0x1f0: {
				const kind = (v[0] ?? 0) === 1 ? "PARAM_STEALTH" : "PARAM_INVISIBLE";
				const rows = [ `${text( kind )} ${v[1] ?? 0} ${text( "UIIT_STT_GRADE" )}` ];
				if ( (v[2] ?? 0) !== 0 ) rows.push( `${text( "PARAM_ER" )} ${v[2]} ${increase}` );
				return rows;
			}
			case 0x198: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( `${text( "UIIT_STT_EU_TAUNT" )} ${v[0]}` );
				if ( (v[1] ?? 0) > 0 ) rows.push( `${text( "PARAM_AGGRO" )} ${v[1]}% ${increase}` );
				return rows;
			}
			case 0x19c: {
				const rows: string[] = [];
				if ( (v[0] ?? 0) > 0 ) rows.push( `${text( "PARAM_AGGRO" )} ${v[0]} ${decrease}` );
				if ( (v[1] ?? 0) > 0 ) rows.push( `${text( "PARAM_AGGRO" )} ${v[1]}% ${decrease}` );
				return rows;
			}
			case 0x1a0:
				return damagePairRows( v, "PARAM_ABSORB", true );
			case 0x070:
				return [ text( "UIIT_TOOLTIP_CURSING_STONE_01" ) ];
			case 0x268:
				return (v[1] ?? 0) !== 0 ? [ `${text( "UIIT_STT_MONSTER" )} ${text( "PARAM_MAX" )} ${v[1]}` ] : [];
			case 0x26c:
				return (v[0] ?? 0) !== 0 ?
					[ `${text( "UIIT_STT_MONSTER" )} ${text( "PARAM_LV" )} ${text( "PARAM_MAX" )} ${v[0]}` ] :
					[];
			// sub_7f9bd0 @0x7ff386..0x7ff48b: both one-dword boosts use
			// "%s %d%% %s" with PARAM_INCREASE.
			case 0x278:
				return (v[0] ?? 0) !== 0 ? [ `${text( "PARAM_ALCU" )} ${v[0]}% ${increase}` ] : [];
			case 0x27c:
				return (v[0] ?? 0) !== 0 ? [ `${text( "PARAM_LUCK" )} ${v[0]}% ${increase}` ] : [];
			case 0x2a4: {
				const rows: string[] = [];
				if ( (v[1] ?? 0) > 0 ) {
					rows.push(
						`${text( "PARAM_HP" )} ${v[1]}% ${text( "PARAM_ABSORB" )} (${text( "UIIT_STT_DAMAGE" )}${
							v[2] ?? 0
						})`
					);
				}
				if ( (v[0] ?? 0) > 0 ) rows.push( `${text( "PARAM_MP" )} ${v[0]}% ${text( "PARAM_ABSORB" )}` );
				return rows;
			}
			default:
				return [];
		}
	}

	/**
	 * The projection-backed portion of sub_7f9bd0's direct parameter formatter.
	 * Values are decoded upstream from the complete Param2..Param50 stream; this
	 * helper owns only the native row text/order. It is exported so CH/EU fixture
	 * rows can pin exact output without constructing the helper-bubble chrome.
	 */
	/*
================
CSkillData_BuildProjectedDirectParamRows
================
	*/
	function CSkillData_BuildProjectedDirectParamRows(
		row: SkillTooltipRowView,
		options: { includeDuration?: boolean; includeAttack?: boolean; } = {}
	): string[] {
		const rows: string[] = [];
		const includeDuration = options.includeDuration !== false;
		const includeAttack = options.includeAttack !== false;
		const direct = row.directTooltipParams;

		// sub_7f9bd0 is not numeric-offset ordered.  This list follows its exact
		// control-flow order and the map implements native last-writer ownership
		// when the source stream assigns the same CSkillData slot more than once.
		const blocksByOffset = new Map<number, NativeParamBlock>();
		for ( const block of direct.nativeParamBlocks ) blocksByOffset.set( block.offset, block );
		const order = [
			0x054,
			0x144,
			0x004,
			0x010,
			0x014,
			0x018,
			0x01c,
			0x028,
			0x02c,
			0x034,
			0x038,
			0x03c,
			0x040,
			0x044,
			0x1a4,
			0x04c,
			0x024,
			0x050,
			0x058,
			0x05c,
			0x060,
			0x064,
			0x068,
			0x088,
			0x08c,
			0x080,
			0x084,
			0x090,
			0x094,
			0x098,
			0x09c,
			0x0ac,
			0x0b0,
			0x0b4,
			0x1b0,
			0x0b8,
			0x0bc,
			0x0c8,
			0x0cc,
			0x0e0,
			0x0e4,
			0x0e8,
			0x0ec,
			0x0f0,
			0x0f4,
			0x0f8,
			0x0fc,
			0x1f8,
			0x1fc,
			0x200,
			0x204,
			0x208,
			0x20c,
			0x210,
			0x214,
			0x218,
			0x21c,
			0x220,
			0x224,
			0x228,
			0x22c,
			0x230,
			0x234,
			0x238,
			0x23c,
			0x240,
			0x244,
			0x278,
			0x27c,
			0x280,
			0x284,
			0x288,
			0x28c,
			0x290,
			0x294,
			0x298,
			0x2a4,
			0x26c,
			0x10c,
			0x110,
			0x114,
			0x118,
			0x1dc,
			0x13c,
			0x140,
			0x100,
			0x104,
			0x108,
			0x15c,
			0x160,
			0x164,
			0x168,
			0x16c,
			0x008,
			0x1a0,
			0x198,
			0x19c,
			0x1ac,
			0x1b8,
			0x1c4,
			0x1c8,
			0x1cc,
			0x1d0,
			0x0d4,
			0x0d8,
			0x0dc,
			0x1d8,
			0x1e4,
			0x1e8,
			0x1ec,
			0x1f0,
			0x070
		];
		for ( const offset of order ) {
			if ( (!includeDuration && offset === 0x054) || (!includeAttack && offset === 0x004) ) continue;
			const block = blocksByOffset.get( offset );
			if ( !block ) continue;
			if ( offset >= 0x15c && offset <= 0x16c ) {
				const value = projectedSetValueRow( block.values[0] ?? 0, block.values[1] ?? 0 );
				if ( value ) rows.push( value );
			} else {
				rows.push( ...projectedNativeRowsForBlock( row, block ) );
			}
		}
		return rows;
	}

	/**
	 * Browser twin of sub_7f8720 + sub_803c40. The accumulator retains the last
	 * block (and therefore the last chance/power/grade) while its byte-sized
	 * application count adds each participating record's raw mc count, or one
	 * when that record has no mc block.
	 */
	/*
================
CSkillData_BuildProjectedAggregateEffectRows
================
	*/
	function CSkillData_BuildProjectedAggregateEffectRows(
		chain: readonly SkillTooltipRowView[]
	): string[] {
		const rows: string[] = [];
		const contributionCount = ( row: SkillTooltipRowView ): number => {
			const countBlock = nativeParamBlock( row, 0x05c );
			return countBlock ? ((countBlock.values[1] ?? 0) & 0xff) : 1;
		};
		const aggregate = ( offset: number ): { owner: NativeParamBlock; count: number; } | null => {
			let owner: NativeParamBlock | null = null;
			let count = 0;
			for ( const row of chain ) {
				const block = nativeParamBlock( row, offset );
				if ( !block ) continue;
				owner = block;
				count = (count + contributionCount( row )) & 0xff;
			}
			return owner ? { owner, count } : null;
		};

		const powerEffects: Array<[number, string, boolean]> = [
			[ 0x0e8, "PARAM_FZ", false ],
			[ 0x0ec, "PARAM_FB", false ],
			[ 0x0f0, "PARAM_ES", false ],
			[ 0x0f4, "PARAM_BU", true ],
			[ 0x0f8, "PARAM_PS", false ]
		];
		for ( const [offset, symbol, hasLevel] of powerEffects ) {
			const effect = aggregate( offset );
			if ( !effect ) continue;
			const values = effect.owner.values;
			rows.push(
				`${text( symbol )}${text( "UIIT_STT_PROBABILITY" )} ${values[1] ?? 0}% (` +
					`${effect.count}${text( "UIIT_STT_COUNT" )} ${text( "PARAM_POWER" )}${values[0] ?? 0}` +
					`${hasLevel ? `, lv ${values[2] ?? 0}` : ""})`
			);
		}

		const timedEffects: Array<[number, string]> = [
			[ 0x1f8, "PARAM_SLEEP" ],
			[ 0x1fc, "PARAM_ROOT" ],
			[ 0x200, "PARAM_SLOW" ],
			[ 0x204, "PARAM_FEAR" ],
			[ 0x208, "PARAM_MYOPIA" ],
			[ 0x20c, "PARAM_BLOOD" ],
			[ 0x210, "PARAM_DN" ],
			[ 0x214, "PARAM_STUN" ],
			[ 0x218, "PARAM_DISEASE" ],
			[ 0x21c, "PARAM_CHAOS" ],
			[ 0x220, "PARAM_CURSE_STR" ],
			[ 0x224, "PARAM_CURSE_INT" ],
			[ 0x228, "PARAM_CURSE_PD" ],
			[ 0x22c, "PARAM_CURSE_MD" ],
			[ 0x230, "PARAM_CURSE_HP" ],
			[ 0x234, "PARAM_CURSIE_MP" ]
		];
		for ( const [offset, symbol] of timedEffects ) {
			const effect = aggregate( offset );
			if ( !effect ) continue;
			const values = effect.owner.values;
			rows.push(
				`${text( symbol )} ${values[2] ?? 0}${text( "UIIT_STT_GRADE" )} (` +
					`${text( "UIIT_STT_PROBABILITY" )}${values[1] ?? 0}% ${effect.count}${text( "UIIT_STT_COUNT" )}, ` +
					`${((values[0] ?? 0) / 1_000).toFixed( 1 )}${text( "PARAM_SECOND" )})`
			);
		}

		const timeBomb = aggregate( 0x238 );
		if ( timeBomb ) {
			const values = timeBomb.owner.values;
			rows.push(
				`${text( "PARAM_TIME_BOMB" )} ${values[2] ?? 0}${text( "UIIT_STT_GRADE" )} (` +
					`${text( "UIIT_STT_PROBABILITY" )}${values[1] ?? 0}% ${timeBomb.count}${text( "UIIT_STT_COUNT" )})`
			);
		}
		const knockback = aggregate( 0x02c );
		if ( knockback ) {
			rows.push(
				`${text( "PARAM_KB" )} ${knockback.owner.values[0] ?? 0}% (${knockback.count}${
					text( "UIIT_STT_COUNT" )
				})`
			);
		}
		const knockout = aggregate( 0x010 );
		if ( knockout ) {
			rows.push(
				`${text( "PARAM_KO" )} ${knockout.owner.values[1] ?? 0}%(` +
					`${knockout.count}${text( "UIIT_STT_COUNT" )}, lv ${knockout.owner.values[0] ?? 0})`
			);
		}
		return rows;
	}

	return {
		direct: CSkillData_BuildProjectedDirectParamRows,
		aggregate: CSkillData_BuildProjectedAggregateEffectRows,
		area: projectedAreaRow,
		block: nativeParamBlock
	};
}
