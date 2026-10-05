/*
===========================================================================

onboarding-steps.ts - the first-login tour's steps and which one is next

The tour covers only what this port adds to the v1.150 interface; the
original's own controls are left to the player and the native guide. Each
step names the element it points at by a CSS selector: a DOM tool (the
FPS chip, the bug launcher) or the transparent mirror the canvas UI keeps
for every control (`data-ui-id`). A step waits until its element is on
screen (the bug launcher shows only while bug reports are enabled) instead
of pointing at nothing.

The player's progress is the set of step ids seen, so a step added later
is shown on its own to a player who already finished the tour.

===========================================================================
*/

import { HOTBAR_PAGE_COUNT, hotbarSlot } from "@/engine/foundation/gameplay/quickslots";
import { skillQueueChipReach } from "./skill-press-feedback";

// GDR_TMPQS_* are 32 by 32 (ifunderbar).
const QUICKSLOT_PX = 32;

/*
================
TourStep
================
*/
export interface TourStep {
	readonly id: string;
	readonly target: string;
	/** How far above its element the lit window reaches, in element heights. */
	readonly reachAbove?: number;
	readonly title: string;
	readonly text: string;
}

/*
================
slotOneSelector

Main bar key 1 on any page: the queued-skill chip sits above it
(skill-press-feedback.ts), and its control id follows the page.
================
*/
function slotOneSelector() {
	const ids: string[] = [];
	for ( let page = 0; page < HOTBAR_PAGE_COUNT; page++ ) ids.push( `[data-ui-id="hotbar:${hotbarSlot( page, 1 )}"]` );
	return ids.join( "," );
}

/*
================
tourSteps

In the order they are offered.
================
*/
export function tourSteps(): readonly TourStep[] {
	return [
		{
			id: "fps-chip",
			target: "#fps-toggle",
			title: "Performance readout",
			text: "Opens the frame rate and frame timing, and the builds the client and server are running."
		},
		{
			id: "bug-report",
			target: ".sro-bug-launcher",
			title: "Report a bug",
			text: "Describe what went wrong and send it to the team, with a short replay of the last moments " +
				"if you allow it. Typing /bug in the chat opens it too."
		},
		{
			id: "skill-queue",
			target: slotOneSelector(),
			reachAbove: skillQueueChipReach() / QUICKSLOT_PX,
			title: "Queued skills",
			text: "A skill pressed while another is still casting waits in a small icon above slot 1 and casts next. " +
				"A refused press shakes its slot."
		}
	];
}

/*
================
parseSeen

The stored list of seen step ids; anything unreadable counts as none.
================
*/
export function parseSeen( raw: string | null ): Set<string> {
	if ( !raw ) return new Set();
	try {
		const value: unknown = JSON.parse( raw );
		return new Set( Array.isArray( value ) ? value.filter( id => typeof id === "string" ) : [] );
	} catch {
		return new Set();
	}
}

/*
================
nextStep

The first unseen step whose element is on screen, or null.
================
*/
export function nextStep(
	steps: readonly TourStep[],
	seen: ReadonlySet<string>,
	visible: ( selector: string ) => boolean
) {
	return steps.find( step => !seen.has( step.id ) && visible( step.target ) ) ?? null;
}
