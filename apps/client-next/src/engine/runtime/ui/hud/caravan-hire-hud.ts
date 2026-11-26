/*
===========================================================================

caravan-hire-hud.ts - the AI caravan hire window's local state and clicks

Port-only, not native (SRO_AI_CARAVANS). The worker owns the hire list
the server sends (foundation/gameplay/caravan-hire.ts); this owns what the
player does with it before anything is sent: the "show only available"
filter, the list page, and the hire being confirmed. Every hire asks
first; a Trader's also picks how many stars of goods it carries.
caravan-hire-window.ts draws this state and the list.

===========================================================================
*/

import type { GameplayCommand } from "@/engine/contracts/gameplay";
import {
	CARAVAN_HIRE_EXTEND,
	CARAVAN_HIRE_LEAD,
	CARAVAN_HIRE_OPEN,
	CARAVAN_HIRE_TAKE,
	type CaravanHire
} from "@/engine/foundation/gameplay/caravan-hire";

// The stars a Trader's pick starts at, when the list offers that many.
export const CARAVAN_HIRE_DEFAULT_STARS = 3;

const ROW_PREFIX = "caravan-hire-row:";

/*
================
CaravanHirePick

The hire waiting on the confirm. stars is 0 for a Hunter or Thief and
for an extension; a Trader's is 1..the list's star choices.
================
*/
export interface CaravanHirePick {
	readonly name: string;
	readonly display: string;
	readonly role: string;
	stars: number;
	readonly extend: boolean;
}

/*
================
CaravanHireSend

The commands this window sends: a hire request, or the close.
================
*/
export type CaravanHireSend = Extract<GameplayCommand, { kind: "caravan-hire" | "caravan-hire-close"; }>;
type CaravanHireRequest = Extract<GameplayCommand, { kind: "caravan-hire"; }>;

/*
================
caravanHireOpenRequest

The request that opens (or refreshes) the hire window.
================
*/
export function caravanHireOpenRequest(): CaravanHireRequest {
	return { kind: "caravan-hire", operation: CARAVAN_HIRE_OPEN, stars: 0, name: "" };
}

/*
================
caravanHireLeadRequest

Hands the caravan's lead to name (operation 2).
================
*/
export function caravanHireLeadRequest( name: string ): CaravanHireRequest {
	return { kind: "caravan-hire", operation: CARAVAN_HIRE_LEAD, stars: 0, name };
}

/*
================
createCaravanHireHud
================
*/
export function createCaravanHireHud() {
	let onlyAvailable = false, page = 0;
	let pick: CaravanHirePick | null = null;
	return {
		/*
		================
		onlyAvailable
		================
		*/
		onlyAvailable() {
			return onlyAvailable;
		},
		/*
		================
		page

		The list page, clamped to the pages the filtered list now has (a
		refresh can shorten it under the player).
		================
		*/
		page( pages: number ) {
			page = Math.max( 0, Math.min( page, pages - 1 ) );
			return page;
		},
		/*
		================
		pick
		================
		*/
		pick(): Readonly<CaravanHirePick> | null {
			return pick;
		},
		/*
		================
		activate

		One click on a caravan-hire-* control. false when id is not this
		window's or no hire window is open; otherwise the click is taken and
		send carries any request it makes. The close sends its own frame:
		the server tracks who has the window open.
		================
		*/
		activate(
			id: string,
			hire: CaravanHire | undefined,
			send: ( command: CaravanHireSend ) => void
		): boolean {
			if ( !id.startsWith( "caravan-hire" ) || !hire ) return false;
			if ( id === "caravan-hire-close" ) {
				pick = null;
				send( { kind: "caravan-hire-close" } );
			} else if ( id === "caravan-hire-filter" ) {
				onlyAvailable = !onlyAvailable;
				page = 0;
			} else if ( id === "caravan-hire-prev" ) {
				page = Math.max( 0, page - 1 );
			} else if ( id === "caravan-hire-next" ) {
				page++;
			} else if ( id === "caravan-hire-refresh" ) {
				send( caravanHireOpenRequest() );
			} else if ( id.startsWith( ROW_PREFIX ) ) {
				const row = hire.rows.find( r => r.name === id.slice( ROW_PREFIX.length ) );
				if ( row?.available ) {
					const trader = row.role === "Trader" && hire.stars.length > 0;
					pick = {
						name: row.name,
						display: row.display,
						role: row.role,
						stars: trader ? Math.min( CARAVAN_HIRE_DEFAULT_STARS, hire.stars.length ) : 0,
						extend: false
					};
				} else if ( row?.extend ) {
					// The player's own Hunter or Thief: renew its hour, after a confirm.
					pick = { name: row.name, display: row.display, role: row.role, stars: 0, extend: true };
				}
			} else if ( id === "caravan-hire-star-less" || id === "caravan-hire-star-more" ) {
				if ( pick && pick.stars > 0 ) {
					const step = id === "caravan-hire-star-less" ? -1 : 1;
					pick.stars = Math.max( 1, Math.min( hire.stars.length, pick.stars + step ) );
				}
			} else if ( id === "caravan-hire-star-ok" ) {
				if ( pick ) {
					send( {
						kind: "caravan-hire",
						operation: pick.extend ? CARAVAN_HIRE_EXTEND : CARAVAN_HIRE_TAKE,
						stars: pick.stars,
						name: pick.name
					} );
				}
				pick = null;
			} else if ( id === "caravan-hire-star-cancel" ) {
				pick = null;
			}
			return true;
		}
	};
}

export type CaravanHireHud = ReturnType<typeof createCaravanHireHud>;
