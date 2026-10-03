/*
===========================================================================

skill-slot.ts - one cell of the skill window's mastery board

A cell is one relation-group column of a mastery row.
CIFSkillBoard_PopulateTabSlots (584F40) binds it in one of two states:

	state 0	the group's highest learned rank
	state 8	nothing learned: the group's level-1 skill, as a preview

CIFSkillSlot_BindSkill (589050) then picks the icon, and
CIFSkillSlot_UpdateLevelUpButton (588AF0) picks the level-up button and
dims a state-8 preview once its mastery level is met (0x588D95). The dim
happens before the prerequisite walk is consulted (0x588DB6), so an
unlearned skill with an untrained predecessor is still drawn dimmed;
prerequisites only decide the button.

This module only decides. ui.ts draws what SkillSlot_Resolve returns.

===========================================================================
*/

import type { SkillUi } from "@/engine/foundation/ui/skill-layout";
import type { SkillMetadata, createSkillTrainingContext } from "@/engine/foundation/gameplay/skill-catalog";
import type { Progression } from "@/engine/foundation/gameplay/progression";

type SkillTraining = ReturnType<typeof createSkillTrainingContext>;
type SkillSlotCandidate = SkillUi["skills"][number];

/** Alpha 588AF0 gives a state-8 preview whose mastery level is met. */
export const SKILL_SLOT_PREVIEW_ALPHA = 0x80 / 0xff;

export type SkillSlotIcon =
	| { readonly kind: "skill"; readonly icon: string; }
	| { readonly kind: "mastery-disable"; }
	| { readonly kind: "empty"; };

export type SkillSlotButton =
	| { readonly kind: "learn"; readonly skill: SkillMetadata; readonly upgrade: boolean; }
	| { readonly kind: "max"; }
	| { readonly kind: "none"; };

export interface SkillSlot {
	/** The bound record: the highest learned rank, else the level-1 skill. */
	readonly entry: SkillSlotCandidate | undefined;
	/** The highest learned rank (bind state 0), when any. */
	readonly owned: SkillSlotCandidate | undefined;
	readonly icon: SkillSlotIcon;
	/** Icon alpha, 0..1: opaque except a revealed state-8 preview. */
	readonly alpha: number;
	readonly button: SkillSlotButton;
}

export interface SkillSlotInput {
	readonly candidates: readonly SkillSlotCandidate[];
	readonly training: SkillTraining;
	readonly masteries: readonly { readonly id: number; readonly level: number; }[];
	readonly progression: Progression | undefined;
}

/*
==================
SkillSlot_Bind

584F40: a group with a learned rank binds its highest one (state 0); an
unlearned group binds its level-1 skill (state 8), which may be absent.
==================
*/
function SkillSlot_Bind(
	candidates: readonly SkillSlotCandidate[],
	training: SkillTraining
): { entry: SkillSlotCandidate | undefined; owned: SkillSlotCandidate | undefined; } {
	let owned: SkillSlotCandidate | undefined;

	for ( const candidate of candidates ) {
		if ( training.learned( candidate.id ) && (!owned || candidate.level > owned.level) ) {
			owned = candidate;
		}
	}
	const entry = owned ?? candidates.find( candidate => candidate.level === 1 );
	return { entry, owned };
}

/*
==================
SkillSlot_MasteryLevelMet

8500A0: the bound skill's first mastery requirement (info+0xA4 id,
info+0xAC level) against the character's live mastery level. A skill
without a first requirement is never met this way.
==================
*/
function SkillSlot_MasteryLevelMet(
	entry: SkillSlotCandidate,
	training: SkillTraining,
	masteries: SkillSlotInput["masteries"]
): boolean {
	const required = training.skill( entry.id )?.masteries[0];

	if ( !required ) {
		return false;
	}
	return masteries.some( mastery => mastery.id === required.ID && mastery.level >= required.Level );
}

/*
==================
SkillSlot_Icon

589050 at 0x589144..0x589197: a learned skill, or one whose mastery level
is met, shows its own icon; anything else the mastery-disable icon. A
column with no bound record shows the empty cell.
==================
*/
function SkillSlot_Icon( entry: SkillSlotCandidate | undefined, revealed: boolean ): SkillSlotIcon {
	if ( !entry ) {
		return { kind: "empty" };
	}
	if ( !revealed ) {
		return { kind: "mastery-disable" };
	}
	return { kind: "skill", icon: entry.icon };
}

/*
==================
SkillSlot_Button

The level-up button: the next rank when it may be trained now, the max
marker when a learned group has no next rank, otherwise nothing. Training
admission (mastery, prerequisites, stats, SP) is the shared training
reason; the server rechecks it.
==================
*/
function SkillSlot_Button( input: SkillSlotInput, owned: SkillSlotCandidate | undefined ): SkillSlotButton {
	const nextLevel = (owned?.level ?? 0) + 1;
	const next = input.candidates.find( candidate => candidate.level === nextLevel );
	const ref = input.training.skill( next?.id );

	// CIFSkillSlot_UpdateLevelUpButton (588AF0) shows the button from mastery,
	// prerequisites and learned skills only: no request-pending gate. Hiding
	// every button while one request was in flight made them all blink for a
	// round trip. A second request is still refused at confirmation (5DE690).
	if ( ref && input.progression && !input.training.reason( ref, input.progression ) ) {
		return { kind: "learn", skill: ref, upgrade: !!owned };
	}
	if ( owned && !next ) {
		return { kind: "max" };
	}
	return { kind: "none" };
}

/*
==================
SkillSlot_Resolve

Everything the skill window draws for one cell.
==================
*/
export function SkillSlot_Resolve( input: SkillSlotInput ): SkillSlot {
	const { entry, owned } = SkillSlot_Bind( input.candidates, input.training );
	const revealed = !!owned || (!!entry && SkillSlot_MasteryLevelMet( entry, input.training, input.masteries ));

	// 588AF0 0x588D95: only a state-8 preview past the mastery gate dims.
	const alpha = !owned && entry && revealed ? SKILL_SLOT_PREVIEW_ALPHA : 1;

	return {
		entry,
		owned,
		icon: SkillSlot_Icon( entry, revealed ),
		alpha,
		button: entry ? SkillSlot_Button( input, owned ) : { kind: "none" }
	};
}
