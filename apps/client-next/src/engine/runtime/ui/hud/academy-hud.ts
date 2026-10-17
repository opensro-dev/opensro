/*
===========================================================================

academy-hud.ts - Academy Honor Ranking, slot actions and Guardian Evaluation

Owns the client UI state for the storage keeper's Honor Ranking window
(CIFApprenticeShipHonorRanking 6E8C10 / CIFApprenticeShipHonorSlot 6E91D0),
the Academy window's Dissolve (5C51A0) and member-slot Exit/Graduate
buttons (CIFApprenticeShipSlot 5C6F20..5C7938), and the Apprentice
graduation Guardian Evaluation dialog (CIFApprenticeShipJudge 6E97A0).

===========================================================================
*/

import type {
	AcademyCommand,
	AcademyHonorRow,
	AcademyMember,
	AcademyState
} from "@/engine/foundation/gameplay/academy";

export const ACADEMY_HONOR_PANEL = "Academy honor";
export const ACADEMY_HONOR_PAGE_ROWS = 10;

export interface AcademyHonorSlotView {
	readonly rankingText: string;
	readonly badgePath: string;
	readonly nameText: string;
	readonly levelText: string;
	readonly graduatesText: string;
	readonly guildText: string;
}

export type AcademySlotAction =
	| {
		readonly kind: "compulsory-graduate";
		readonly labelKey: "UIIT_CTL_TC_GRADUATION";
		readonly name: string;
	}
	| {
		readonly kind: "ban";
		readonly labelKey: "UIIT_CTL_BAN_PARTY";
		readonly name: string;
	}
	| {
		readonly kind: "apprentice-graduate";
		readonly labelKey: "UIIT_CTL_TC_GRADUATION";
		readonly name: string;
	}
	| {
		readonly kind: "submentor-graduate";
		readonly labelKey: "UIIT_CTL_TC_GRADUATION";
		readonly name: string;
	}
	| {
		readonly kind: "leave";
		readonly labelKey: "UIIT_CTL_LEAVE_PARTY";
		readonly name: string;
	};

export interface AcademyConfirm {
	readonly key: string;
	readonly args: readonly string[];
	readonly command: AcademyCommand;
}

export interface AcademyJudgeOption {
	readonly index: number;
	readonly satisfaction: number;
	readonly labelKey: string;
}

// 6E97A0 / 6E9AC0: radio index 0..4 maps to wire satisfaction 4..0 (4 - i);
// 6E9A62 selects index 1 (wire 3, Satisfaction) whenever the dialog opens.
export const ACADEMY_JUDGE_DEFAULT_SATISFACTION = 3;
export const ACADEMY_JUDGE_OPTIONS: readonly AcademyJudgeOption[] = [
	{ index: 0, satisfaction: 4, labelKey: "UIIT_STT_TC_APPRAISAL_VERY_SATISFACTION" },
	{ index: 1, satisfaction: 3, labelKey: "UIIT_STT_TC_APPRAISAL_SATISFACTION" },
	{ index: 2, satisfaction: 2, labelKey: "UIIT_STT_TC_APPRAISAL_NORMAL" },
	{ index: 3, satisfaction: 1, labelKey: "UIIT_STT_TC_APPRAISAL_DESPAIR" },
	{ index: 4, satisfaction: 0, labelKey: "UIIT_STT_TC_APPRAISAL_VERY_DESPAIR" }
];

/*
================
academyHonorBadge

5C6572 / 6E9340: evaluation tiers 1..5 use com_honor_level_1..5.ddj; 0 or
out-of-range clears the badge texture.
================
*/
export function academyHonorBadge( rank: number | undefined ): string {
	return rank !== undefined && Number.isInteger( rank ) && rank >= 1 && rank <= 5 ?
		`interface/ifcommon/com_honor_level_${rank}.png` :
		"";
}

/*
================
academyHonorPages

6E9080: 10 rows per page, clamped to at least 1 page when the list is empty.
================
*/
export function academyHonorPages( rowCount: number ): number {
	return Math.max( 1, Math.ceil( Math.max( 0, rowCount ) / ACADEMY_HONOR_PAGE_ROWS ) );
}

