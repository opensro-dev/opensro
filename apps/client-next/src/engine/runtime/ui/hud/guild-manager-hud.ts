/*
===========================================================================

guild-manager-hud.ts - the guild manager's dialogs

The guild set's rows (guild-manager.ts) raise native boxes: questions for
the level-up window (CIFGuildLevelUp), disband (box 0x13), leave (box
0x14), release (UIIT_MSG_MRELEASE_CONFIRM), the war compensation claim
(5D4050) and create's name and cost (boxes 0x11 and 0x12); the create input (5D2540), master-transfer selection (5F2390)
and subsequent confirmation (5D3EB0); and the election window (5F0940): the
vote row asks for the candidates first (0x7332, 5DA1B0 case 0x1A) and the
window opens when 0xB332 brings them (75CBD0), one row per candidate with
its ballots (5F0F30); a ticked candidate and Confirm cast the 0x7330 ballot.
The UI draws from this owner every frame.

===========================================================================
*/

import type { GuildVote } from "@/engine/foundation/gameplay/social";

// A guild name and a character name both fit twelve characters
// (UIIT_MSG_GUILDERR_INVALID_GUILDNAME_LEN).
const GUILD_FIELD_MAX_LENGTH = 12;

/*
================
GuildManagerAsk
================
*/
export interface GuildManagerAsk {
	readonly kind: "level-up" | "dissolve" | "secede" | "release" | "compensation" | "create-name" | "create-cost";
	readonly npc: number;
	readonly value: number;
	// The guild name the create questions name (5D3D10's boxes 0x11 and 0x12).
	readonly text?: string;
}

/*
================
GuildManagerField
================
*/
export interface GuildManagerField {
	readonly kind: "create" | "master-leave";
	readonly npc: number;
	readonly text: string;
	readonly member?: number;
	readonly confirmed?: boolean;
}

/*
================
GuildElection

The open election window: its vote, time left, the guild manager it was
opened at (the ballot names it) and the ticked candidate's option.
================
*/
export interface GuildElection {
	readonly id: number;
	readonly remainingMs: number;
	readonly npc: number;
	readonly option: number | null;
}

/*
================
createGuildManagerHud
================
*/
export function createGuildManagerHud() {
	let soldierNpc: number | undefined, soldierSequence = 0;
	let transferOffset = 0, electionOffset = 0;
	let ask: GuildManagerAsk | null = null,
		field: GuildManagerField | null = null,
		vote: GuildElection | null = null,
		awaiting: { readonly id: number; readonly npc: number; } | null = null;
	return {
		/*
        ================
        observeSoldiers

        766D30 and 5E4710 refresh the visible NPC talk window on a new delta.
        ================
        */
		observeSoldiers( npc: number | undefined, sequence: number ) {
			if ( soldierNpc !== npc ) soldierNpc = undefined;
			if ( sequence !== soldierSequence ) soldierNpc = npc;
			soldierSequence = sequence;
		},
		/*
        ================
        soldiers
        ================
        */
		soldiers() {
			return soldierNpc !== undefined;
		},
		/*
		================
		ask

		Raises a question; value carries the level or the amount it names,
		text the guild name.
		================
		*/
		ask( kind: GuildManagerAsk["kind"], npc: number, value = 0, text?: string ) {
			field = null;
			vote = null;
			awaiting = null;
			ask = text === undefined ? { kind, npc, value } : { kind, npc, value, text };
		},
		/*
		================
		question
		================
		*/
		question(): GuildManagerAsk | null {
			return ask;
		},
		/*
		================
		takeQuestion
		================
		*/
		takeQuestion(): GuildManagerAsk | null {
			const taken = ask;
			ask = null;
			return taken;
		},
		/*
		================
		openField
		================
		*/
		openField( kind: GuildManagerField["kind"], npc: number ) {
			transferOffset = 0;
			ask = null;
			vote = null;
			awaiting = null;
			field = { kind, npc, text: "" };
		},
		/*
		================
		field
		================
		*/
		field(): GuildManagerField | null {
			return field;
		},
		/*
		================
		selectTransfer
		================
		*/
		selectTransfer( member: number, name: string ) {
			if ( field?.kind === "master-leave" && !field.confirmed ) field = { ...field, member, text: name };
		},
		/*
		================
		confirmTransfer
		================
		*/
		confirmTransfer() {
			if ( field?.member ) field = { ...field, confirmed: true };
		},
		/*
		================
		transferPosition
		================
		*/
		transferPosition( count: number, delta = 0 ) {
			transferOffset = Math.max( 0, Math.min( Math.max( 0, count - 7 ), transferOffset + delta ) );
			return Math.floor( transferOffset );
		},
		/*
		================
		type

		Names keep their shape: letters, digits and underscores.
		================
		*/
		type( value: string ) {
			if ( field ) {
				field = { ...field, text: value.replace( /[^A-Za-z0-9_]/g, "" ).slice( 0, GUILD_FIELD_MAX_LENGTH ) };
			}
		},
		/*
		================
		takeField
		================
		*/
		takeField(): GuildManagerField | null {
			const taken = field;
			field = null;
			return taken;
		},
		/*
		================
		showVote
		================
		*/
		showVote( id: number, remainingMs: number, npc: number ) {
			ask = null;
			field = null;
			awaiting = null;
			electionOffset = 0;
			vote = { id, remainingMs, npc, option: null };
		},
		/*
		================
		awaitVote

		The vote row sent 0x7332; the window opens when 0xB332 brings the
		candidates (observeVote).
		================
		*/
		awaitVote( id: number, npc: number ) {
			ask = null;
			field = null;
			vote = null;
			awaiting = { id, npc };
		},
		/*
		================
		observeVote

		7603D0 type 5 refreshes the displayed time; type 3 closes the vote.
		0xB332's candidates open the window the vote row asked for.
		================
		*/
		observeVote( votes: readonly GuildVote[] ) {
			if ( awaiting ) {
				const asked = votes.find( row => row.id === awaiting?.id );
				if ( !asked ) {
					awaiting = null;
					return true;
				}
				if ( !asked.candidates ) return false;
				electionOffset = 0;
				vote = { id: asked.id, remainingMs: asked.remainingMs, npc: awaiting.npc, option: null };
				awaiting = null;
				return true;
			}
			if ( !vote ) return false;
			const current = votes.find( row => row.id === vote?.id );
			if ( current?.remainingMs === vote.remainingMs ) return false;
			vote = current ? { ...vote, remainingMs: current.remainingMs } : null;
			return true;
		},
		/*
		================
		pickCandidate

		5EFF00's check boxes hold one candidate at a time.
		================
		*/
		pickCandidate( option: number ) {
			if ( vote ) vote = { ...vote, option: vote.option === option ? null : option };
		},
		/*
		================
		electionPosition
		================
		*/
		electionPosition( count: number, delta = 0 ) {
			electionOffset = Math.max( 0, Math.min( Math.max( 0, count - 7 ), electionOffset + delta ) );
			return Math.floor( electionOffset );
		},
		/*
		================
		vote
		================
		*/
		vote() {
			return vote;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			ask = null;
			field = null;
			vote = null;
			awaiting = null;
		}
	};
}
