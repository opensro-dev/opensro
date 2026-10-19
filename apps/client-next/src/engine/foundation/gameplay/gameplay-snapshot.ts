/*
===========================================================================

gameplay-snapshot.ts - the shared rule for partial gameplay publications

The worker coalesces queued snapshots and presentation applies delivered
ones. Both retain the same three omitted fields; every other field comes
only from the newest snapshot. This pure helper owns no state or indexes.

===========================================================================
*/
import type { GameplayState } from "@/engine/contracts/gameplay";

/*
================
mergeGameplaySnapshots

An omitted shop retains its previous value; an explicitly undefined shop
closes it. Preserve that distinction even when neither input names a shop:
the merged publication may follow a shop delivered in an earlier batch.
The caller discards previous at a world reset. Presentation alone rebuilds
the derived skill index after merging.
================
*/
export function mergeGameplaySnapshots(
	previous: GameplayState | null | undefined,
	next: GameplayState
): GameplayState {
	const gmCatalog = next.gmCatalog ?? previous?.gmCatalog;
	return {
		...next,
		skillCatalog: next.skillCatalog ?? previous?.skillCatalog,
		...(gmCatalog ? { gmCatalog } : {}),
		social: next.social ?? previous?.social,
		...(!("shop" in next) && previous && "shop" in previous ? { shop: previous.shop } : {})
	};
}
