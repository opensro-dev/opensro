/*
===========================================================================

readback.ts - the renderer's picking mask for a texture that lacks one

World textures normally arrive with their mask from the asset worker
(pick-alpha.ts). The image owner calls this on demand for any that do not,
and retains or releases the mask itself.

===========================================================================
*/
import type { PickAlpha } from "@/engine/foundation/rendering/picking";
import { bitmapPickAlpha } from "@/engine/foundation/rendering/pick-alpha";

/*
================
readPickAlpha
================
*/
export function readPickAlpha( image: ImageBitmap ): PickAlpha {
	return bitmapPickAlpha( image );
}
