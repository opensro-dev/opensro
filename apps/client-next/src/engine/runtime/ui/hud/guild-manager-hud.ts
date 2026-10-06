/*
===========================================================================

guild-manager-hud.ts - the guild manager's dialogs

The guild set's rows (guild-manager.ts) raise native boxes: questions for
the level-up window (CIFGuildLevelUp), disband (box 0x13), leave (box
0x14), release (UIIT_MSG_MRELEASE_CONFIRM) and the war compensation claim
(5D4050); name fields for the create input (5D2540) and the master-leave
window (UIIT_MSG_MLEAVE_INPUTID); and the vote state, which a v1.150 client
can only read: it never receives the candidates (0x3A6C type 2 asserts).
The UI draws from this owner every frame.

===========================================================================
*/

// A guild name and a character name both fit twelve characters
// (UIIT_MSG_GUILDERR_INVALID_GUILDNAME_LEN).
const GUILD_FIELD_MAX_LENGTH = 12;

/*
================
GuildManagerAsk
================
*/
export interface GuildManagerAsk {
	readonly kind: "level-up" | "dissolve" | "secede" | "release" | "compensation";
	readonly npc: number;
	readonly value: number;
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
}

/*
================
createGuildManagerHud
================
*/
export function createGuildManagerHud() {
	let soldierNpc: number | undefined, soldierSequence = 0;
	let ask: GuildManagerAsk | null = null,
		field: GuildManagerField | null = null,
		vote: { readonly remainingMs: number; } | null = null;
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

		Raises a question; value carries the level or the amount it names.
		================
		*/
		ask( kind: GuildManagerAsk["kind"], npc: number, value = 0 ) {
			field = null;
			vote = null;
			ask = { kind, npc, value };
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
			ask = null;
			vote = null;
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
		showVote( remainingMs: number ) {
			ask = null;
			field = null;
			vote = { remainingMs };
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
		}
	};
}
