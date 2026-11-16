/*
===========================================================================

server-event-hud.ts - the Server Mission gauge widget (port-only, not native)

The later v1.696 client's CNIFServerEvent (res_ui\nifserverevent.2dt):
a 120x24 frame with a snow icon, an 80x4 gauge filled count/goal and the
"%d/%d" text (0x00E1E940), and a close button. The client moves it 244 px
left of the screen's right edge (0x0080F4E0: x = right - 0xF4), at the
2dt's y. v1.150 has no such window, so it shows only with the
Experimental "Server Mission gauge" row on; its art is the v1.696
interface/event/winter_count_* published by the later-client import.

The close button hides the window (0x00E1EB00); the client creates it
again only when it does not exist, so a hidden window stays hidden while
the mission runs. Inference: it shows again once the mission is gone
(the event's end, or a new session) and comes back.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import type { GameplayState } from "@/engine/contracts/gameplay";

type ServerEventGauge = NonNullable<GameplayState["serverEventGauge"]>;

// interface/event/winter_count_ + wind | gage | snow | close[_focus|_press] + .png
export const SERVER_EVENT_ART = "/assets/images/Media_extracted/interface/event/winter_count_";

// The 2dt is laid out for a 1024-wide screen.
const NATIVE_WIDTH = 1024;
const FRAME: UiRect = [ 780, 56, 120, 24 ];
const GAUGE: UiRect = [ 803, 59, 80, 4 ];
const TEXT: UiRect = [ 802, 67, 80, 13 ];
const SNOW: UiRect = [ 779, 56, 24, 24 ];
const CLOSE: UiRect = [ 885, 61, 16, 16 ];

// The text colour, 2dt 0xFFFCFF00 (ARGB).
export const SERVER_EVENT_TEXT_COLOR = [ 252 / 255, 1, 0, 1 ] as const;

/*
================
ServerEventLayout
================
*/
export interface ServerEventLayout {
	readonly frame: UiRect;
	readonly gauge: UiRect;
	readonly text: UiRect;
	readonly snow: UiRect;
	readonly close: UiRect;
}

/*
================
serverEventLayout

The 2dt rects, kept at their distance from the right edge.
================
*/
export function serverEventLayout( width: number ): ServerEventLayout {
	const shift = width - NATIVE_WIDTH;
	const at = ( r: UiRect ): UiRect => [ r[0] + shift, r[1], r[2], r[3] ];
	return { frame: at( FRAME ), gauge: at( GAUGE ), text: at( TEXT ), snow: at( SNOW ), close: at( CLOSE ) };
}

/*
================
gaugeRatio

count / goal, held to [0, 1]; a goal of zero is an empty gauge.
================
*/
export function gaugeRatio( count: number, goal: number ): number {
	if ( goal <= 0 ) return 0;
	return Math.min( 1, Math.max( 0, count / goal ) );
}

/*
================
gaugeText
================
*/
export function gaugeText( count: number, goal: number ): string {
	return `${count}/${goal}`;
}

/*
================
createServerEventHud
================
*/
export function createServerEventHud() {
	let shown: ServerEventGauge | null = null;
	let closed = false;
	return {
		/*
		================
		reconcile

		The worker's mission, shown with the row on unless the player
		closed it; a gone mission forgets the close.
		================
		*/
		reconcile( gauge: ServerEventGauge | undefined, enabled: boolean ) {
			if ( !gauge ) closed = false;
			shown = gauge && enabled && !closed ? gauge : null;
		},
		shown: () => shown,
		/*
		================
		close
		================
		*/
		close() {
			closed = true;
			shown = null;
		}
	};
}