/*
================
academyHonorPage

6E8FC0 / 6E9300: slices the current 10-row page and formats each slot's
six cells (ranking, badge, name, level(maxLevel)Lv, graduates, guild).
================
*/
export function academyHonorPage(
	rows: readonly AcademyHonorRow[],
	page: number,
	strings: Readonly<Record<string, string>>
): readonly (AcademyHonorSlotView | null)[] {
	const clamped = Math.max( 0, Math.min( academyHonorPages( rows.length ) - 1, page ) );
	const base = clamped * ACADEMY_HONOR_PAGE_ROWS;
	const lvSuffix = strings.UIIT_STT_LEVEL_LV ?? "";
	const noGuild = strings.UIIT_STT_NO_GUILD ?? "";
	const slots: (AcademyHonorSlotView | null)[] = [];
	for ( let i = 0; i < ACADEMY_HONOR_PAGE_ROWS; i++ ) {
		const row = rows[base + i];
		if ( !row ) {
			slots.push( null );
			continue;
		}
		slots.push( {
			rankingText: String( row.ranking ),
			badgePath: academyHonorBadge( row.rank ),
			nameText: row.name,
			levelText: `${row.level}(${row.maxLevel})${lvSuffix}`,
			graduatesText: String( row.graduates ),
			guildText: row.guild || noGuild
		} );
	}
	return slots;
}

/*
================
academyIsLeader

5C6020 / 5C7125: the local player is the Academy Leader when the camp has a
kind-0 member whose id equals localMemberId.
================
*/
export function academyIsLeader( camp: AcademyState | undefined ): boolean {
	if ( !camp?.member || camp.localMemberId === undefined ) return false;
	return (camp.members ?? []).some( m => m.kind === 0 && m.id === camp.localMemberId );
}

/*
================
academySlotAction

5C6F20 / 5C7550: decides the caption and click action for a non-leader
member slot's button 18.
- Leader view (5C7159, 5C758F): strict `>` (level > 40 for kind 2,
  level > 60 for kind 1) Compulsory Graduation; otherwise Ban.
- Non-leader own row (5C71C6, 5C77A1): `>=` (level >= 40 for kind 2 opens
  CIFApprenticeShipJudge, level >= 60 for kind 1 confirms graduation with
  satisfaction 0); otherwise Leave.
- Any other row hides button 18.
================
*/
export function academySlotAction(
	camp: AcademyState | undefined,
	member: AcademyMember
): AcademySlotAction | null {
	if ( !camp?.member || member.kind === 0 ) return null;
	if ( academyIsLeader( camp ) ) {
		if ( (member.kind === 2 && member.level > 40) || (member.kind === 1 && member.level > 60) ) {
			return { kind: "compulsory-graduate", labelKey: "UIIT_CTL_TC_GRADUATION", name: member.name };
		}
		return { kind: "ban", labelKey: "UIIT_CTL_BAN_PARTY", name: member.name };
	}
	if ( member.id !== camp.localMemberId ) return null;
	if ( member.kind === 2 && member.level >= 40 ) {
		return { kind: "apprentice-graduate", labelKey: "UIIT_CTL_TC_GRADUATION", name: member.name };
	}
	if ( member.kind === 1 && member.level >= 60 ) {
		return { kind: "submentor-graduate", labelKey: "UIIT_CTL_TC_GRADUATION", name: member.name };
	}
	return { kind: "leave", labelKey: "UIIT_CTL_LEAVE_PARTY", name: member.name };
}

