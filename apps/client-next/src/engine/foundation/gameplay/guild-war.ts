/*
===========================================================================

guild-war.ts - the native guild-war terms and outgoing requests

704390 sends 771B after the confirmation page; 701530 sends 7465 after
the surrender question. The score selector indexes the native table.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { SocialState } from "./social";

export const GUILD_WAR_PROPOSAL = 10;
export const WAR_UNLIMITED = 0x7fffffff;
export const WAR_MAX_STAKE = 500_000_000;
/*
================
warScoreLimits
================
*/
export function warScoreLimits(): readonly number[] {
	return [ 0, 5000, 10000, 50000, 100000, 150000, 250000, 300000 ];
}

/*
================
GuildWarTerms
================
*/
export interface GuildWarTerms {
	readonly name: string;
	readonly mode: number;
	readonly period: number;
	readonly scoreIndex: number;
	readonly stake: number;
}

/*
================
GuildWarCommand
================
*/
export type GuildWarCommand =
	| { kind: "guild-war-declare"; terms: GuildWarTerms; }
	| { kind: "guild-war-surrender"; id: number; };

/*
================
packWarPeriod

617970 maps the three combo indices to native duration fields.
================
*/
export function packWarPeriod( days: number, hours: number, minutes: number ): number {
	if ( days === 31 || hours === 24 || minutes === 6 ) return WAR_UNLIMITED;
	return ((days & 31) << 10 | (hours & 31) << 15 | minutes * 10 << 20) >>> 0;
}

/*
================
advanceGuildWarClock

6875F0 decrements the single surrender countdown on timer 0x0D. Zero
clears the timer without another notice. Ending one war clears it too.
================
*/
export function advanceGuildWarClock( state: SocialState, now: number ): SocialState {
	let next = state;
	const timer = state.warCountdown;
	if ( timer && now >= timer.nextAt ) {
		const remaining = Math.max( 0, timer.remaining - 1 );
		next = {
			...next,
			warCountdown: remaining ? { remaining, nextAt: now + 1000 } : undefined,
			notice: remaining ?
				{
					key: "UIIT_MSG_GUILDWAR_END_COUNTDOWN",
					value: remaining,
					notificationBanner: true,
					bannerOnly: true
				} :
				undefined
		};
	}
	if ( state.wars?.some( w => w.word3c !== WAR_UNLIMITED && now >= (w.clockAt ?? now) + 1000 && w.word3c > 0 ) ) {
		next = {
			...next,
			wars: state.wars.map( w => {
				if ( w.word3c === WAR_UNLIMITED || now < (w.clockAt ?? now) + 1000 ) return w;
				const elapsed = Math.floor( (now - (w.clockAt ?? now)) / 1000 );
				return {
					...w,
					word3c: Math.max( 0, w.word3c - elapsed ),
					clockAt: (w.clockAt ?? now) + elapsed * 1000
				};
			} )
		};
	}
	return next;
}

/*
================
guildWarRequest
================
*/
export function guildWarRequest( state: SocialState, command: GuildWarCommand ): WireFrame {
	if ( state.guild?.members.find( m => m.id === state.self )?.grade !== 0 ) throw Error( "Guild master required" );
	if ( command.kind === "guild-war-surrender" ) {
		if ( !state.wars?.some( war => war.id === command.id && !war.ending ) ) throw Error( "Guild war unavailable" );
		const payload = new Uint8Array( 4 );
		new DataView( payload.buffer ).setUint32( 0, command.id, true );
		return { opcode: 0x7465, payload };
	}
	const terms = command.terms, name = new TextEncoder().encode( terms.name );
	if ( name.length < 2 || name.length > 12 || terms.name.includes( "\0" ) || terms.name === state.guild.name ) {
		throw Error( "Invalid enemy guild name" );
	}
	if (
		!Number.isInteger( terms.scoreIndex ) || terms.scoreIndex < 0 || terms.scoreIndex >= warScoreLimits().length ||
		!Number.isInteger( terms.stake ) || terms.stake < 0 || terms.stake > WAR_MAX_STAKE ||
		!Number.isInteger( terms.period ) || terms.period < 0 || terms.period > 0xffffffff
	) throw Error( "Invalid guild war terms" );
	const payload = new Uint8Array( name.length + 12 ), view = new DataView( payload.buffer );
	view.setUint16( 0, name.length, true );
	payload.set( name, 2 );
	let at = name.length + 2;
	view.setUint8( at++, 0 );
	view.setUint32( at, terms.period, true );
	at += 4;
	view.setUint8( at++, terms.scoreIndex );
	view.setUint32( at, terms.stake, true );
	return { opcode: 0x771b, payload };
}

/*
================
guildWarProposalReply

76309C distinguishes the requesting and answering pending modes. A timeout
retires the answering dialog, while only the requester sees its notice.
================
*/
export function guildWarProposalReply( state: SocialState, result: number, name: string ): SocialState {
	let next = state;
	if ( result === 3 ) {
		next = {
			...next,
			warResult: {
				key: "UIIT_MSG_GUILDWAR_SUGGESTIONS_01",
				additionalKey: "UIIT_MSG_GUILDWAR_SUGGESTIONS_02",
				names: [],
				sequence: (state.warResult?.sequence ?? 0) + 1
			}
		};
	}
	if ( result === 0 || result === 2 ) {
		if ( result === 2 && state.warPending === 2 && state.invitation?.type === GUILD_WAR_PROPOSAL ) {
			next = { ...next, invitation: null };
		}
		if ( result === 0 || state.warPending === 1 ) {
			next = {
				...next,
				notice: {
					key: result === 0 ?
						"UIIT_MSG_GUILDWAR_WARREFUSAL" :
						"UIIT_MSG_GUILDWARERR_REQUISITION_TIME_OUT",
					value: 0,
					text: name
				}
			};
		}
	}
	return next;
}
