/*
===========================================================================

ownedModels.mjs - entity models the port draws itself, with no retail BSR

Port-only, not native: two published manifest rows name a resource no
retail archive holds. The Stormbrood egg (crowEgg.mjs, an npc COS) and the
pet gear's ground model (portItemRows.mjs PORT_PET_DROP_MODEL, an itemdrop)
are GLBs the build writes, and their rows carry whatever particle modifiers
they have (the collar borrows the armor chest's glow; the egg has none).
Every reader that would otherwise open the retail BSR behind a manifest row
(the effect closure, the entity modifier publisher, the BSR parity audit)
asks here first, so the list of owned models lives in one place.

===========================================================================
*/
import { CROW_EGG_BSR } from "./crowEgg.mjs";
import { PORT_PET_DROP_MODEL } from "../shared/portItemRows.mjs";

/*
================
isOwnedModel

Whether a manifest row of domain names an owned GLB instead of a retail
BSR. bsr is the row's resource path (res/...).
================
*/
export function isOwnedModel( domain, bsr ) {
	return domain === "npc" && bsr === CROW_EGG_BSR ||
		domain === "itemdrop" && bsr === `res/${PORT_PET_DROP_MODEL}`;
}
