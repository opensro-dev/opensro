/*
===========================================================================

job-hud.ts - the job guild dialogs: join and withdraw confirmations and
the alias window

CIFNpcTalk_OnSimpleMsgBoxResult (5D26F0) sends the join (box type 4,
field tri_job_type) and the withdrawal (type 5) after their questions;
the job menu's alias rows open CIFJobAlias (6461C0 checks a name, 646470
confirms and submits it). The rank rows open CIFJobRank or
CIFJobContributionRank on the worker's answer (JobRanks.opened), and the
outcome row turns the talk into the outcome page (5D7870) until it is
collected. The UI draws from this owner every frame.

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
JobRankWindow

The open rank window: its job, its kind (0 activity, 1 contribution) and
the page its spin control shows.
================
*/
export interface JobRankWindow {
	readonly job: number;
	readonly kind: number;
	readonly page: number;
}

/*
================
createJobHud
================
*/
export function createJobHud() {
	let confirm: JobConfirm | null = null, alias: JobAliasWindow | null = null;
	let rank: JobRankWindow | null = null, rankSeen = 0;
	// The outcome page: the answer sequence shown, and the collections seen.
	let outcome: { readonly npc: number; readonly sequence: number; } | null = null, outcomeSeen = 0, collectedSeen = 0;
	return {
		/*
		================
		observe

		Opens a rank window for each new 0xB37E answer or cached open, and the
		outcome page for each new outcome told. True when a collection ended
		the outcome: the caller closes the talk, as 75D5C0 does.
		================
		*/
		observe(
			opened: { readonly job: number; readonly kind: number; readonly sequence: number; } | null | undefined,
			told: { readonly npc: number; readonly sequence: number; readonly collected: number; } | null | undefined
		): boolean {
			if ( opened && opened.sequence > rankSeen ) {
				rankSeen = opened.sequence;
				rank = { job: opened.job, kind: opened.kind, page: 0 };
			}
			if ( !told ) return false;
			if ( told.collected > collectedSeen ) {
				collectedSeen = told.collected;
				outcomeSeen = told.sequence;
				outcome = null;
				return true;
			}
			if ( told.sequence > outcomeSeen ) {
				outcomeSeen = told.sequence;
				outcome = { npc: told.npc, sequence: told.sequence };
			}
			return false;
		},
		/*
		================
		rank
		================
		*/
		rank(): JobRankWindow | null {
			return rank;
		},
		/*
		================
		pageRank

		The spin control: one page back or forward within pages.
		================
		*/
		pageRank( step: number, pages: number ) {
			if ( rank ) rank = { ...rank, page: Math.max( 0, Math.min( pages - 1, rank.page + step ) ) };
		},
		/*
		================
		closeRank
		================
		*/
		closeRank() {
			rank = null;
		},
		/*
		================
		outcome

		The NPC whose talk shows the outcome page, or null.
		================
		*/
		outcome(): number | null {
			return outcome?.npc ?? null;
		},
		/*
		================
		closeOutcome
		================
		*/
		closeOutcome() {
			outcome = null;
		},
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
			outcome = null;
		}
	};
}
