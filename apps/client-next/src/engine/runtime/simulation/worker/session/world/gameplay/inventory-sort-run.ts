/*
===========================================================================

inventory-sort-run.ts - one container sort in progress, one move at a time

Port-only, not native (the server's SRO_INVENTORY_SORT, off unless the
shard turns it on). The sort button of the inventory, the warehouse (personal or guild)
or a companion's bag starts a run on that container; every gameplay step
that finds the inventory idle asks inventory-sort.ts for the next move
and sends it through the container's ordinary move request, so exactly
one sort move is ever in flight and the next one is planned from the
container the server's answer left. A new run replaces the old one.

A run ends, without touching the container again, when:
- the container is sorted;
- the server refused the last move (the inventory's error);
- the container is gone or locked: an exchange, a stall or alchemy for
  the bag, a closed warehouse, a dead or dismissed companion;
- the player gives another inventory command (gameplay cancels the run);
- the container returns to a layout this run already left (no progress);
- the move budget runs out.

===========================================================================
*/
import type { InventoryItem, InventorySortContainer } from "@/engine/contracts/gameplay";
import {
	type InventorySortBag,
	inventorySortSignature,
	type InventorySortMove,
	nextInventorySortMove
} from "@/engine/foundation/gameplay/inventory-sort";

// A sort moves each bag item at most once and merges each partial stack
// at most once; twice the bag plus slack is never reached by a sort that
// makes progress.
const MOVE_BUDGET_SLACK = 2;

/*
================
InventorySortFrame

What one step reads: the bag, whether a request is in flight, the
inventory's last refusal and whether a window locks the bag.
================
*/
export interface InventorySortFrame {
	readonly items: readonly InventoryItem[];
	readonly bag: InventorySortBag;
	readonly pending: boolean;
	readonly error: string | null;
	readonly locked: boolean;
}

/*
================
InventorySortStep
================
*/
export interface InventorySortStep {
	readonly move: InventorySortMove | null;
	readonly ended: boolean;
}

const IDLE: InventorySortStep = { move: null, ended: false } as const;
const ENDED: InventorySortStep = { move: null, ended: true } as const;

/*
================
createInventorySortRun
================
*/
export function createInventorySortRun() {
	let active = false, sent = false, moves = 0;
	let container: InventorySortContainer = "bag", gid = 0;
	let seen = new Set<string>();

	/*
	================
	stop
	================
	*/
	function stop() {
		active = false;
		sent = false;
		moves = 0;
		seen = new Set();
	}

	return {
		/*
		================
		start

		Sort this container (gid names the companion of a "cos" run).
		================
		*/
		start( target: InventorySortContainer, companion = 0 ) {
			stop();
			active = true;
			container = target;
			gid = companion;
		},
		/*
		================
		cancel
		================
		*/
		cancel() {
			const was = active;
			stop();
			return was;
		},
		/*
		================
		active
		================
		*/
		active() {
			return active;
		},
		/*
		================
		container

		The container being sorted, or undefined when no run is active.
		================
		*/
		container(): InventorySortContainer | undefined {
			return active ? container : undefined;
		},
		/*
		================
		gid

		The companion a "cos" run sorts.
		================
		*/
		gid() {
			return gid;
		},
		/*
		================
		step

		The move to send now, or null. A run that ends in this step returns
		ended, so the caller republishes its state. The caller sends the
		move through the inventory's ordinary move request and cancels the
		run if the inventory refuses to compose it.
		================
		*/
		step( frame: InventorySortFrame ): InventorySortStep {
			if ( !active || frame.pending ) return IDLE;
			// The inventory clears its error on every request it sends, so an
			// error after our own move is that move's refusal.
			if ( (sent && frame.error !== null) || frame.locked ) {
				stop();
				return ENDED;
			}
			const signature = inventorySortSignature( frame.items, frame.bag );
			const budget = MOVE_BUDGET_SLACK * Math.max( 0, frame.bag.end - frame.bag.first ) + MOVE_BUDGET_SLACK;
			const move = nextInventorySortMove( frame.items, frame.bag );
			if ( !move || seen.has( signature ) || moves >= budget ) {
				stop();
				return ENDED;
			}
			seen.add( signature );
			sent = true;
			moves++;
			return { move, ended: false };
		}
	};
}
