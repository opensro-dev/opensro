import assert from "node:assert/strict";
import test from "node:test";

import { runtimeCifImageReferences } from "../../build/shared/cifRuntimeImageCatalog.mjs";
import { offeredActions } from "../../../apps/client-next/src/engine/foundation/ui/action-layout.ts";

test("every icon the Actions window can offer is in the published runtime image catalog", () => {
	const published = new Set( runtimeCifImageReferences );
	const offered = offeredActions( [], true );

	assert.ok( offered.length > 0, "Auto Select adds its row when the Experimental option is on" );
	for ( const action of offered ) {
		assert.ok( published.has( `icon/${action.icon}` ), `${action.name} icon icon/${action.icon} is not published` );
	}
});
