/*
===========================================================================

asset-slots.ts - how many foreground asset loads may run, by lane

The retail client reads interface art synchronously from its local PK2, so
a window opens on the frame it is asked for. In the browser every image is
a request, and the requests share a few execution slots with the world,
character and effect streams. A shop's icons queued behind town models
held the shop closed for seconds (#3977).

One rule, used by both counts that admit a load: the main thread's
request handles (assets.ts) and the worker's executions (loader.ts). If
they differed, the worker would queue a load the main thread had admitted.

- ASSET_SLOTS loads run at once.
- The interface lane (the UI's image demand) may take any free slot.
- Every other load may take any slot but one: the interface lane always
  has a slot of its own, so a window never waits behind streaming. While
  the interface lane is idle, the others keep the four slots they had
  before the lane existed.

The interface lane borrows free stream slots, but never starves the
streams: each slot it frees is admissible to them again.

===========================================================================
*/
import type { AssetLane } from "@/engine/contracts/assets";

// Four stream slots, as before the interface lane, plus its own.
export const ASSET_SLOTS = 5;
const INTERFACE_RESERVED_SLOTS = 1;
// The slots a stream load can ever hold. Abandoned jobs holding all of them
// mean the world can never load again: the owner treats the worker as dead.
export const ASSET_STREAM_SLOTS = ASSET_SLOTS - INTERFACE_RESERVED_SLOTS;

/*
================
assetSlotsFree

How many more loads of this lane may start, given every load holding a
slot (a main-thread job or a worker execution; each names its lane, and
no lane is the stream). At most ASSET_SLOTS are held, so the walk is short.
================
*/
export function assetSlotsFree(
	lane: AssetLane | undefined,
	held: Iterable<{ readonly lane?: AssetLane | undefined; }>
): number {
	let interfaces = 0, streams = 0;
	for ( const load of held ) {
		if ( load.lane === "interface" ) interfaces++;
		else streams++;
	}
	if ( lane === "interface" ) return Math.max( 0, ASSET_SLOTS - interfaces - streams );
	const reserved = Math.max( interfaces, INTERFACE_RESERVED_SLOTS );
	return Math.max( 0, ASSET_SLOTS - reserved - streams );
}