/*
================
createAcademyHud
================
*/
export function createAcademyHud() {
	let honorPage: number | null = null;
	let honorSeen = 0;
	let confirm: AcademyConfirm | null = null;
	let judgeSatisfaction: number | null = null;

	return {
		/*
		================
		observeHonor

		771750 -> 69EEE0: each accepted 0xB2D0 opens CIFApprenticeShipHonorRanking
		at page 0.
		================
		*/
		observeHonor( honorRanks: AcademyState["honorRanks"] ): boolean {
			const seq = honorRanks?.sequence ?? 0;
			if ( seq <= honorSeen ) return false;
			honorSeen = seq;
			honorPage = 0;
			return true;
		},
		/*
		================
		honor
		================
		*/
		honor() {
			return honorPage === null ? null : { page: honorPage };
		},
		/*
		================
		turnHonorPage

		6E9100 / 6E9130: prev/next buttons step within 0..totalPages-1.
		================
		*/
		turnHonorPage( delta: -1 | 1, totalPages: number ): boolean {
			if ( honorPage === null ) return false;
			const next = Math.max( 0, Math.min( Math.max( 1, totalPages ) - 1, honorPage + delta ) );
			if ( next === honorPage ) return false;
			honorPage = next;
			return true;
		},
		/*
		================
		closeHonor
		================
		*/
		closeHonor() {
			honorPage = null;
		},
		/*
		================
		askDisband

		5C51A0 -> 5C53C0 case 1: confirms UIIT_STT_TC_DISSOLUTION before 0x77C5.
		================
		*/
		askDisband() {
			judgeSatisfaction = null;
			confirm = {
				key: "UIIT_STT_TC_DISSOLUTION",
				args: [],
				command: { kind: "academy-disband" }
			};
		},
		/*
		================
		activateSlotAction

		5C7550 -> 5C76C0 / 6E9A40: routes button 18 to either CIFApprenticeShipJudge
		or one of the four type-0x14 confirmation message boxes.
		================
		*/
		activateSlotAction( action: AcademySlotAction ) {
			if ( action.kind === "apprentice-graduate" ) {
				confirm = null;
				judgeSatisfaction = ACADEMY_JUDGE_DEFAULT_SATISFACTION;
				return;
			}
			judgeSatisfaction = null;
			if ( action.kind === "compulsory-graduate" ) {
				confirm = {
					key: "UIIT_MSG_TC_GRADUATION_COMPULSION",
					args: [ action.name ],
					command: { kind: "academy-compulsory-graduate", name: action.name }
				};
				return;
			}
			if ( action.kind === "ban" ) {
				confirm = {
					key: "UIIT_STT_TC_BAN_TRAININGCAMP",
					args: [ action.name ],
					command: { kind: "academy-kick", name: action.name }
				};
				return;
			}
			if ( action.kind === "submentor-graduate" ) {
				confirm = {
					key: "UIIT_STT_TC_GRADUATION",
					args: [],
					command: { kind: "academy-graduate", satisfaction: 0 }
				};
				return;
			}
			confirm = {
				key: "UIIT_STT_TC_OUT_TRAININGCAMP",
				args: [],
				command: { kind: "academy-leave" }
			};
		},
		/*
		================
		currentConfirm
		================
		*/
		currentConfirm() {
			return confirm;
		},
		/*
		================
		takeConfirm
		================
		*/
		takeConfirm(): AcademyCommand | null {
			const next = confirm?.command ?? null;
			confirm = null;
			return next;
		},
		/*
		================
		cancelConfirm
		================
		*/
		cancelConfirm() {
			confirm = null;
		},
		/*
		================
		judge
		================
		*/
		judge(): number | null {
			return judgeSatisfaction;
		},
		/*
		================
		selectJudge
		================
		*/
		selectJudge( satisfaction: number ) {
			if ( judgeSatisfaction === null ) return;
			if ( ACADEMY_JUDGE_OPTIONS.some( o => o.satisfaction === satisfaction ) ) {
				judgeSatisfaction = satisfaction;
			}
		},
		/*
		================
		takeJudge

		6E9AC0: sends 0x710A with the selected satisfaction (4..0) and closes.
		================
		*/
		takeJudge(): AcademyCommand | null {
			if ( judgeSatisfaction === null ) return null;
			const satisfaction = judgeSatisfaction;
			judgeSatisfaction = null;
			return { kind: "academy-graduate", satisfaction };
		},
		/*
		================
		closeJudge
		================
		*/
		closeJudge() {
			judgeSatisfaction = null;
		},
		/*
		================
		syncCamp

		773FA5 / 774162: leaving or dissolving the camp closes any open judge or
		confirm modal.
		================
		*/
		syncCamp( camp: AcademyState | undefined ) {
			if ( !camp?.member ) {
				confirm = null;
				judgeSatisfaction = null;
			}
		},
		/*
		================
		reset
		================
		*/
		reset() {
			honorPage = null;
			honorSeen = 0;
			confirm = null;
			judgeSatisfaction = null;
		}
	};
}
