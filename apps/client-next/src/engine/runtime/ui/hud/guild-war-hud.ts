/*
===========================================================================

guild-war-hud.ts - hostile guild selection and the native war dialogs

CIFRequestGuildWar uses input, confirmation and agreement pages. The input
page retains one money box and one duration selector; unlimited selections
move all three duration controls together (617730).

===========================================================================
*/
import { packWarPeriod, type GuildWarCommand, type GuildWarTerms } from "@/engine/foundation/gameplay/guild-war";
import type { GuildMember, GuildWar, SocialState } from "@/engine/foundation/gameplay/social";

/*
================
WarDraft
================
*/
interface WarDraft {
	name: string;
	scoreIndex: number;
	days: number;
	hours: number;
	minutes: number;
	stake: number;
}

/*
================
createGuildWarHud
================
*/
export function createGuildWarHud() {
	let relation = 0, selected = 0, offset = 0, contributionOffset = 0;
	let resultSequence = 0, result: SocialState["warResult"];
	let mode: "closed" | "input" | "confirm" | "surrender" = "closed";
	let combo = 0, comboOffset = 0, money: string | null = null, contributionSort = 3;
	let draft: WarDraft = { name: "", scoreIndex: 2, days: 1, hours: 0, minutes: 0, stake: 0 };
	/*
	================
	terms
	================
	*/
	function terms(): GuildWarTerms {
		return {
			name: draft.name,
			mode: 0,
			period: packWarPeriod( draft.days, draft.hours, draft.minutes ),
			scoreIndex: draft.scoreIndex,
			stake: draft.stake
		};
	}
	return {
		/*
		================
		state
		================
		*/
		state() {
			return {
				result,
				relation,
				selected,
				offset: Math.round( offset ),
				contributionOffset: Math.round( contributionOffset ),
				mode,
				combo,
				comboOffset: Math.round( comboOffset ),
				money,
				draft: { ...draft },
				terms: terms()
			};
		},
		/*
		================
		order
		================
		*/
		order( rows: readonly GuildWar[] ) {
			return [ ...rows ].sort( ( a, b ) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0 );
		},
		/*
        ================
        members

        82B9A0 assigns dense ranks: equal scores share a rank. 616990 uses
        name, rank, score or the member-map order; headers do not toggle.
        ================
        */
		members( members: readonly GuildMember[] ) {
			const scores = [ ...new Set( members.map( m => m.warScore ?? 0 ) ) ].sort( ( a, b ) => b - a );
			return members.map( member => ({ ...member, rank: scores.indexOf( member.warScore ?? 0 ) + 1 }) )
				.sort( ( a, b ) =>
					contributionSort === 0 ?
						(a.name < b.name ? -1 : a.name > b.name ? 1 : 0) :
						contributionSort === 1 ?
						a.rank - b.rank || a.id - b.id :
						contributionSort === 2 ?
						(a.warScore ?? 0) - (b.warScore ?? 0) || a.id - b.id :
						a.id - b.id
				);
		},

		/*
		================
		type
		================
		*/
		type( id: string, value: string, gold: number ) {
			if ( id === "war-name" ) draft.name = value;
			if ( id === "war-money" && money !== null && /^\d{0,10}$/.test( value ) ) {
				money = value ?
					String( Math.min( Number( value ), gold, 0xffffffff ) ) :
					"";
			}
		},
		/*
		================
		command
		================
		*/
		command( id: string, social: SocialState | undefined ): GuildWarCommand | null {
			const master = social?.guild?.members.find( m => m.id === social.self )?.grade === 0;
			if ( id === "war-result-close" ) result = undefined;
			else if ( id.startsWith( "war-relation:" ) ) {
				relation = Number( id.split( ":" )[1] );
				offset = 0;
			} else if ( id.startsWith( "war-select:" ) ) selected = Number( id.split( ":" )[1] );
			else if ( id.startsWith( "war-contribution-sort:" ) ) {
				contributionSort = Number( id.split( ":" )[1] ) - 60;
				contributionOffset = 0;
			} else if ( id === "war-declare" && master ) mode = "input";
			else if (
				id === "war-surrender" && master && social?.wars?.some( row => row.id === selected && !row.ending )
			) mode = "surrender";
			else if ( id === "war-cancel" ) {
				mode = "closed";
				combo = 0;
				money = null;
			} else if ( id === "war-money-open" && mode === "input" ) money = String( draft.stake );
			else if ( id === "war-money-cancel" ) money = null;
			else if ( id === "war-money-ok" && money !== null ) {
				draft.stake = Number( money );
				money = null;
			} else if ( id.startsWith( "war-combo:" ) ) {
				const next = Number( id.split( ":" )[1] );
				combo = combo === next ? 0 : next;
				comboOffset = 0;
			} else if ( id.startsWith( "war-choice:" ) ) {
				const value = Number( id.split( ":" )[1] );
				if ( combo === 23 ) draft.scoreIndex = value;
				else if ( combo >= 24 && combo <= 26 ) {
					const maxima = [ 31, 24, 6 ];
					const values = [ draft.days, draft.hours, draft.minutes ];
					if ( value === maxima[combo - 24] ) values.splice( 0, 3, ...maxima );
					else {
						for ( let i = 0; i < 3; i++ ) if ( values[i] === maxima[i] ) values[i] = 0;
						values[combo - 24] = value;
					}
					[draft.days, draft.hours, draft.minutes] = values as [number, number, number];
				}
				combo = 0;
			} else if ( id === "war-confirm" && master ) {
				if ( mode === "input" ) mode = "confirm";
				else if ( mode === "confirm" ) {
					mode = "closed";
					return { kind: "guild-war-declare", terms: terms() };
				} else if ( mode === "surrender" ) {
					mode = "closed";
					return { kind: "guild-war-surrender", id: selected };
				}
			}
			return null;
		},
		/*
		================
		scroll
		================
		*/
		scroll( target: "enemies" | "members" | "combo", delta: number, count: number, visible: number ) {
			const limit = Math.max( 0, count - visible );
			if ( target === "enemies" ) offset = Math.max( 0, Math.min( limit, offset + delta ) );
			if ( target === "members" ) {
				contributionOffset = Math.max( 0, Math.min( limit, contributionOffset + delta ) );
			}
			if ( target === "combo" ) comboOffset = Math.max( 0, Math.min( limit, comboOffset + delta ) );
		},
		/*
		================
		reconcile
		================
		*/
		reconcile( social: SocialState | undefined ) {
			if ( social?.warResult && social.warResult.sequence > resultSequence ) {
				result = social.warResult;
				resultSequence = result.sequence;
			}
			if ( social?.guild?.members.find( m => m.id === social.self )?.grade !== 0 ) {
				mode = "closed";
				combo = 0;
				money = null;
			}
			if ( !social?.wars?.some( row => row.id === selected ) ) {
				selected = 0;
				if ( mode === "surrender" ) mode = "closed";
			}
		},
		/*
		================
		reset
		================
		*/
		reset( sessionEnded = false ) {
			if ( sessionEnded ) {
				result = undefined;
				resultSequence = 0;
			}
			mode = "closed";
			combo = 0;
			money = null;
			selected = 0;
			offset = 0;
			contributionOffset = 0;
		}
	};
}
