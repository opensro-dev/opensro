/*
===========================================================================

job-hud.ts - the job guild dialogs: join and withdraw confirmations and
the alias window

CIFNpcTalk_OnSimpleMsgBoxResult (5D26F0) sends the join (box type 4,
field tri_job_type) and the withdrawal (type 5) after their questions;
the job menu's alias rows open CIFJobAlias (6461C0 checks a name, 646470
confirms and submits it). The UI draws from this owner every frame.

===========================================================================
*/

// The alias field's limit: the character name's 12 characters.
const JOB_ALIAS_MAX_LENGTH = 12;

/*
================
JobConfirm
================
*/
interface JobConfirm {
	readonly kind: "join" | "withdraw";
	readonly npc: number;
	readonly job: number;
}

/*
================
JobAliasWindow
================
*/
interface JobAliasWindow {
	readonly npc: number;
	readonly modify: boolean;
	readonly text: string;
}

/*
================
createJobHud
================
*/
export function createJobHud() {
	let confirm: JobConfirm | null = null, alias: JobAliasWindow | null = null;
	return {
		/*
		================
		ask

		Raises the join or withdrawal question for a guild NPC.
		================
		*/
		ask( kind: JobConfirm["kind"], npc: number, job: number ) {
			alias = null;
			confirm = { kind, npc, job };
		},
		/*
		================
		confirm
		================
		*/
		confirm(): JobConfirm | null {
			return confirm;
		},
		/*
		================
		takeConfirm

		Closes the question, returning it.
		================
		*/
		takeConfirm(): JobConfirm | null {
			const taken = confirm;
			confirm = null;
			return taken;
		},
		/*
		================
		openAlias
		================
		*/
		openAlias( npc: number, modify: boolean ) {
			confirm = null;
			alias = { npc, modify, text: "" };
		},
		/*
		================
		alias
		================
		*/
		alias(): JobAliasWindow | null {
			return alias;
		},
		/*
		================
		typeAlias

		The field keeps the name shape: letters, digits and underscores.
		================
		*/
		typeAlias( value: string ) {
			if ( alias ) {
				alias = { ...alias, text: value.replace( /[^A-Za-z0-9_]/g, "" ).slice( 0, JOB_ALIAS_MAX_LENGTH ) };
			}
		},
		/*
		================
		closeAlias
		================
		*/
		closeAlias() {
			alias = null;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			confirm = null;
			alias = null;
		}
	};
}
