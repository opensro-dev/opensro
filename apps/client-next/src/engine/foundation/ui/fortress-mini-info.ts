/*
===========================================================================

fortress-mini-info.ts - the native fortress indicators on the player panel

CIFPlayerMiniInfo 6B5370 shows a battle rank for the current siege world;
6B5A50 shows the guild's active fortress and its two countdowns. State stays
with the existing fortress stream owner. These functions only present it.

===========================================================================
*/
import { fortressActive, type FortressState } from "@/engine/foundation/gameplay/fortress";
import { fortressWarFormat } from "./fortress-war-apply";

// Enabled v1.150 siegefortressbattlerank.txt rows, in unsigned threshold
// order (7E1C60); names are the 7BB8C0 switch, icons are row column 5.
/*
================
battleRanks
================
*/
function battleRanks() {
	return [
		{ kills: 15, name: "SN_SKILL_ASSAULTING_SOILDER", icon: "rank_assault_soilder" },
		{ kills: 25, name: "SN_SKILL_ELITE_ASSAULTING_SOILDER", icon: "rank_elite_assault_soilder" },
		{ kills: 45, name: "SN_SKILL_CENTURION", icon: "rank_centurion" },
		{ kills: 70, name: "SN_SKILL_ASSAULTING_LEADER", icon: "rank_assault_leader" },
		{ kills: 100, name: "SN_SKILL_ELITE_IMPERIAL_GUARD", icon: "rank_elite_guard" },
		{ kills: 150, name: "SN_SKILL_COMBAT_COMMANDER", icon: "rank_combat_commander" }
	] as const;
}
const SECONDS_PER_MINUTE = 60;

/*
================
FortressMiniIndicator
================
*/
export interface FortressMiniIndicator {
	readonly control: string;
	readonly image?: string;
	readonly text: string;
}

/*
================
fortressMiniIndicators

A rank is hidden outside an active fortress world. Guild status instead
uses the guild's listed fortress, so it can show while the player is away.
================
*/
export function fortressMiniIndicators(
	state: FortressState,
	inGuild: boolean,
	text: ( key: string ) => string
): FortressMiniIndicator[] {
	const out: FortressMiniIndicator[] = [];
	if ( fortressActive( state ) ) {
		const kills = state.localKills ?? 0, deaths = state.localDeaths ?? 0;
		const ranks = battleRanks();
		let rank: typeof ranks[number] | undefined;
		for ( const row of ranks ) if ( kills >= row.kills ) rank = row;
		if ( rank ) {
			out.push( {
				control: "GDR_PMI_BATTLE_GRADE",
				image: `/assets/images/Media_extracted/icon/etc/${rank.icon}.png`,
				text: [
					text( rank.name ),
					fortressWarFormat( text( "UIIT_STT_FORT_PK_NUM_01" ), [ kills ] ),
					fortressWarFormat( text( "UIIT_STT_FORT_PK_NUM_02" ), [ deaths ] )
				].join( "\n" )
			} );
		}
	}
	const war = inGuild && state.wars.find( row => row.id === state.listId && (row.flags & 1) !== 0 );
	if ( war ) {
		const fortress = state.fortresses.find( row => row.id === war.id );
		const lines = [
			`${text( "UIIT_STT_FORT_OFFICAL_FORTRESSNAME" )} : ${text( fortress?.nameStrId ?? "" )}`,
			`${text( "UIIT_STT_FORT_OFFICAL_OCCUPYGUILD" )} : ${war.name}`
		];
		for (
			const [seconds, key] of [
				[ war.captureWait ?? 0, "UIIT_STT_FORT_ETC_ENTER_COUNTDOWN" ],
				[ war.stoneWait ?? 0, "UIIT_STT_FORT_ETC_ATTACK_FORT_STONE_COUNTDOWN" ]
			] as const
		) {
			if ( !seconds ) continue;
			const minutes = Math.floor( seconds / SECONDS_PER_MINUTE ), rest = seconds % SECONDS_PER_MINUTE;
			const duration = (minutes ? `${minutes}${text( "PARAM_MINUTE" )} ` : "") +
				`${rest}${text( "PARAM_SECOND" )} `;
			lines.push( `${text( key )} : ${duration}` );
		}
		out.push( { control: "GDR_PMI_FORTRESS_INFO", text: lines.join( "\n" ) } );
	}
	return out;
}
