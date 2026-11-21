/*
===========================================================================

asset-budget.ts - the byte budget of one asset request, by decode

The worker refuses a response past its budget ("Response exceeds byte
limit"), so a hostile or broken body cannot hold unbounded memory. Model
and image residency keep their own bounds; these cap one transfer.

===========================================================================
*/
import type { AssetRequest } from "@/engine/contracts/assets";

const MIB = 1048576;
// World, front-end world and navigation transactions.
const WORLD_BUDGET_MIB = 128;
// One effect image transaction.
const EFFECT_IMAGES_BUDGET_MIB = 32;
// Every other request: one character model, image or record, or one EasyFX
// effect program (its own file since #3951; the largest is under 1 MiB).
// The shared catalog it replaced had reached 86 MiB.
const DEFAULT_BUDGET_MIB = 16;

/*
================
assetRequestBudget
================
*/
export function assetRequestBudget( decode: Extract<AssetRequest, { kind: "load"; }>["decode"] ): number {
	if ( decode === "world" || decode === "frontend-world" || decode === "navigation" ) return WORLD_BUDGET_MIB * MIB;
	if ( decode === "effects" ) return EFFECT_IMAGES_BUDGET_MIB * MIB;
	return DEFAULT_BUDGET_MIB * MIB;
}
