/*
===========================================================================

onboarding-steps.ts - the first-login tour's steps and which one is next

The tour covers only what this port adds to the v1.150 interface; the
original's own controls are left to the player and the native guide. Each
step names the element it points at by a CSS selector: a DOM tool (the
FPS chip, the bug launcher) or the transparent mirror the canvas UI keeps
for every control (`data-ui-id`). A step waits until its element is on
screen, so a contextual one (the party board) appears the first time the
player is in a party instead of pointing at nothing.

The player's progress is the set of step ids seen, so a step added later
is shown on its own to a player who already finished the tour.

===========================================================================
*/

/*
================
TourStep
================
*/
export interface TourStep {
	readonly id: string;
	readonly target: string;
	readonly title: string;
	readonly text: string;
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
			id: "self-target",
			target: '[data-ui-id="self-target"]',
			title: "Target yourself",
			text:
				"Click your portrait to select your own character, so a buff or heal that needs a target lands on you."
		},
		{
			id: "skill-queue",
			target: '[data-ui-id="hotbar:0"]',
			title: "Queued skills",
			text:
				"A skill pressed while another is still casting waits in a small icon over this slot and casts next. " +
				"A refused press shakes its slot."
		},
		{
			id: "chat-time",
			target: '[data-ui-id^="chat-line:"]',
			title: "Message time",
			text: "Hover a chat line to see the local time it arrived."
		},
		{
			id: "party-masteries",
			target: '[data-ui-id^="party-target:"]',
			title: "Party masteries",
			text: "Each party member shows their two main masteries beside their name."
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
